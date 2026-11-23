package modem

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"e173-control/pkg/at"
	"e173-control/pkg/pdu"
)

type SimStatus string

const (
	SimReady   SimStatus = "В сети (READY)"
	SimLocked  SimStatus = "PIN код"
	SimMissing SimStatus = "Нет SIM"
	SimUnknown SimStatus = "Определение SIM..."
)

type Info struct {
	Port          string
	Slot          int
	USBPath       string
	IMEI          string
	Model         string
	Revision      string
	PhoneNumber   string
	Operator      string
	SignalDbm     int
	SignalPercent int
	SimState      SimStatus
	Balance       string
	Status        string
	LastError     string
	LastUpdated   time.Time
}

type Device struct {
	portName string
	client   *at.Client
	mu       sync.RWMutex
	info     Info
	onUpdate func()

	cmdQueue chan task
	ctx      context.Context
	cancel   context.CancelFunc
}

type task struct {
	execute func(c *at.Client)
}

func NewDevice(portName, imei, model, rev, usbPath string, slot int, existingClient *at.Client, onUpdate func()) *Device {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Device{
		portName: portName,
		client:   existingClient,
		onUpdate: onUpdate,
		info: Info{
			Port:        portName,
			Slot:        slot,
			USBPath:     usbPath,
			IMEI:        imei,
			Model:       model,
			Revision:    rev,
			PhoneNumber: "—",
			Status:      "Подключен",
			SimState:    SimUnknown,
			Operator:    "—",
			LastUpdated: time.Now(),
		},
		cmdQueue: make(chan task, 32),
		ctx:      ctx,
		cancel:   cancel,
	}
	return d
}

func (d *Device) GetInfo() Info {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.info
}

func (d *Device) SetSlot(slot int) {
	d.mu.Lock()
	d.info.Slot = slot
	d.mu.Unlock()
	d.notify()
}

func (d *Device) notify() {
	if d.onUpdate != nil {
		d.onUpdate()
	}
}

func (d *Device) Start() error {
	if d.client == nil {
		client, err := at.Open(d.portName, 115200)
		if err != nil {
			d.mu.Lock()
			d.info.Status = "Ошибка порта"
			d.info.LastError = err.Error()
			d.mu.Unlock()
			d.notify()
			return err
		}
		d.client = client
	}

	d.initModemHardware()
	d.notify()

	go d.workerLoop()

	return nil
}

func (d *Device) Stop() {
	d.cancel()
	if d.client != nil {
		_ = d.client.Close()
	}
}

func (d *Device) initModemHardware() {
	if d.info.Model == "" || d.info.Revision == "" {
		resp, err := d.client.Command("ATI", 300*time.Millisecond)
		if err == nil {
			d.mu.Lock()
			if m := extractField(resp, "Model:"); m != "" && d.info.Model == "" {
				d.info.Model = m
			}
			if r := extractField(resp, "Revision:"); r != "" {
				d.info.Revision = r
			}
			d.mu.Unlock()
		}
	}

	_, _ = d.client.Command("AT+CUSD=1", 300*time.Millisecond)
	_, _ = d.client.Command("AT+CMGF=1", 300*time.Millisecond)
}

func (d *Device) workerLoop() {
	d.pollSimAndNetwork()

	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	phoneRequested := false

	for {
		select {
		case <-d.ctx.Done():
			return

		case t := <-d.cmdQueue:
			if d.client != nil {
				t.execute(d.client)
			}

		case <-ticker.C:
			d.pollSimAndNetwork()

			// Если SIM в сети, но номер еще не определен — один раз автоматически запрашиваем *161#
			d.mu.RLock()
			ready := d.info.SimState == SimReady
			hasPhone := d.info.PhoneNumber != "" && d.info.PhoneNumber != "—"
			d.mu.RUnlock()

			if ready && !hasPhone && !phoneRequested {
				phoneRequested = true
				go func() {
					_, _ = d.SendUSSD("*161#", 10*time.Second)
				}()
			}
		}
	}
}

func (d *Device) pollSimAndNetwork() {
	if d.client == nil {
		return
	}

	cpinResp, err := d.client.Command("AT+CPIN?", 1*time.Second)
	combined := cpinResp
	if err != nil {
		combined += " " + err.Error()
	}

	hasSim := false
	var newSimState SimStatus

	if strings.Contains(combined, "READY") {
		hasSim = true
		newSimState = SimReady
	} else if strings.Contains(combined, "SIM PIN") || strings.Contains(combined, "SIM PUK") {
		newSimState = SimLocked
	} else if strings.Contains(combined, "SIM failure") || strings.Contains(combined, "not inserted") || strings.Contains(combined, "ERROR") {
		newSimState = SimMissing
	} else {
		newSimState = SimUnknown
	}

	d.mu.Lock()
	d.info.SimState = newSimState
	d.info.LastUpdated = time.Now()

	if !hasSim {
		d.info.Operator = "—"
		d.info.PhoneNumber = "—"
		d.info.SignalPercent = 0
		d.info.SignalDbm = -113
		d.info.Status = "Онлайн (без SIM)"
		d.mu.Unlock()
		d.notify()
		return
	}
	d.mu.Unlock()

	// CSQ
	csqResp, err := d.client.Command("AT+CSQ", 1*time.Second)
	var signalDbm, signalPercent int
	if err == nil {
		signalDbm, signalPercent = parseCSQ(csqResp)
	}

	// COPS
	copsResp, err := d.client.Command("AT+COPS?", 1*time.Second)
	operator := ""
	if err == nil {
		operator = parseCOPS(copsResp)
	}

	d.mu.Lock()
	d.info.SignalDbm = signalDbm
	d.info.SignalPercent = signalPercent
	if operator != "" {
		d.info.Operator = operator
	}
	d.info.Status = "Онлайн"
	d.mu.Unlock()

	d.notify()
}

// SendUSSD выполняет USSD запрос и автоматически парсит номер телефона и баланс
func (d *Device) SendUSSD(code string, timeout time.Duration) (string, error) {
	resChan := make(chan struct {
		text string
		err  error
	}, 1)

	d.cmdQueue <- task{
		execute: func(c *at.Client) {
			encodedCode := pdu.EncodeGSM7BitHuawei(code)
			cmd := fmt.Sprintf("AT+CUSD=1,\"%s\",15", encodedCode)

			_, err := c.Command(cmd, 3*time.Second)
			if err != nil {
				rawCmd := fmt.Sprintf("AT+CUSD=1,\"%s\",15", code)
				_, err = c.Command(rawCmd, 3*time.Second)
				if err != nil {
					resChan <- struct {
						text string
						err  error
					}{"", err}
					return
				}
			}

			resp, err := c.WaitFor("+CUSD:", timeout)
			if err != nil {
				resChan <- struct {
					text string
					err  error
				}{"", err}
				return
			}

			decoded := pdu.DecodeUSSDResponse(resp)
			resChan <- struct {
				text string
				err  error
			}{decoded, nil}
		},
	}

	select {
	case res := <-resChan:
		if res.err == nil {
			d.mu.Lock()
			// Если ответ содержит номер телефона (например, Vash nomer: 380733623737)
			if phone := extractPhoneNumber(res.text); phone != "" {
				d.info.PhoneNumber = phone
			}

			// Если это запрос номера *161#, не затираем баланс, если баланс уже был получен ранее
			if strings.Contains(code, "161") {
				if d.info.Balance == "" {
					d.info.Balance = res.text
				}
			} else {
				d.info.Balance = res.text
			}
			d.mu.Unlock()
			d.notify()
		}
		return res.text, res.err
	case <-time.After(timeout + 2*time.Second):
		return "", at.ErrTimeout
	}
}

// extractPhoneNumber ищет номер телефона в ответе оператора
var phoneRegex = regexp.MustCompile(`(?:Vash nomer:?\s*|номер:?\s*|\+?)(380\d{9}|0\d{9})`)

func extractPhoneNumber(text string) string {
	matches := phoneRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		num := matches[1]
		if strings.HasPrefix(num, "0") {
			num = "38" + num
		}
		if !strings.HasPrefix(num, "+") {
			num = "+" + num
		}
		return num
	}
	return ""
}

func (d *Device) SendSMS(phone, text string) error {
	resChan := make(chan error, 1)

	d.cmdQueue <- task{
		execute: func(c *at.Client) {
			_, _ = c.Command("AT+CMGF=1", 2*time.Second)
			_, _ = c.Command("AT+CSCS=\"GSM\"", 2*time.Second)

			cmd := fmt.Sprintf("AT+CMGS=\"%s\"", phone)
			_, _ = c.Command(cmd, 2*time.Second)

			payload := text + string([]byte{0x1A})
			_ = c.RawWrite([]byte(payload))

			resp, err := c.WaitFor("OK", 15*time.Second)
			if err != nil {
				resChan <- fmt.Errorf("sms error: %w (resp: %s)", err, resp)
				return
			}
			resChan <- nil
		},
	}

	select {
	case err := <-resChan:
		return err
	case <-time.After(20 * time.Second):
		return at.ErrTimeout
	}
}

func (d *Device) ExecuteCustomAT(cmd string) (string, error) {
	resChan := make(chan struct {
		out string
		err error
	}, 1)

	d.cmdQueue <- task{
		execute: func(c *at.Client) {
			out, err := c.Command(cmd, 5*time.Second)
			resChan <- struct {
				out string
				err error
			}{out, err}
		},
	}

	select {
	case res := <-resChan:
		return res.out, res.err
	case <-time.After(6 * time.Second):
		return "", at.ErrTimeout
	}
}

func extractField(text, field string) string {
	lines := strings.Split(text, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, field) {
			return strings.TrimSpace(strings.TrimPrefix(l, field))
		}
	}
	return ""
}

func parseCSQ(csq string) (dbm int, percent int) {
	idx := strings.Index(csq, "+CSQ:")
	if idx == -1 {
		return -113, 0
	}
	parts := strings.Split(csq[idx+5:], ",")
	if len(parts) == 0 {
		return -113, 0
	}
	rssiStr := strings.TrimSpace(parts[0])
	rssi, err := strconv.Atoi(rssiStr)
	if err != nil || rssi == 99 {
		return -113, 0
	}

	dbm = -113 + (rssi * 2)
	percent = int((float64(rssi) / 31.0) * 100)
	if percent > 100 {
		percent = 100
	}
	return dbm, percent
}

func parseCOPS(cops string) string {
	start := strings.Index(cops, "\"")
	if start == -1 {
		return ""
	}
	end := strings.LastIndex(cops, "\"")
	if end <= start {
		return ""
	}
	name := cops[start+1 : end]

	switch name {
	case "25501", "255 01":
		return "Vodafone UA"
	case "25503", "255 03":
		return "Kyivstar"
	case "25506", "255 06":
		return "lifecell"
	case "25001", "250 01":
		return "MTS"
	case "25002", "250 02":
		return "MegaFon"
	case "25099", "250 99":
		return "Beeline"
	case "25020", "250 20":
		return "Tele2"
	}

	return name
}
