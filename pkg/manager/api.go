package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type APIPortData struct {
	Port        string `json:"port"`
	IMEI        string `json:"imei"`
	PhoneNumber string `json:"phone_number"`
	Status      string `json:"status"`
	Operator    string `json:"operator"`
	Model       string `json:"model"`
	Signal      int    `json:"signal"`
}

type APIServer struct {
	pool    *Pool
	port    int
	server  *http.Server
	mu      sync.Mutex
	running bool
}

func NewAPIServer(p *Pool, port int) *APIServer {
	return &APIServer{
		pool: p,
		port: port,
	}
}

func (s *APIServer) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}

	mux := http.NewServeMux()

	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		devs := s.pool.GetAllDevices()

		// Список модемов
		list := make([]APIPortData, 0, len(devs))
		for _, d := range devs {
			statusStr := "Connected"
			if d.Status != "" && (strings.Contains(d.Status, "Ошибка") || strings.Contains(d.Status, "Error")) {
				statusStr = "Disconnected"
			}
			op := d.Operator
			if op == "" || op == "—" {
				if d.SimState == "Нет SIM" {
					op = "No SIM"
				} else {
					op = "No Service"
				}
			}

			portLabel := strings.TrimPrefix(d.Port, "/dev/")
			if d.Slot > 0 {
				portLabel = fmt.Sprintf("#%d %s", d.Slot, portLabel)
			}

			list = append(list, APIPortData{
				Port:        portLabel,
				IMEI:        d.IMEI,
				PhoneNumber: d.PhoneNumber,
				Status:      statusStr,
				Operator:    op,
				Model:       d.Model,
				Signal:      d.SignalPercent,
			})
		}

		// Если запросили конкретный порт (/api/port1) - возвращаем первый модем
		if strings.Contains(r.URL.Path, "port1") || len(list) == 1 {
			if len(list) > 0 {
				_ = json.NewEncoder(w).Encode(list[0])
				return
			}
			_ = json.NewEncoder(w).Encode(APIPortData{
				Port:     "—",
				IMEI:     "",
				Status:   "Disconnected",
				Operator: "No Device",
			})
			return
		}

		// Если модемов несколько — отдаем список
		_ = json.NewEncoder(w).Encode(list)
	}

	mux.HandleFunc("/api/port1", handler)
	mux.HandleFunc("/api/modems", handler)

	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler: mux,
	}
	s.server = srv
	s.running = true
	s.mu.Unlock()

	go func() {
		_ = srv.ListenAndServe()
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	return nil
}

func (s *APIServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
	s.running = false
}
