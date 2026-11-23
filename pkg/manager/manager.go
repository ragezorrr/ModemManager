package manager

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"e173-control/pkg/at"
	"e173-control/pkg/modem"
	"e173-control/pkg/usbpath"
	"go.bug.st/serial"
)

type Pool struct {
	mu           sync.RWMutex
	devices      map[string]*modem.Device // key: IMEI
	ports        map[string]string        // port -> IMEI
	probingPort  map[string]bool          // port -> is probing
	slotsMapping map[string]int           // USBPath -> Slot (1..49)

	onChange func([]modem.Info)
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewPool(onChange func([]modem.Info)) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		devices:      make(map[string]*modem.Device),
		ports:        make(map[string]string),
		probingPort:  make(map[string]bool),
		slotsMapping: make(map[string]int),
		onChange:     onChange,
		ctx:          ctx,
		cancel:       cancel,
	}

	p.loadMapping()
	return p
}

const mappingFile = "slots_mapping.json"

func (p *Pool) loadMapping() {
	data, err := os.ReadFile(mappingFile)
	if err == nil {
		_ = json.Unmarshal(data, &p.slotsMapping)
	}
}

func (p *Pool) saveMapping() {
	data, err := json.MarshalIndent(p.slotsMapping, "", "  ")
	if err == nil {
		_ = os.WriteFile(mappingFile, data, 0644)
	}
}

func (p *Pool) SetSlotForDevice(imei string, slot int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	dev, ok := p.devices[imei]
	if !ok {
		return
	}

	dev.SetSlot(slot)
	info := dev.GetInfo()
	if info.USBPath != "" {
		p.slotsMapping[info.USBPath] = slot
		p.saveMapping()
	}
	p.notify()
}

func (p *Pool) Start() {
	go p.scanLoop()
}

func (p *Pool) Rescan() {
	go func() {
		p.scanPorts()
	}()
}

func (p *Pool) Stop() {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, dev := range p.devices {
		dev.Stop()
	}
}

func (p *Pool) GetAllDevices() []modem.Info {
	p.mu.RLock()
	defer p.mu.RUnlock()

	res := make([]modem.Info, 0, len(p.devices))
	for _, dev := range p.devices {
		res = append(res, dev.GetInfo())
	}
	return res
}

func (p *Pool) GetDeviceByIMEI(imei string) *modem.Device {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.devices[imei]
}

func (p *Pool) notify() {
	if p.onChange != nil {
		p.onChange(p.GetAllDevices())
	}
}

func (p *Pool) scanLoop() {
	p.scanPorts()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.scanPorts()
		}
	}
}

func (p *Pool) collectAllPortNames() []string {
	seen := make(map[string]bool)
	var list []string

	if ports, err := serial.GetPortsList(); err == nil {
		for _, port := range ports {
			if (strings.Contains(port, "ttyUSB") || strings.Contains(port, "COM")) && !strings.Contains(port, "ttyS") {
				if !seen[port] {
					seen[port] = true
					list = append(list, port)
				}
			}
		}
	}

	if matches, err := filepath.Glob("/dev/ttyUSB*"); err == nil {
		for _, port := range matches {
			if !seen[port] {
				seen[port] = true
				list = append(list, port)
			}
		}
	}

	// ttyUSB6 перед ttyUSB3 для E173
	sort.Slice(list, func(i, j int) bool {
		return list[i] > list[j]
	})

	return list
}

func (p *Pool) scanPorts() {
	allPorts := p.collectAllPortNames()
	activeMap := make(map[string]bool)

	for _, portName := range allPorts {
		activeMap[portName] = true

		p.mu.RLock()
		_, alreadyKnown := p.ports[portName]
		isProbing := p.probingPort[portName]
		p.mu.RUnlock()

		if alreadyKnown || isProbing {
			continue
		}

		go p.probeAndAddPort(portName)
	}

	changed := false
	p.mu.Lock()
	for portName, imei := range p.ports {
		if !activeMap[portName] {
			if dev, exists := p.devices[imei]; exists {
				dev.Stop()
				delete(p.devices, imei)
				changed = true
			}
			delete(p.ports, portName)
		}
	}
	p.mu.Unlock()

	if changed {
		p.notify()
	}
}

func (p *Pool) probeAndAddPort(portName string) {
	p.mu.Lock()
	p.probingPort[portName] = true
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.probingPort, portName)
		p.mu.Unlock()
	}()

	client, err := at.Open(portName, 115200)
	if err != nil {
		return
	}

	// Быстрое получение IMEI через AT+CGSN
	imeiResp, err := client.Command("AT+CGSN", 250*time.Millisecond)
	imei := extractIMEIOnly(imeiResp)

	if imei == "" || err != nil {
		atiResp, err := client.Command("ATI", 300*time.Millisecond)
		if err == nil {
			imei = extractField(atiResp, "IMEI:")
		}
	}

	if imei == "" {
		_ = client.Close()
		return
	}

	p.mu.Lock()
	if _, exists := p.devices[imei]; exists {
		p.ports[portName] = imei
		p.mu.Unlock()
		_ = client.Close()
		return
	}

	usbPath := usbpath.GetUSBPath(portName)
	slot := usbpath.Calculate49Slot(usbPath, p.slotsMapping)
	if slot == 0 {
		slot = len(p.devices) + 1
	}

	dev := modem.NewDevice(portName, imei, "E173", "", usbPath, slot, client, func() {
		p.notify()
	})
	p.devices[imei] = dev
	p.ports[portName] = imei
	p.mu.Unlock() // <--- КРИТИЧЕСКИ ВАЖНО: отпускаем блокировку ДО dev.Start()!

	if err := dev.Start(); err != nil {
		p.mu.Lock()
		delete(p.devices, imei)
		delete(p.ports, portName)
		p.mu.Unlock()
		_ = client.Close()
		return
	}

	p.notify()
}

func extractIMEIOnly(resp string) string {
	lines := strings.Split(resp, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if len(l) == 15 && isDigitsOnly(l) {
			return l
		}
	}
	return ""
}

func isDigitsOnly(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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

func (p *Pool) BulkUSSD(code string) {
	p.mu.RLock()
	devices := make([]*modem.Device, 0, len(p.devices))
	for _, dev := range p.devices {
		devices = append(devices, dev)
	}
	p.mu.RUnlock()

	for _, dev := range devices {
		go func(d *modem.Device) {
			_, _ = d.SendUSSD(code, 15*time.Second)
			p.notify()
		}(dev)
	}
}
