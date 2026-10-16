// Package output implements multi-destination log output routing.
package output

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config defines one output destination.
type Config struct {
	Synchronous     bool              `json:"-"`                  // Router flushes before checkpointing
	Type            string            `json:"type"`               // file, loki, clickhouse, controller, stdout
	Routes          []string          `json:"routes,omitempty"`   // nil = all routes
	Families        []string          `json:"families,omitempty"` // optional input/parser families routed here
	Path            string            `json:"path,omitempty"`
	PathTemplate    string            `json:"path_template,omitempty"` // file: absolute path containing exactly one {hour_utc} token
	MaxMB           int64             `json:"max_mb,omitempty"`
	MaxBackups      int               `json:"max_backups,omitempty"`
	URL             string            `json:"url,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	BatchSize       int               `json:"batch_size,omitempty"`
	FlushSec        int               `json:"flush_sec,omitempty"`
	DSN             string            `json:"dsn,omitempty"`
	Database        string            `json:"database,omitempty"`
	Table           string            `json:"table,omitempty"`
	FieldColumns    map[string]string `json:"field_columns,omitempty"` // source field -> ClickHouse column; unmapped safe fields retain their name
	MaxFieldColumns int               `json:"max_field_columns,omitempty"`
	Username        string            `json:"username,omitempty"`
	PasswordEnv     string            `json:"password_env,omitempty"`
	BearerTokenEnv  string            `json:"bearer_token_env,omitempty"`
	TenantID        string            `json:"tenant_id,omitempty"` // Loki multi-tenancy
	CAFile          string            `json:"ca_file,omitempty"`   // Additional trusted PEM CA
	TokenFile       string            `json:"token_file,omitempty"`
	Writer          Writer            `json:"-"` // For test injection only
	NodeID          string            `json:"node_id,omitempty"`
	NodeIDFile      string            `json:"node_id_file,omitempty"`
	FallbackPath    string            `json:"fallback_path,omitempty"`
	Required        bool              `json:"required,omitempty"`
	RetryMax        int               `json:"retry_max,omitempty"`
	RetryBackoffMS  int               `json:"retry_backoff_ms,omitempty"`
	Format          string            `json:"format,omitempty"` // stdout: json|pretty
}

// Writer sends a parsed envelope to a destination.
type Writer interface {
	Write(envelope []byte, route string) error
	Close() error
}

// DeliveryError reports a remote delivery failure and whether a hash-only
// accounting receipt was durably recorded. Callers must not treat it as a
// successful destination acknowledgement.
type DeliveryError struct {
	Destination string
	Accounted   bool
	Cause       error
}

func (err *DeliveryError) Error() string {
	if err.Accounted {
		return fmt.Sprintf("%s delivery failed (accounted): %v", err.Destination, err.Cause)
	}
	return fmt.Sprintf("%s delivery failed: %v", err.Destination, err.Cause)
}

func (err *DeliveryError) Unwrap() error { return err.Cause }

// NewWriter creates a Writer based on the config type
func NewWriter(cfg Config) (Writer, error) {
	switch cfg.Type {
	case "file", "":
		return NewFileWriter(cfg)
	case "stdout":
		return NewStdoutWriter(cfg), nil
	case "discard":
		return NewDiscardWriter(cfg), nil
	case "telemetry":
		return NewTelemetryWriter(cfg)
	case "captured":
		if cfg.Writer != nil {
			return cfg.Writer, nil
		}
		return nil, fmt.Errorf("captured writer requires Writer field")
	default:
		return nil, fmt.Errorf("unsupported writer type: %s", cfg.Type)
	}
}

// FileWriter writes JSONL to a file with size cap.
type FileWriter struct {
	cfg         Config
	file        *os.File
	buffer      *bufio.Writer
	currentPath string
	currentSize int64 // includes bytes accepted into buffer
	now         func() time.Time
	mu          sync.Mutex
}

const fileWriterBufferSize = 256 * 1024

// NewFileWriter opens (creates if needed) the output file.
func NewFileWriter(cfg Config) (*FileWriter, error) {
	w := &FileWriter{cfg: cfg, now: time.Now}
	if _, err := w.ensureCurrentFile(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *FileWriter) Write(envelope []byte, route string) error {
	if !w.acceptsRoute(route) {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	path, err := w.ensureCurrentFile()
	if err != nil {
		return err
	}
	maxBytes := w.cfg.MaxMB * 1024 * 1024
	if maxBytes > 0 && w.currentSize+int64(len(envelope))+1 > maxBytes {
		if err := w.rotate(path); err != nil {
			return fmt.Errorf("rotate output %s: %w", path, err)
		}
	}
	n, err := w.buffer.Write(envelope)
	w.currentSize += int64(n)
	if err != nil {
		return err
	}
	if err := w.buffer.WriteByte('\n'); err != nil {
		return err
	}
	w.currentSize++
	return nil
}

func (w *FileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.flushAndSyncLocked()
	if closeErr := w.file.Close(); err == nil {
		err = closeErr
	}
	w.file = nil
	w.buffer = nil
	return err
}

func (w *FileWriter) acceptsRoute(route string) bool {
	if len(w.cfg.Routes) == 0 {
		return true
	}
	for _, r := range w.cfg.Routes {
		if r == route {
			return true
		}
	}
	return false
}

// StdoutWriter writes to stdout.
type StdoutWriter struct {
	cfg Config
	mu  sync.Mutex
}

func NewStdoutWriter(cfg Config) *StdoutWriter { return &StdoutWriter{cfg: cfg} }

func (w *StdoutWriter) Write(envelope []byte, route string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cfg.Format == "pretty" {
		var v interface{}
		if err := json.Unmarshal(envelope, &v); err == nil {
			pretty, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println(string(pretty))
			return nil
		}
	}
	fmt.Println(string(envelope))
	return nil
}

func (w *StdoutWriter) Close() error { return nil }

// DiscardWriter discards all output (for telemetry when not configured).
type DiscardWriter struct {
	cfg Config
}

func NewDiscardWriter(cfg Config) *DiscardWriter { return &DiscardWriter{cfg: cfg} }

func (w *DiscardWriter) Write(envelope []byte, route string) error {
	// Discard all output - used for default telemetry routing
	return nil
}

func (w *DiscardWriter) Close() error { return nil }

// Router dispatches envelopes to multiple writers based on route.
type Router struct {
	writers  []Writer
	families [][]string
}

// NewRouter creates a router from a list of output configs.
func NewRouter(configs []Config) (*Router, error) {
	router := &Router{}
	for _, cfg := range configs {
		cfg.Synchronous = true
		var writer Writer
		var err error
		switch cfg.Type {
		case "file":
			writer, err = NewFileWriter(cfg)
		case "loki":
			if !cfg.Required && cfg.FallbackPath == "" {
				err = fmt.Errorf("optional Loki output requires fallback_path")
			} else {
				writer, err = NewLokiWriter(cfg)
			}
		case "clickhouse":
			if !cfg.Required && cfg.FallbackPath == "" {
				err = fmt.Errorf("optional ClickHouse output requires fallback_path")
			} else {
				writer, err = NewClickHouseWriter(cfg)
			}
		case "controller":
			if !cfg.Required && cfg.FallbackPath == "" {
				err = fmt.Errorf("optional controller output requires fallback_path")
			} else {
				writer, err = NewControllerWriter(cfg)
			}
		case "stdout":
			writer = NewStdoutWriter(cfg)
		case "discard":
			writer = NewDiscardWriter(cfg)
		case "telemetry":
			writer, err = NewTelemetryWriter(cfg)
		default:
			err = fmt.Errorf("unknown output type: %s", cfg.Type)
		}
		if err != nil {
			_ = router.Close()
			return nil, fmt.Errorf("output %s %q: %w", cfg.Type, cfg.Path, err)
		}
		router.writers = append(router.writers, writer)
		router.families = append(router.families, append([]string(nil), cfg.Families...))
	}
	return router, nil
}

// Flush acknowledges buffered destinations before an input cursor is persisted.
func (r *Router) Flush() error {
	var failures []error
	for _, writer := range r.writers {
		if flusher, ok := writer.(interface{ Flush() error }); ok {
			failures = append(failures, flusher.Flush())
		}
	}
	return errors.Join(failures...)
}

// Flush makes the file durable before the input offset advances.
func (w *FileWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.ensureCurrentFile(); err != nil {
		return err
	}
	return w.flushAndSyncLocked()
}

func (w *FileWriter) flushAndSyncLocked() error {
	if w.file == nil {
		return nil
	}
	if w.buffer != nil {
		if err := w.buffer.Flush(); err != nil {
			return err
		}
	}
	return w.file.Sync()
}

// Write sends an envelope to all applicable writers.
func (r *Router) Write(envelope []byte, route string) error {
	return r.WriteForFamily(envelope, route, "")
}

// WriteForFamily sends an envelope only to destinations for the selected
// input/parser family. Destinations without a families filter remain global.
func (r *Router) WriteForFamily(envelope []byte, route, family string) error {
	var failures []error
	for index, writer := range r.writers {
		if index < len(r.families) && !acceptsFamily(r.families[index], family) {
			continue
		}
		if err := writer.Write(envelope, route); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func acceptsFamily(allowed []string, family string) bool {
	if len(allowed) == 0 {
		return true
	}
	family = strings.ToLower(strings.TrimSpace(family))
	if family == "" {
		return false
	}
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(candidate), family) {
			return true
		}
	}
	return false
}

// Close closes all writers.
func (r *Router) Close() error {
	var last error
	for _, w := range r.writers {
		if err := w.Close(); err != nil {
			last = err
		}
	}
	return last
}

var fallbackSpoolMu sync.Mutex

func acceptsRoutes(routes []string, route string) bool {
	if len(routes) == 0 {
		return true
	}
	for _, accepted := range routes {
		if accepted == route {
			return true
		}
	}
	return false
}

// acceptsRemoteRoute is the final privacy boundary for network destinations.
// Only fully parsed normal and warning events may leave the client. Quarantine
// and unknown records remain in the local retryable quarantine store.
func acceptsRemoteRoute(route string) bool {
	return route == "normal" || route == "warning"
}

// deliveryFailureReceipt accounts for an undelivered record without retaining
// raw text or the parsed EventPayload in client state.
type deliveryFailureReceipt struct {
	EventSHA256 string    `json:"event_sha256"`
	Bytes       int       `json:"bytes"`
	Destination string    `json:"destination"`
	Status      string    `json:"status"`
	RecordedAt  time.Time `json:"recorded_at"`
}

func spoolFailedBatch(cfg Config, records [][]byte) error {
	if cfg.FallbackPath == "" {
		return fmt.Errorf("%s output failed and no fallback_path is configured", cfg.Type)
	}
	fallbackSpoolMu.Lock()
	defer fallbackSpoolMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(cfg.FallbackPath), 0750); err != nil {
		return err
	}
	file, err := os.OpenFile(cfg.FallbackPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, record := range records {
		hash := sha256.Sum256(record)
		receipt := deliveryFailureReceipt{
			EventSHA256: fmt.Sprintf("%x", hash[:]), Bytes: len(record),
			Destination: cfg.Type, Status: "delivery_failed", RecordedAt: time.Now().UTC(),
		}
		if err := encoder.Encode(receipt); err != nil {
			return err
		}
	}
	return file.Sync()
}

func (w *FileWriter) rotate(path string) error {
	if err := w.flushAndSyncLocked(); err != nil {
		return err
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	w.buffer = nil
	backups := w.cfg.MaxBackups
	if backups <= 0 {
		backups = 5
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups))
	for index := backups - 1; index >= 1; index-- {
		from := fmt.Sprintf("%s.%d", path, index)
		to := fmt.Sprintf("%s.%d", path, index+1)
		if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(path, path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	w.file = file
	w.buffer = bufio.NewWriterSize(file, fileWriterBufferSize)
	w.currentSize = 0
	return nil
}

func (w *FileWriter) ensureCurrentFile() (string, error) {
	path, err := resolveFilePath(w.cfg, w.now().UTC())
	if err != nil {
		return "", err
	}
	if w.file != nil && path == w.currentPath {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return "", err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return "", err
	}
	if w.file != nil {
		if err := w.flushAndSyncLocked(); err != nil {
			_ = file.Close()
			return "", err
		}
		if closeErr := w.file.Close(); closeErr != nil {
			_ = file.Close()
			return "", closeErr
		}
	}
	w.file = file
	w.buffer = bufio.NewWriterSize(file, fileWriterBufferSize)
	w.currentPath = path
	w.currentSize = info.Size()
	return path, nil
}

func resolveFilePath(cfg Config, now time.Time) (string, error) {
	template := strings.TrimSpace(cfg.PathTemplate)
	if template == "" {
		if strings.TrimSpace(cfg.Path) == "" {
			return "", errors.New("file output requires path or path_template")
		}
		return cfg.Path, nil
	}
	if cfg.Path != "" {
		return "", errors.New("file output cannot set both path and path_template")
	}
	if strings.Count(template, "{hour_utc}") != 1 || strings.ContainsAny(template, "\r\n\x00") || !filepath.IsAbs(template) {
		return "", errors.New("file path_template must be absolute and contain exactly one {hour_utc} token")
	}
	return strings.Replace(template, "{hour_utc}", now.UTC().Format("20060102_1500"), 1), nil
}

// ResolveFilePath returns the concrete file used by a file destination at the
// supplied instant. The agent uses the same resolver when it appends exact raw
// quarantine records, keeping replay storage aligned with hourly routing.
func ResolveFilePath(cfg Config, now time.Time) (string, error) {
	return resolveFilePath(cfg, now)
}
