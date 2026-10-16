// Package eventstore persists only bounded parser metadata. It intentionally has
// no field capable of storing raw logs or emitted customer JSON.
package eventstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxRecordLine = 64 << 10

type Record struct {
	NodeID           string    `json:"node_id"`
	ReceivedAt       time.Time `json:"received_at"`
	Timestamp        time.Time `json:"timestamp"`
	Service          string    `json:"service"`
	Parser           string    `json:"parser"`
	Rule             string    `json:"rule,omitempty"`
	Route            string    `json:"route"`
	EventID          string    `json:"event_id,omitempty"`
	EventHash        string    `json:"event_hash,omitempty"`
	Confidence       float64   `json:"confidence,omitempty"`
	EvidenceCoverage float64   `json:"evidence_coverage,omitempty"`
	AttemptCount     int       `json:"attempt_count,omitempty"`
	FallbackDepth    int       `json:"fallback_depth,omitempty"`
	FieldCount       int       `json:"field_count,omitempty"`
	LatencyMs        int64     `json:"latency_ms,omitempty"`
	Result           string    `json:"result,omitempty"`
	ErrorCodes       []string  `json:"error_codes,omitempty"`
}

type Query struct {
	NodeID  string
	Service string
	Parser  string
	Route   string
	Search  string
	After   time.Time
	Limit   int
}

type TelemetryMetadata struct {
	EventID          string
	EventHash        string
	Service          string
	ParserID         string
	RuleID           string
	Route            string
	Confidence       float64
	EvidenceCoverage float64
	AttemptCount     int
	FallbackDepth    int
	FieldCount       int
	LatencyMs        int64
	Result           string
	ErrorCodes       []string
	Timestamp        time.Time
}

type Store struct {
	mu      sync.RWMutex
	path    string
	max     int
	records []Record
}

func New(path string, maxRecords int) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("event metadata path is required")
	}
	if maxRecords <= 0 {
		maxRecords = 10000
	}
	if maxRecords > 1_000_000 {
		return nil, errors.New("event retention exceeds safety limit")
	}
	store := &Store{path: path, max: maxRecords}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func bounded(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}

func (s *Store) Append(nodeID string, metadata TelemetryMetadata) (Record, error) {
	nodeID = bounded(nodeID, 128)
	if nodeID == "" {
		return Record{}, errors.New("node id is required")
	}
	if metadata.Confidence < 0 || metadata.Confidence > 1 || metadata.EvidenceCoverage < 0 || metadata.EvidenceCoverage > 1 {
		return Record{}, errors.New("confidence and evidence coverage must be between zero and one")
	}
	if metadata.AttemptCount < 0 || metadata.FallbackDepth < 0 || metadata.FieldCount < 0 || metadata.LatencyMs < 0 {
		return Record{}, errors.New("telemetry counters must be non-negative")
	}
	now := time.Now().UTC()
	record := Record{
		NodeID: nodeID, ReceivedAt: now, Timestamp: metadata.Timestamp,
		Service: bounded(metadata.Service, 128), Parser: bounded(metadata.ParserID, 128),
		Rule: bounded(metadata.RuleID, 128), Route: bounded(metadata.Route, 32),
		EventID: bounded(metadata.EventID, 128), EventHash: bounded(metadata.EventHash, 64),
		Confidence: metadata.Confidence, EvidenceCoverage: metadata.EvidenceCoverage,
		AttemptCount: metadata.AttemptCount, FallbackDepth: metadata.FallbackDepth,
		FieldCount: metadata.FieldCount, LatencyMs: metadata.LatencyMs,
		Result: bounded(metadata.Result, 32),
	}
	if record.Service == "" {
		record.Service = "unknown"
	}
	if record.Parser == "" {
		record.Parser = "unknown"
	}
	if record.Route == "" {
		record.Route = "unknown"
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = now
	}
	for _, code := range metadata.ErrorCodes {
		if len(record.ErrorCodes) == 10 {
			break
		}
		if code = bounded(code, 64); code != "" {
			record.ErrorCodes = append(record.ErrorCodes, code)
		}
	}
	encoded, err := json.Marshal(record)
	if err != nil || len(encoded) > maxRecordLine {
		return Record{}, errors.New("telemetry metadata record exceeds bounds")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return Record{}, err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return Record{}, err
	}
	_, writeErr := file.Write(append(encoded, '\n'))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return Record{}, writeErr
	}
	if closeErr != nil {
		return Record{}, closeErr
	}
	s.records = append(s.records, record)
	if len(s.records) > s.max {
		s.records = append([]Record(nil), s.records[len(s.records)-s.max:]...)
		if err := s.compactLocked(); err != nil {
			return Record{}, err
		}
	}
	return record, nil
}

func (s *Store) compactLocked() error {
	tmp := s.path + ".compact"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	for _, record := range s.records {
		if err := encoder.Encode(record); err != nil {
			file.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Query(query Query) []Record {
	if query.Limit <= 0 || query.Limit > 1000 {
		query.Limit = 100
	}
	search := strings.ToLower(query.Search)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, 0, query.Limit)
	for index := len(s.records) - 1; index >= 0 && len(out) < query.Limit; index-- {
		record := s.records[index]
		if query.NodeID != "" && record.NodeID != query.NodeID {
			continue
		}
		if query.Service != "" && record.Service != query.Service {
			continue
		}
		if query.Parser != "" && record.Parser != query.Parser {
			continue
		}
		if query.Route != "" && record.Route != query.Route {
			continue
		}
		if !query.After.IsZero() && record.Timestamp.Before(query.After) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(record.Service+" "+record.Parser+" "+record.Rule+" "+record.Route+" "+record.EventID), search) {
			continue
		}
		copyRecord := record
		copyRecord.ErrorCodes = append([]string(nil), record.ErrorCodes...)
		out = append(out, copyRecord)
	}
	return out
}

// Window returns a chronological copy of all retained metadata in a bounded
// time interval. The store itself has a hard record retention cap, so callers
// can aggregate dashboards without an unbounded query or exposing event data.
func (s *Store) Window(after, before time.Time, nodeID string) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, 0)
	for _, record := range s.records {
		if !after.IsZero() && record.Timestamp.Before(after) || !before.IsZero() && record.Timestamp.After(before) {
			continue
		}
		if nodeID != "" && record.NodeID != nodeID {
			continue
		}
		copyRecord := record
		copyRecord.ErrorCodes = append([]string(nil), record.ErrorCodes...)
		out = append(out, copyRecord)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out
}

func (s *Store) Stats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	routes, services, parsers, results := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	cutoff, lastHour, attempts, fallbacks := time.Now().Add(-time.Hour), 0, 0, 0
	for _, record := range s.records {
		routes[record.Route]++
		services[record.Service]++
		parsers[record.Parser]++
		results[record.Result]++
		attempts += record.AttemptCount
		fallbacks += record.FallbackDepth
		if record.ReceivedAt.After(cutoff) {
			lastHour++
		}
	}
	return map[string]interface{}{
		"retained_metadata": len(s.records), "retained_events": len(s.records), "max_records": s.max,
		"events_last_hour": lastHour, "routes": routes, "services": services, "parsers": parsers,
		"results": results, "parser_attempts": attempts, "fallback_depth_total": fallbacks,
	}
}

func (s *Store) load() error {
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 16<<10), maxRecordLine)
	for scanner.Scan() {
		var record Record
		if json.Unmarshal(scanner.Bytes(), &record) == nil && record.NodeID != "" {
			s.records = append(s.records, record)
			if len(s.records) > s.max {
				s.records = s.records[1:]
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}
	info, statErr := file.Stat()
	if statErr == nil && (len(s.records) == s.max || info.Size() > int64(s.max*maxRecordLine)) {
		s.mu.Lock()
		compactErr := s.compactLocked()
		s.mu.Unlock()
		return compactErr
	}
	return nil
}

func (s *Store) Services() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	for _, record := range s.records {
		seen[record.Service] = true
	}
	out := make([]string, 0, len(seen))
	for service := range seen {
		out = append(out, service)
	}
	sort.Strings(out)
	return out
}
