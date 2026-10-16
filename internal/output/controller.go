package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type ControllerWriter struct {
	cfg       Config
	client    *http.Client
	mu        sync.Mutex
	flushMu   sync.Mutex
	batch     [][]byte
	ticker    *time.Ticker
	done      chan struct{}
	closeOnce sync.Once
}

func NewControllerWriter(cfg Config) (*ControllerWriter, error) {
	if cfg.URL == "" || cfg.TokenFile == "" || (cfg.NodeID == "" && cfg.NodeIDFile == "") {
		return nil, fmt.Errorf("controller output requires url, token_file, and node_id or node_id_file")
	}
	client, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.FlushSec <= 0 {
		cfg.FlushSec = 2
	}
	writer := &ControllerWriter{
		cfg: cfg, client: client,
		ticker: time.NewTicker(time.Duration(cfg.FlushSec) * time.Second), done: make(chan struct{}),
	}
	if !cfg.Synchronous {
		go writer.flushLoop()
	}
	return writer, nil
}

func (w *ControllerWriter) Write(envelope []byte, route string) error {
	if !acceptsRemoteRoute(route) || !acceptsRoutes(w.cfg.Routes, route) {
		return nil
	}
	w.mu.Lock()
	w.batch = append(w.batch, append([]byte(nil), envelope...))
	full := len(w.batch) >= w.cfg.BatchSize
	w.mu.Unlock()
	if full {
		return w.flush()
	}
	return nil
}

func (w *ControllerWriter) Close() error {
	w.closeOnce.Do(func() { close(w.done); w.ticker.Stop() })
	return w.flush()
}

func (w *ControllerWriter) flushLoop() {
	for {
		select {
		case <-w.ticker.C:
			_ = w.flush()
		case <-w.done:
			return
		}
	}
}

func (w *ControllerWriter) Flush() error { return w.flush() }

func (w *ControllerWriter) flush() error {
	w.flushMu.Lock()
	defer w.flushMu.Unlock()
	w.mu.Lock()
	if len(w.batch) == 0 {
		w.mu.Unlock()
		return nil
	}
	batch := w.batch
	w.batch = nil
	w.mu.Unlock()

	tokenBytes, tokenErr := os.ReadFile(w.cfg.TokenFile)
	nodeID := w.cfg.NodeID
	if nodeID == "" {
		idBytes, err := os.ReadFile(w.cfg.NodeIDFile)
		if err != nil {
			return w.failed(batch, err)
		}
		nodeID = strings.TrimSpace(string(idBytes))
	}
	if tokenErr != nil {
		return w.failed(batch, tokenErr)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" || nodeID == "" {
		return w.failed(batch, fmt.Errorf("controller node identity is not enrolled"))
	}

	events := make([]json.RawMessage, len(batch))
	for index, event := range batch {
		events[index] = json.RawMessage(event)
	}
	payload, _ := json.Marshal(struct {
		NodeID string            `json:"node_id"`
		Events []json.RawMessage `json:"events"`
	}{NodeID: nodeID, Events: events})
	endpoint := strings.TrimRight(w.cfg.URL, "/") + "/api/v1/node/events"
	deliveryErr := post(w.client, w.cfg, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	})
	if deliveryErr == nil {
		return nil
	}

	return w.failed(batch, fmt.Errorf("controller output: %w", deliveryErr))
}

func (w *ControllerWriter) failed(batch [][]byte, deliveryErr error) error {
	if w.cfg.FallbackPath != "" {
		if err := spoolFailedBatch(w.cfg, batch); err == nil {
			return &DeliveryError{Destination: "controller", Accounted: true, Cause: deliveryErr}
		} else {
			deliveryErr = fmt.Errorf("%v; fallback accounting: %w", deliveryErr, err)
		}
	}
	w.mu.Lock()
	w.batch = append(batch, w.batch...)
	w.mu.Unlock()
	return &DeliveryError{Destination: "controller", Cause: deliveryErr}
}
