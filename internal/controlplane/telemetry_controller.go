package controlplane

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nexora.local/nli-v13/internal/eventstore"
	"nexora.local/nli-v13/internal/wsrelay"
	"nexora.local/nli-v13/telemetry"
)

type TelemetryController struct{ store *eventstore.Store }

// StreamHub publishes bounded latest-state updates to non-blocking subscribers.
type StreamHub struct {
	mu         sync.Mutex
	next       uint64
	latest     map[string]telemetry.StreamFileTelemetry
	subs       map[uint64]chan telemetry.StreamFileTelemetry
	max        int
	maxSources int
	sequence   atomic.Uint64
}

func NewStreamHub(maxSubscribers int) *StreamHub {
	if maxSubscribers < 1 {
		maxSubscribers = 64
	}
	return &StreamHub{latest: map[string]telemetry.StreamFileTelemetry{}, subs: map[uint64]chan telemetry.StreamFileTelemetry{}, max: maxSubscribers, maxSources: 10000}
}

func (h *StreamHub) Publish(sample telemetry.StreamFileTelemetry) {
	h.mu.Lock()
	key := sample.NodeID + "\x00" + sample.Source
	if _, exists := h.latest[key]; !exists && len(h.latest) >= h.maxSources {
		h.mu.Unlock()
		return
	}
	if previous, exists := h.latest[key]; exists && !previous.UpdatedAt.IsZero() && !sample.UpdatedAt.After(previous.UpdatedAt) {
		h.mu.Unlock()
		return
	}
	h.latest[key] = sample
	for _, ch := range h.subs {
		select {
		case ch <- sample:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- sample:
			default:
			}
		}
	}
	h.mu.Unlock()
}

func (h *StreamHub) Snapshot() []telemetry.StreamFileTelemetry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]telemetry.StreamFileTelemetry, 0, len(h.latest))
	for _, sample := range h.latest {
		out = append(out, sample)
	}
	return out
}

func (h *StreamHub) Subscribe() (uint64, <-chan telemetry.StreamFileTelemetry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= h.max {
		return 0, nil, false
	}
	h.next++
	ch := make(chan telemetry.StreamFileTelemetry, 1)
	h.subs[h.next] = ch
	return h.next, ch, true
}

func (h *StreamHub) Unsubscribe(id uint64) {
	h.mu.Lock()
	if ch, ok := h.subs[id]; ok {
		delete(h.subs, id)
		close(ch)
	}
	h.mu.Unlock()
}

// NewTelemetryController uses the same bounded metadata store as the
// controller's live metadata, overview, and time-series APIs.
func NewTelemetryController(store *eventstore.Store) (*TelemetryController, error) {
	if store == nil {
		return nil, fmt.Errorf("telemetry store is required")
	}
	return &TelemetryController{store: store}, nil
}

func validateParseTelemetry(record *telemetry.ParseTelemetry) error {
	if record == nil {
		return errors.New("telemetry is required")
	}
	if record.NodeID == "" || record.ParserID == "" {
		return errors.New("node_id and parser_id are required")
	}
	if record.Confidence < 0 || record.Confidence > 1 || record.EvidenceCoverage < 0 || record.EvidenceCoverage > 1 {
		return errors.New("confidence and evidence_coverage must be between zero and one")
	}
	if record.AttemptCount < 0 || record.FallbackDepth < 0 || record.StageCount < 0 || record.InputFieldCount < 0 || record.OutputFieldCount < 0 || record.LatencyMs < 0 {
		return errors.New("telemetry counters must be non-negative")
	}
	if len(record.NodeID) > 128 || len(record.ParserID) > 128 || len(record.RuleID) > 128 || len(record.EventHash) > 64 || len(record.PolicyHash) > 64 {
		return errors.New("telemetry field exceeds bounds")
	}
	if len(record.OrderedAttempts) > 20 || len(record.FieldEvidence) > 512 || len(record.ErrorCodes) > 10 || len(record.DeliveryAcks) > 32 {
		return errors.New("telemetry collection exceeds bounds")
	}
	for _, attempt := range record.OrderedAttempts {
		if len(attempt.StageResults) > 64 {
			return errors.New("telemetry stage collection exceeds bounds")
		}
	}
	for _, hash := range []string{record.EventHash, record.PolicyHash} {
		if hash != "" && (len(hash) != 64 || !isHex(hash)) {
			return errors.New("telemetry hashes must be 64 hexadecimal characters")
		}
	}
	return nil
}
func isHex(value string) bool {
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}

func (tc *TelemetryController) Decode(body io.Reader) (*telemetry.ParseTelemetry, error) {
	decoder := json.NewDecoder(io.LimitReader(body, (1<<20)+1))
	decoder.DisallowUnknownFields()
	var record telemetry.ParseTelemetry
	if err := decoder.Decode(&record); err != nil {
		return nil, fmt.Errorf("invalid telemetry: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("telemetry body must contain one JSON object")
	}
	if err := validateParseTelemetry(&record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (tc *TelemetryController) Store(record *telemetry.ParseTelemetry) (eventstore.Record, error) {
	if record.ProcessedAt.IsZero() {
		record.ProcessedAt = time.Now().UTC()
	}
	return tc.store.Append(record.NodeID, eventstore.TelemetryMetadata{EventID: record.EventID, EventHash: record.EventHash, Service: "telemetry", ParserID: record.ParserID, RuleID: record.RuleID, Route: record.Route, Confidence: record.Confidence, EvidenceCoverage: record.EvidenceCoverage, AttemptCount: record.AttemptCount, FallbackDepth: record.FallbackDepth, FieldCount: record.OutputFieldCount, LatencyMs: record.LatencyMs, Result: record.Result, ErrorCodes: record.ErrorCodes, Timestamp: record.ProcessedAt})
}

func (tc *TelemetryController) GetTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 1000 {
			jsonError(w, "limit must be 1..1000", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	query := eventstore.Query{NodeID: r.URL.Query().Get("node_id"), Service: "telemetry", Parser: r.URL.Query().Get("parser_id"), Route: r.URL.Query().Get("route"), Search: r.URL.Query().Get("search"), Limit: limit}
	if after := r.URL.Query().Get("after"); after != "" {
		value, err := time.Parse(time.RFC3339, after)
		if err != nil {
			jsonError(w, "after must be RFC3339", http.StatusBadRequest)
			return
		}
		query.After = value
	}
	jsonOK(w, map[string]any{"telemetry": tc.store.Query(query), "bounded": true, "normal_payload_retained": false})
}
func (tc *TelemetryController) GetTelemetryStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	stats := tc.store.Stats()
	stats["normal_payload_retained"] = false
	jsonOK(w, stats)
}

func (h *StreamHub) SnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	jsonOK(w, map[string]any{"files": h.Snapshot(), "bounded": true})
}

func (h *StreamHub) SSEHandler(w http.ResponseWriter, r *http.Request) {
	h.serveSSE(w, r, nil)
}

// SSEHandlerWithEvents multiplexes bounded stream samples and safe controller
// metadata events. Event payloads are filtered by wsrelay before subscription.
func (h *StreamHub) SSEHandlerWithEvents(events *wsrelay.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { h.serveSSE(w, r, events) }
}

func (h *StreamHub) serveSSE(w http.ResponseWriter, r *http.Request, events *wsrelay.Hub) {
	if r.Method != http.MethodGet {
		jsonError(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	id, ch, ok := h.Subscribe()
	if !ok {
		http.Error(w, "subscriber limit reached", http.StatusServiceUnavailable)
		return
	}
	defer h.Unsubscribe(id)
	var eventID uint64
	var eventCh <-chan []byte
	if events != nil {
		var subscribed bool
		eventID, eventCh, subscribed = events.SubscribeSafe()
		if !subscribed {
			http.Error(w, "subscriber limit reached", http.StatusServiceUnavailable)
			return
		}
		defer events.UnsubscribeSafe(eventID)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	enc := json.NewEncoder(w)
	if !writeSSE(w, flusher, enc, strconv.FormatUint(h.sequence.Add(1), 10), "snapshot", map[string]any{"files": h.Snapshot()}) {
		return
	}
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case sample, open := <-ch:
			if !open {
				return
			}
			if !writeSSE(w, flusher, enc, strconv.FormatUint(h.sequence.Add(1), 10), "telemetry", sample) {
				return
			}
		case message, open := <-eventCh:
			if !open {
				return
			}
			var event struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(message, &event) != nil || event.Type == "" {
				continue
			}
			if !writeSSERaw(w, flusher, strconv.FormatUint(h.sequence.Add(1), 10), event.Type, message) {
				return
			}
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w io.Writer, flusher http.Flusher, enc *json.Encoder, id, event string, sample any) bool {
	if _, err := io.WriteString(w, "id: "+id+"\nevent: "+event+"\ndata: "); err != nil {
		return false
	}
	if err := enc.Encode(sample); err != nil {
		return false
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func writeSSERaw(w io.Writer, flusher http.Flusher, id, event string, payload []byte) bool {
	if _, err := io.WriteString(w, "id: "+id+"\nevent: "+event+"\ndata: "); err != nil {
		return false
	}
	if _, err := w.Write(payload); err != nil {
		return false
	}
	if _, err := io.WriteString(w, "\n\n"); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// Compatibility handlers are intentionally unauthenticated only when called
// directly in unit tests. Production routing performs authorization first.
func (tc *TelemetryController) PostTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	record, err := tc.Decode(r.Body)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "too large") {
			code = http.StatusRequestEntityTooLarge
		}
		jsonError(w, err.Error(), code)
		return
	}
	stored, err := tc.Store(record)
	if err != nil {
		jsonError(w, "storage error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]any{"status": "accepted", "event_id": stored.EventID})
}
