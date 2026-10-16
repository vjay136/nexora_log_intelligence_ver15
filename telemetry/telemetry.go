// Package telemetry implements the v5 separation of event data from parser telemetry
package telemetry

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// StreamThresholds provides operator-set rate and backpressure warning levels.
type StreamThresholds struct {
	ReadLinesPerSecond   float64 `json:"read_lines_per_second,omitempty"`
	NormalizedPerSecond  float64 `json:"normalized_per_second,omitempty"`
	QuarantinedPerSecond float64 `json:"quarantined_per_second,omitempty"`
	BytesPerSecond       float64 `json:"bytes_per_second,omitempty"`
	QueueDepth           int     `json:"queue_depth,omitempty"`
}

// StreamFileTelemetry is a bounded live view of one source file; it contains no raw log data.
type StreamFileTelemetry struct {
	NodeID               string           `json:"node_id"`
	Source               string           `json:"source"`
	Phase                string           `json:"phase"`
	SourceWorkers        int              `json:"source_workers"`
	ReadLinesPerSecond   float64          `json:"read_lines_per_second"`
	NormalizedPerSecond  float64          `json:"normalized_per_second"`
	QuarantinedPerSecond float64          `json:"quarantined_per_second"`
	BytesPerSecond       float64          `json:"bytes_per_second"`
	QueueDepth           int              `json:"queue_depth"`
	QueueCapacity        int              `json:"queue_capacity"`
	Backpressure         bool             `json:"backpressure"`
	ReadDuration         time.Duration    `json:"read_duration_ns"`
	ParseDuration        time.Duration    `json:"parse_duration_ns"`
	OutputDuration       time.Duration    `json:"output_duration_ns"`
	TotalDuration        time.Duration    `json:"total_duration_ns"`
	ReadLines            uint64           `json:"read_lines"`
	Normalized           uint64           `json:"normalized"`
	Quarantined          uint64           `json:"quarantined"`
	Bytes                uint64           `json:"bytes"`
	UpdatedAt            time.Time        `json:"updated_at"`
	Thresholds           StreamThresholds `json:"thresholds"`
}

// EventPayload contains only configured customer fields with guaranteed valid JSON
type EventPayload struct {
	Fields map[string]interface{} `json:"-"` // Raw fields map for internal use
}

// MarshalJSON preserves declared JSON types and fails closed for unsupported
// values; it never silently stringifies data or permits raw-retention keys.
func (ep EventPayload) MarshalJSON() ([]byte, error) {
	if ep.Fields == nil {
		return []byte("{}"), nil
	}
	safeFields := make(map[string]interface{}, len(ep.Fields))
	for key, value := range ep.Fields {
		if key == "" {
			return nil, fmt.Errorf("event payload field name is empty")
		}
		if key == "raw" || key == "_raw" {
			return nil, fmt.Errorf("event payload field %q is forbidden", key)
		}
		if err := validateEventValue(value); err != nil {
			return nil, fmt.Errorf("event payload field %q: %w", key, err)
		}
		if _, err := json.Marshal(value); err != nil {
			return nil, fmt.Errorf("event payload field %q is not JSON-safe: %w", key, err)
		}
		safeFields[key] = value
	}
	return json.Marshal(safeFields)
}

func validateEventValue(value interface{}) error {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			if key == "raw" || key == "_raw" {
				return fmt.Errorf("nested raw field is forbidden")
			}
			if err := validateEventValue(child); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, child := range typed {
			if err := validateEventValue(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// UnmarshalJSON handles deserialization
func (ep *EventPayload) UnmarshalJSON(data []byte) error {
	var fields map[string]interface{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	ep.Fields = fields
	return nil
}

// Get retrieves a field value
func (ep EventPayload) Get(key string) (interface{}, bool) {
	if ep.Fields == nil {
		return nil, false
	}
	val, exists := ep.Fields[key]
	return val, exists
}

// Set stores a field value
func (ep *EventPayload) Set(key string, value interface{}) {
	if ep.Fields == nil {
		ep.Fields = make(map[string]interface{})
	}
	ep.Fields[key] = value
}

// FieldCount returns the number of fields
func (ep EventPayload) FieldCount() int {
	if ep.Fields == nil {
		return 0
	}
	return len(ep.Fields)
}

// ParseTelemetry contains parser metadata and execution details
// Must not contain raw log text or emitted JSON event
type ParseTelemetry struct {
	EventID           string          `json:"event_id"`
	EventHash         string          `json:"event_hash"`
	NodeID            string          `json:"node_id"`
	PackID            string          `json:"pack_id"`
	ParserID          string          `json:"parser_id"`
	RuleID            string          `json:"rule_id,omitempty"`
	ParserVersion     string          `json:"parser_version"`
	PolicyHash        string          `json:"policy_hash"`
	ParserPriority    int             `json:"parser_priority,omitempty"`
	TemplateID        string          `json:"template_id,omitempty"`
	AttemptCount      int             `json:"attempt_count"`
	FallbackDepth     int             `json:"fallback_depth"`
	StageCount        int             `json:"stage_count"`
	OrderedAttempts   []ParserAttempt `json:"ordered_attempts"`
	LatencyMs         int64           `json:"latency_ms"`
	Confidence        float64         `json:"confidence"`
	EvidenceCoverage  float64         `json:"evidence_coverage"`
	Ambiguity         float64         `json:"ambiguity,omitempty"`
	InputFieldCount   int             `json:"input_field_count"`
	OutputFieldCount  int             `json:"output_field_count"`
	SourceFieldCount  int             `json:"source_field_count"`
	DerivedFieldCount int             `json:"derived_field_count"`
	Route             string          `json:"route,omitempty"`
	Result            string          `json:"result"`
	FieldEvidence     []FieldEvidence `json:"field_evidence,omitempty"`
	DeliveryAcks      []DeliveryAck   `json:"delivery_acks,omitempty"`
	QuarantineID      string          `json:"quarantine_id,omitempty"`
	QuarantineStatus  string          `json:"quarantine_status,omitempty"`
	QuarantineAttempt int             `json:"quarantine_attempt_count,omitempty"`
	ErrorCodes        []string        `json:"error_codes,omitempty"`
	ProcessedAt       time.Time       `json:"processed_at"`
}

// ParserAttempt represents one parser execution attempt
type ParserAttempt struct {
	ParserID      string         `json:"parser_id"`
	Priority      int            `json:"priority"`
	StageResults  []StageResult  `json:"stage_results"`
	Outcome       AttemptOutcome `json:"outcome"`
	FailureReason string         `json:"failure_reason,omitempty"`
	LatencyMs     int64          `json:"latency_ms"`
	FieldsEmitted int            `json:"fields_emitted"`
}

// FieldEvidence ties an emitted field to a source byte span or a named
// declarative derivation without retaining the source text.
type FieldEvidence struct {
	Field       string `json:"field"`
	Source      bool   `json:"source"`
	Start       int    `json:"start,omitempty"`
	End         int    `json:"end,omitempty"`
	DerivedRule string `json:"derived_rule,omitempty"`
}

// StageResult captures individual stage execution
type StageResult struct {
	StageID        string       `json:"stage_id"`
	StageName      string       `json:"stage_name"`
	StageType      string       `json:"stage_type"`
	Outcome        StageOutcome `json:"outcome"`
	FieldsAdded    int          `json:"fields_added"`
	FieldsModified int          `json:"fields_modified"`
	LatencyMs      int64        `json:"latency_ms"`
	ErrorCode      string       `json:"error_code,omitempty"`
}

type AttemptOutcome string

const (
	AttemptOutcomeSuccess     AttemptOutcome = "success"
	AttemptOutcomeFailed      AttemptOutcome = "failed"
	AttemptOutcomeRejected    AttemptOutcome = "rejected"
	AttemptOutcomeQuarantined AttemptOutcome = "quarantined"
)

type StageOutcome string

const (
	StageOutcomeSuccess   StageOutcome = "success"
	StageOutcomeFailed    StageOutcome = "failed"
	StageOutcomeSkipped   StageOutcome = "skipped"
	StageOutcomeContinued StageOutcome = "continued"
)

// DeliveryAck tracks output delivery status
type DeliveryAck struct {
	Destination string    `json:"destination"`
	Status      string    `json:"status"`
	Timestamp   time.Time `json:"timestamp"`
	ErrorCode   string    `json:"error_code,omitempty"`
}

// GenerateEventHash creates a SHA256 hash of the input
func GenerateEventHash(input string) string {
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", hash)
}

// NewParseTelemetry creates a new telemetry record
func NewParseTelemetry(input string, nodeID string) *ParseTelemetry {
	eventHash := GenerateEventHash(input)
	return &ParseTelemetry{
		EventID:         eventHash[:16], // First 16 chars as event ID
		EventHash:       eventHash,
		NodeID:          nodeID,
		Result:          "pending",
		OrderedAttempts: make([]ParserAttempt, 0),
		FieldEvidence:   make([]FieldEvidence, 0),
		DeliveryAcks:    make([]DeliveryAck, 0),
		ErrorCodes:      make([]string, 0),
		ProcessedAt:     time.Now().UTC(),
	}
}
