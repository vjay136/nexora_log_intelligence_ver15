// Package output implements a ClickHouse HTTP JSONEachRow destination.
package output

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

var clickHouseIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// ClickHouseWriter batches normalized envelopes and inserts them through the
// ClickHouse HTTP interface. It needs no native client library or CGO.
type ClickHouseWriter struct {
	cfg          Config
	client       *http.Client
	mu           sync.Mutex
	flushMu      sync.Mutex
	batch        []batchItem // Store route with each item
	ticker       *time.Ticker
	done         chan struct{}
	closeOnce    sync.Once
	initialized  bool
	knownColumns map[string]bool
}

// batchItem stores EventPayload data with its route
type batchItem struct {
	data  []byte
	route string
}

// NewClickHouseWriter validates configuration and starts periodic flushing.
func NewClickHouseWriter(cfg Config) (*ClickHouseWriter, error) {
	if cfg.URL == "" {
		cfg.URL = cfg.DSN
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("clickhouse output requires an http(s) url")
	}
	client, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Database == "" {
		cfg.Database = "default"
	}
	if cfg.Table == "" {
		cfg.Table = "nli_logs"
	}
	if !clickHouseIdentifier.MatchString(cfg.Database) || !clickHouseIdentifier.MatchString(cfg.Table) {
		return nil, fmt.Errorf("invalid ClickHouse database or table identifier")
	}
	if len(cfg.FieldColumns) > 256 {
		return nil, fmt.Errorf("clickhouse field_columns exceeds the 256-field limit")
	}
	for source, target := range cfg.FieldColumns {
		if strings.TrimSpace(source) == "" || !clickHouseIdentifier.MatchString(target) || reservedClickHouseColumn(target) {
			return nil, fmt.Errorf("invalid ClickHouse field mapping %q -> %q", source, target)
		}
	}
	if cfg.MaxFieldColumns <= 0 || cfg.MaxFieldColumns > 256 {
		cfg.MaxFieldColumns = 256
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 250
	}
	if cfg.FlushSec <= 0 {
		cfg.FlushSec = 5
	}
	writer := &ClickHouseWriter{
		cfg:          cfg,
		client:       client,
		knownColumns: map[string]bool{"received_at": true, "route": true, "event_json": true, "event_sha256": true},
		ticker:       time.NewTicker(time.Duration(cfg.FlushSec) * time.Second),
		done:         make(chan struct{}),
	}
	if !cfg.Synchronous {
		go writer.flushLoop()
	}
	return writer, nil
}

func (w *ClickHouseWriter) Write(envelope []byte, route string) error {
	if !acceptsRemoteRoute(route) || !acceptsRoutes(w.cfg.Routes, route) {
		return nil
	}
	copyOfEnvelope := append([]byte(nil), envelope...)
	w.mu.Lock()
	w.batch = append(w.batch, batchItem{data: copyOfEnvelope, route: route})
	full := len(w.batch) >= w.cfg.BatchSize
	w.mu.Unlock()
	if full {
		return w.flush()
	}
	return nil
}

func (w *ClickHouseWriter) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		w.ticker.Stop()
	})
	return w.flush()
}

func (w *ClickHouseWriter) flushLoop() {
	for {
		select {
		case <-w.ticker.C:
			_ = w.flush()
		case <-w.done:
			return
		}
	}
}

func (w *ClickHouseWriter) Flush() error { return w.flush() }

func (w *ClickHouseWriter) flush() error {
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

	if !w.initialized {
		if err := w.ensureTable(); err != nil {
			return w.handleFailure(batch, err)
		}
		w.initialized = true
	}

	var body bytes.Buffer
	rows := make([]map[string]interface{}, 0, len(batch))
	columns := map[string]bool{}
	buffer := bufio.NewWriter(&body)
	for _, item := range batch {
		row, err := buildClickHouseRowWithMapping(item.data, item.route, w.cfg.FieldColumns, w.cfg.MaxFieldColumns)
		if err != nil {
			return w.handleFailure(batch, err)
		}
		for name := range row {
			if !reservedClickHouseColumn(name) {
				columns[name] = true
			}
		}
		rows = append(rows, row)
	}
	if err := w.ensureColumns(columns); err != nil {
		return w.handleFailure(batch, err)
	}
	for _, row := range rows {
		encoded, _ := json.Marshal(row)
		buffer.Write(encoded)
		buffer.WriteByte('\n')
	}
	buffer.Flush()

	query := fmt.Sprintf("INSERT INTO %s.%s FORMAT JSONEachRow", w.cfg.Database, w.cfg.Table)
	if err := w.postQuery(query, &body); err != nil {
		return w.handleFailure(batch, err)
	}
	return nil
}

func (w *ClickHouseWriter) ensureColumns(columns map[string]bool) error {
	names := make([]string, 0, len(columns))
	for name := range columns {
		if !w.knownColumns[name] {
			names = append(names, name)
		}
	}
	// Stable ordering makes request traces and release tests deterministic.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		query := fmt.Sprintf("ALTER TABLE %s.%s ADD COLUMN IF NOT EXISTS `%s` Nullable(String)", w.cfg.Database, w.cfg.Table, name)
		if err := w.postQuery(query, nil); err != nil {
			return err
		}
		w.knownColumns[name] = true
	}
	return nil
}

func (w *ClickHouseWriter) requeue(batch []batchItem) {
	w.mu.Lock()
	w.batch = append(batch, w.batch...)
	w.mu.Unlock()
}

func (w *ClickHouseWriter) ensureTable() error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.%s (
 received_at DateTime64(9, 'UTC'),
 route LowCardinality(String),
 event_json String,
 event_sha256 FixedString(64)
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(received_at)
ORDER BY (route, received_at)`, w.cfg.Database, w.cfg.Table)
	return w.postQuery(query, nil)
}

func (w *ClickHouseWriter) postQuery(query string, body *bytes.Buffer) error {
	endpoint, _ := url.Parse(strings.TrimRight(w.cfg.URL, "/"))
	values := endpoint.Query()
	values.Set("query", query)
	endpoint.RawQuery = values.Encode()
	return post(w.client, w.cfg, func() (*http.Request, error) {
		var payload *bytes.Reader
		if body == nil {
			payload = bytes.NewReader(nil)
		} else {
			payload = bytes.NewReader(body.Bytes())
		}
		req, err := http.NewRequest(http.MethodPost, endpoint.String(), payload)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-ndjson")
		return req, nil
	})
}

// ClickHouse row structure with stable schema for v5 EventPayload
type clickHouseRow struct {
	ReceivedAt time.Time `json:"received_at"`
	Route      string    `json:"route"`
	EventJSON  string    `json:"event_json"` // Serialized customer EventPayload
}

func buildClickHouseRow(eventPayload []byte, route string) (map[string]interface{}, error) {
	return buildClickHouseRowWithMapping(eventPayload, route, nil, 256)
}

func buildClickHouseRowWithMapping(eventPayload []byte, route string, mapping map[string]string, maxFields int) (map[string]interface{}, error) {
	// v5: Parse EventPayload to validate and sanitize
	var fields map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(eventPayload))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil {
		return nil, fmt.Errorf("clickhouse eventpayload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("clickhouse eventpayload contains trailing data")
	}

	// Remove any forbidden fields that might have leaked through
	projected := map[string]interface{}{}
	sanitizedFields := make(map[string]interface{}, len(fields))
	used := map[string]string{}
	for key, value := range fields {
		// Only include safe customer fields (exclude raw/nli/meta/source)
		lowerKey := strings.ToLower(key)
		if lowerKey != "raw" && lowerKey != "_raw" && lowerKey != "nli" &&
			lowerKey != "meta" && lowerKey != "source" && !strings.HasPrefix(lowerKey, "_") {
			sanitizedFields[key] = value
			destination, explicitlyMapped := mapping[key]
			if len(mapping) > 0 && !explicitlyMapped {
				continue
			}
			if len(mapping) == 0 {
				destination = safeClickHouseFieldName(key)
			}
			if destination == "" || reservedClickHouseColumn(destination) {
				continue
			}
			if prior, exists := used[destination]; exists && prior != key {
				return nil, fmt.Errorf("ClickHouse field mapping collision: %q and %q both map to %q", prior, key, destination)
			}
			used[destination] = key
			projected[destination] = value
		}
	}
	if len(projected) > maxFields {
		return nil, fmt.Errorf("ClickHouse row has %d customer fields; configured limit is %d", len(projected), maxFields)
	}

	// Serialize sanitized payload as JSON string
	eventJSONBytes, err := json.Marshal(sanitizedFields)
	if err != nil {
		return nil, fmt.Errorf("clickhouse event serialization: %w", err)
	}

	eventHash := sha256.Sum256(eventJSONBytes)
	row := map[string]interface{}{
		"received_at":  time.Now().UTC().Format("2006-01-02 15:04:05.000000000"),
		"route":        route,
		"event_json":   string(eventJSONBytes),
		"event_sha256": fmt.Sprintf("%x", eventHash[:]),
	}
	for key, value := range projected {
		// Store each arbitrary customer value as canonical JSON text in a
		// Nullable(String) column; event_json remains the lossless typed record.
		textValue, err := clickHouseStringValue(value)
		if err != nil {
			return nil, fmt.Errorf("clickhouse field %s: %w", key, err)
		}
		row[key] = textValue
	}

	return row, nil
}

func clickHouseStringValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return typed, nil
	case json.Number:
		return typed.String(), nil
	case bool:
		return fmt.Sprint(typed), nil
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		return string(encoded), nil
	}
}

func safeClickHouseFieldName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		if b.Len() >= 128 {
			break
		}
	}
	result := strings.Trim(b.String(), "_")
	if result == "" {
		return ""
	}
	if result[0] >= '0' && result[0] <= '9' {
		result = "field_" + result
	}
	if reservedClickHouseColumn(result) {
		result = "field_" + result
	}
	return result
}

func reservedClickHouseColumn(name string) bool {
	switch strings.ToLower(name) {
	case "received_at", "route", "event_json", "event_sha256":
		return true
	default:
		return strings.HasPrefix(name, "__")
	}
}

func (w *ClickHouseWriter) handleFailure(batch []batchItem, deliveryErr error) error {
	if w.cfg.FallbackPath != "" {
		// Extract data for spooling (spoolFailedBatch expects [][]byte)
		bytesBatch := make([][]byte, len(batch))
		for i, item := range batch {
			bytesBatch[i] = item.data
		}
		if spoolErr := spoolFailedBatch(w.cfg, bytesBatch); spoolErr == nil {
			return &DeliveryError{Destination: "clickhouse", Accounted: true, Cause: deliveryErr}
		} else {
			deliveryErr = fmt.Errorf("%v; fallback accounting: %w", deliveryErr, spoolErr)
		}
	}
	w.requeue(batch)
	return &DeliveryError{Destination: "clickhouse", Cause: deliveryErr}
}
