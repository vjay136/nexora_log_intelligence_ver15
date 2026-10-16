// Package auditlog implements a bounded-read, append-only, HMAC-chained audit
// trail. Each record authenticates the previous record so deletion, insertion,
// or mutation is detected during startup and verification.
package auditlog

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	Sequence  uint64            `json:"sequence"`
	Timestamp time.Time         `json:"timestamp"`
	Action    string            `json:"action"`
	Actor     string            `json:"actor"`
	Role      string            `json:"role,omitempty"`
	Resource  string            `json:"resource"`
	Outcome   string            `json:"outcome"`
	RemoteIP  string            `json:"remote_ip,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
	Previous  string            `json:"previous_hash,omitempty"`
	Hash      string            `json:"hash"`
}

type Logger interface {
	Append(Event) (Event, error)
	List(limit int) []Event
	Verify() error
	Close() error
}

type ChainLog struct {
	mu     sync.Mutex
	path   string
	key    []byte
	file   *os.File
	events []Event
}

func NewChainLog(path string, key []byte) (*ChainLog, error) {
	if len(key) < 32 {
		return nil, errors.New("audit HMAC key must contain at least 32 bytes")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	log := &ChainLog{path: path, key: append([]byte(nil), key...)}
	if err := log.load(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	log.file = file
	return log, nil
}

func eventHash(key []byte, event Event) (string, error) {
	event.Hash = ""
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(encoded)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (l *ChainLog) load() error {
	file, err := os.Open(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 16<<10), 1<<20)
	var previous string
	var sequence uint64
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("audit record decode: %w", err)
		}
		expected, err := eventHash(l.key, event)
		if err != nil || !hmac.Equal([]byte(expected), []byte(event.Hash)) || event.Previous != previous || event.Sequence != sequence+1 {
			return fmt.Errorf("audit chain verification failed at sequence %d", event.Sequence)
		}
		l.events = append(l.events, event)
		previous = event.Hash
		sequence = event.Sequence
	}
	return scanner.Err()
}

func (l *ChainLog) Append(event Event) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return Event{}, errors.New("audit log closed")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if len(l.events) > 0 {
		event.Sequence = l.events[len(l.events)-1].Sequence + 1
		event.Previous = l.events[len(l.events)-1].Hash
	} else {
		event.Sequence = 1
	}
	if event.Actor == "" {
		event.Actor = "anonymous"
	}
	if event.Outcome == "" {
		event.Outcome = "success"
	}
	hash, err := eventHash(l.key, event)
	if err != nil {
		return Event{}, err
	}
	event.Hash = hash
	encoded, err := json.Marshal(event)
	if err != nil {
		return Event{}, err
	}
	if _, err = l.file.Write(append(encoded, '\n')); err != nil {
		return Event{}, err
	}
	if err = l.file.Sync(); err != nil {
		return Event{}, err
	}
	l.events = append(l.events, event)
	return event, nil
}

func (l *ChainLog) List(limit int) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	start := len(l.events) - limit
	if start < 0 {
		start = 0
	}
	out := make([]Event, len(l.events)-start)
	copy(out, l.events[start:])
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (l *ChainLog) Verify() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var previous string
	for index, event := range l.events {
		expected, err := eventHash(l.key, event)
		if err != nil || !hmac.Equal([]byte(expected), []byte(event.Hash)) || event.Previous != previous || event.Sequence != uint64(index+1) {
			return fmt.Errorf("audit chain invalid at sequence %d", event.Sequence)
		}
		previous = event.Hash
	}
	return nil
}

// LogAuditEvent adapts middleware-shaped audit events through JSON so auditlog
// remains independent from HTTP middleware and cannot form an import cycle.
func (l *ChainLog) LogAuditEvent(value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var legacy LegacyEvent
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	outcome := "failure"
	if legacy.Success {
		outcome = "success"
	}
	_, err = l.Append(Event{Timestamp: legacy.Timestamp, Action: legacy.Action, Actor: legacy.Username, Role: legacy.Role, Resource: legacy.Resource, Outcome: outcome, RemoteIP: legacy.RemoteIP, RequestID: legacy.RequestID, Details: map[string]string{"detail": legacy.Error}})
	return err
}

func (l *ChainLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// LegacyEvent and adapters keep older validation callers source-compatible
// without reintroducing the auditlog<->middleware import cycle.
type LegacyEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	RemoteIP  string    `json:"remote_ip"`
	UserAgent string    `json:"user_agent"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	RequestID string    `json:"request_id"`
}

type MemoryAuditLogger struct {
	mu     sync.RWMutex
	events []LegacyEvent
}

func NewMemoryAuditLogger() *MemoryAuditLogger { return &MemoryAuditLogger{} }
func (m *MemoryAuditLogger) LogAuditEvent(value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var event LegacyEvent
	if err = json.Unmarshal(data, &event); err != nil {
		return err
	}
	m.mu.Lock()
	m.events = append(m.events, event)
	m.mu.Unlock()
	return nil
}
func (m *MemoryAuditLogger) GetEvents() []LegacyEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]LegacyEvent, len(m.events))
	copy(out, m.events)
	return out
}
func (m *MemoryAuditLogger) Clear() { m.mu.Lock(); m.events = nil; m.mu.Unlock() }

type FileAuditLogger struct{ chain *ChainLog }

func NewFileAuditLogger(path string) (*FileAuditLogger, error) {
	key := sha256.Sum256([]byte("compatibility:" + path))
	chain, err := NewChainLog(path, key[:])
	if err != nil {
		return nil, err
	}
	return &FileAuditLogger{chain: chain}, nil
}
func (f *FileAuditLogger) LogAuditEvent(value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var legacy LegacyEvent
	if err = json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	outcome := "failure"
	if legacy.Success {
		outcome = "success"
	}
	_, err = f.chain.Append(Event{Timestamp: legacy.Timestamp, Action: legacy.Action, Actor: legacy.Username, Role: legacy.Role, Resource: legacy.Resource, Outcome: outcome, RemoteIP: legacy.RemoteIP, RequestID: legacy.RequestID, Details: map[string]string{"error": legacy.Error}})
	return err
}
func (f *FileAuditLogger) Close() error { return f.chain.Close() }
