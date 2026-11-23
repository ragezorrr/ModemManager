package at

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

var (
	ErrTimeout   = errors.New("command timeout")
	ErrPortError = errors.New("port error")
)

type Client struct {
	portName string
	baudRate int
	port     serial.Port
	mu       sync.Mutex
	closed   bool
}

func Open(portName string, baudRate int) (*Client, error) {
	if baudRate == 0 {
		baudRate = 115200
	}
	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	p, err := serial.Open(portName, mode)
	if err != nil {
		return nil, fmt.Errorf("open serial %s: %w", portName, err)
	}

	// Минимальный таймаут 25 мс для мгновенного неблокирующего ввода/вывода
	_ = p.SetReadTimeout(25 * time.Millisecond)

	c := &Client{
		portName: portName,
		baudRate: baudRate,
		port:     p,
	}

	return c, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.port != nil {
		return c.port.Close()
	}
	return nil
}

func (c *Client) flush() {
	buf := make([]byte, 256)
	for {
		n, err := c.port.Read(buf)
		if err != nil || n == 0 {
			break
		}
	}
}

// Command отправляет AT-команду и немедленно возвращает результат при получении OK/ERROR
func (c *Client) Command(cmd string, timeout time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return "", ErrPortError
	}

	c.flush()

	fullCmd := strings.TrimRight(cmd, "\r\n") + "\r\n"
	_, err := c.port.Write([]byte(fullCmd))
	if err != nil {
		return "", fmt.Errorf("write error: %w", err)
	}

	deadline := time.Now().Add(timeout)
	var output bytes.Buffer
	buf := make([]byte, 512)

	for time.Now().Before(deadline) {
		n, err := c.port.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
			str := output.String()
			if strings.Contains(str, "\nOK") || strings.HasSuffix(strings.TrimSpace(str), "OK") {
				return cleanResponse(str), nil
			}
			if strings.Contains(str, "ERROR") {
				return cleanResponse(str), fmt.Errorf("at error: %s", cleanResponse(str))
			}
		}
		if err != nil {
			return output.String(), fmt.Errorf("read error: %w", err)
		}
	}

	if output.Len() > 0 {
		return cleanResponse(output.String()), nil
	}
	return "", ErrTimeout
}

func (c *Client) RawWrite(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrPortError
	}
	_, err := c.port.Write(data)
	return err
}

func (c *Client) WaitFor(sub string, timeout time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	deadline := time.Now().Add(timeout)
	var output bytes.Buffer
	buf := make([]byte, 512)

	for time.Now().Before(deadline) {
		n, err := c.port.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
			str := output.String()
			if strings.Contains(str, sub) {
				return str, nil
			}
			if strings.Contains(str, "ERROR") {
				return str, fmt.Errorf("at error: %s", str)
			}
		}
		if err != nil {
			return output.String(), fmt.Errorf("read error: %w", err)
		}
	}
	return output.String(), ErrTimeout
}

func cleanResponse(resp string) string {
	resp = strings.ReplaceAll(resp, "\r\r\n", "\r\n")
	return strings.TrimSpace(resp)
}
