// Package output - Loki push API writer.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LokiWriter batches log entries and pushes to a Loki endpoint.
type LokiWriter struct {
	cfg       Config
	batch     []lokiEntry
	mu        sync.Mutex
	flushMu   sync.Mutex
	client    *http.Client
	ticker    *time.Ticker
	done      chan struct{}
	closeOnce sync.Once
}

type lokiEntry struct {
	ts      int64 // nanoseconds
	message string
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

type lokiPush struct {
	Streams []lokiStream `json:"streams"`
}

// NewLokiWriter creates a Loki writer and starts the flush goroutine.
func NewLokiWriter(cfg Config) (*LokiWriter, error) {
	client, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	flushSec := cfg.FlushSec
	if flushSec <= 0 {
		flushSec = 5
	}
	w := &LokiWriter{
		cfg:    cfg,
		client: client,
		ticker: time.NewTicker(time.Duration(flushSec) * time.Second),
		done:   make(chan struct{}),
	}
	if !cfg.Synchronous {
		go w.flushLoop()
	}
	return w, nil
}

func (w *LokiWriter) Write(envelope []byte, route string) error {
	if !acceptsRemoteRoute(route) || !acceptsRoutes(w.cfg.Routes, route) {
		return nil
	}
	ts := time.Now().UnixNano()
	w.mu.Lock()
	w.batch = append(w.batch, lokiEntry{ts: ts, message: string(envelope)})
	full := len(w.batch) >= w.cfg.BatchSize
	w.mu.Unlock()
	if full {
		return w.flush()
	}
	return nil
}

func (w *LokiWriter) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		w.ticker.Stop()
	})
	return w.flush()
}

func (w *LokiWriter) flushLoop() {
	for {
		select {
		case <-w.ticker.C:
			_ = w.flush()
		case <-w.done:
			return
		}
	}
}

func (w *LokiWriter) Flush() error { return w.flush() }

func (w *LokiWriter) flush() error {
	w.flushMu.Lock()
	defer w.flushMu.Unlock()
	w.mu.Lock()
	if len(w.batch) == 0 {
		w.mu.Unlock()
		return nil
	}
	entries := w.batch
	w.batch = nil
	w.mu.Unlock()

	labels := w.cfg.Labels
	if labels == nil {
		labels = map[string]string{"job": "nli"}
	}

	values := make([][2]string, len(entries))
	for i, e := range entries {
		values[i] = [2]string{strconv.FormatInt(e.ts, 10), e.message}
	}

	push := lokiPush{Streams: []lokiStream{{Stream: labels, Values: values}}}
	body, err := json.Marshal(push)
	if err != nil {
		return err
	}

	endpoint := strings.TrimRight(w.cfg.URL, "/")
	if !strings.HasSuffix(endpoint, "/loki/api/v1/push") {
		endpoint += "/loki/api/v1/push"
	}
	deliveryErr := post(w.client, w.cfg, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if w.cfg.TenantID != "" {
			req.Header.Set("X-Scope-OrgID", w.cfg.TenantID)
		}
		return req, nil
	})
	if deliveryErr == nil {
		return nil
	}

	records := make([][]byte, len(entries))
	for index, entry := range entries {
		records[index] = []byte(entry.message)
	}
	if w.cfg.FallbackPath != "" {
		if spoolErr := spoolFailedBatch(w.cfg, records); spoolErr == nil {
			return &DeliveryError{Destination: "loki", Accounted: true, Cause: deliveryErr}
		} else {
			deliveryErr = fmt.Errorf("%v; fallback accounting: %w", deliveryErr, spoolErr)
		}
	}
	w.mu.Lock()
	w.batch = append(entries, w.batch...)
	w.mu.Unlock()
	return &DeliveryError{Destination: "loki", Cause: deliveryErr}
}
