// Package quarantine stores the exact source text needed for an operator
// approved parser retry. Records remain local to the client unless an operator
// explicitly exports the raw field.
package quarantine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Record struct {
	ID             string    `json:"id"`
	NodeID         string    `json:"node_id"`
	Service        string    `json:"service"`
	EventHash      string    `json:"event_hash"`
	Raw            string    `json:"raw"`
	FirstSeen      time.Time `json:"first_seen"`
	LastAttempt    time.Time `json:"last_attempt"`
	AttemptCount   int       `json:"attempt_count"`
	Status         string    `json:"status"`
	ErrorCodes     []string  `json:"error_codes,omitempty"`
	ParserRevision string    `json:"parser_revision,omitempty"`
	SourcePath     string    `json:"source_path,omitempty"`
	SourceLine     uint64    `json:"source_line,omitempty"`
}

// New creates a waiting record. The stable ID includes source location so
// identical lines from separate files or positions remain independently
// actionable while event_hash remains useful for deduplication.
func New(nodeID, service, sourcePath string, sourceLine uint64, raw, parserRevision string, attempts int, errorCodes []string, at time.Time) Record {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	hash := sha256.Sum256([]byte(raw))
	eventHash := hex.EncodeToString(hash[:])
	if attempts < 1 {
		attempts = 1
	}
	// Do not include wall-clock time. A service restart after a downstream
	// delivery failure must produce the same record ID for the same source
	// position, otherwise the queue and controller would treat one input line
	// as several distinct operator actions.
	identity := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", nodeID, sourcePath, sourceLine, eventHash)))
	return Record{
		ID: "q-" + hex.EncodeToString(identity[:16]), NodeID: nodeID, Service: service,
		EventHash: eventHash, Raw: raw, FirstSeen: at.UTC(), LastAttempt: at.UTC(),
		AttemptCount: attempts, Status: "waiting", ErrorCodes: append([]string(nil), errorCodes...),
		ParserRevision: parserRevision, SourcePath: sourcePath, SourceLine: sourceLine,
	}
}

// Append writes one complete JSON object and synchronizes it before returning,
// allowing the caller to advance its input cursor only after durable retention.
func Append(path string, record Record) error {
	if path == "" {
		return fmt.Errorf("quarantine path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(record); err != nil {
		return err
	}
	return file.Sync()
}
