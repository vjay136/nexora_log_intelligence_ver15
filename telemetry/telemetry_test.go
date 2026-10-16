package telemetry

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventPayload_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		payload  EventPayload
		expected map[string]interface{}
	}{
		{
			name:     "empty payload",
			payload:  EventPayload{},
			expected: map[string]interface{}{},
		},
		{
			name: "mixed types",
			payload: EventPayload{
				Fields: map[string]interface{}{
					"string_field": "test",
					"int_field":    42,
					"float_field":  3.14,
					"bool_field":   true,
					"nil_field":    nil,
				},
			},
			expected: map[string]interface{}{
				"string_field": "test",
				"int_field":    float64(42), // JSON unmarshals numbers as float64
				"float_field":  3.14,
				"bool_field":   true,
				"nil_field":    nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Field count mismatch: got %d, expected %d", len(result), len(tt.expected))
			}

			for key, expectedVal := range tt.expected {
				actualVal, exists := result[key]
				if !exists {
					t.Errorf("Missing field: %s", key)
					continue
				}
				if actualVal != expectedVal {
					t.Errorf("Field %s: got %v, expected %v", key, actualVal, expectedVal)
				}
			}
		})
	}
}

func TestEventPayload_GetSet(t *testing.T) {
	payload := &EventPayload{}

	// Test setting and getting
	payload.Set("test_key", "test_value")

	val, exists := payload.Get("test_key")
	if !exists {
		t.Error("Expected field to exist")
	}
	if val != "test_value" {
		t.Errorf("Got %v, expected 'test_value'", val)
	}

	// Test non-existent key
	val, exists = payload.Get("missing_key")
	if exists {
		t.Error("Expected field not to exist")
	}
	if val != nil {
		t.Errorf("Got %v, expected nil", val)
	}
}

func TestEventPayload_FieldCount(t *testing.T) {
	payload := &EventPayload{}

	if payload.FieldCount() != 0 {
		t.Errorf("Expected 0 fields, got %d", payload.FieldCount())
	}

	payload.Set("field1", "value1")
	payload.Set("field2", "value2")

	if payload.FieldCount() != 2 {
		t.Errorf("Expected 2 fields, got %d", payload.FieldCount())
	}
}

func TestGenerateEventHash(t *testing.T) {
	input1 := "test input"
	input2 := "different input"

	hash1 := GenerateEventHash(input1)
	hash2 := GenerateEventHash(input2)

	// Hashes should be different
	if hash1 == hash2 {
		t.Error("Expected different hashes for different inputs")
	}

	// Hash should be consistent
	hash1_again := GenerateEventHash(input1)
	if hash1 != hash1_again {
		t.Error("Hash should be consistent for same input")
	}

	// Hash should be 64 hex characters (SHA256)
	if len(hash1) != 64 {
		t.Errorf("Expected 64 character hash, got %d", len(hash1))
	}
}

func TestNewParseTelemetry(t *testing.T) {
	input := "test log line"
	nodeID := "test-node-01"

	telemetry := NewParseTelemetry(input, nodeID)

	if telemetry.NodeID != nodeID {
		t.Errorf("Expected node ID %s, got %s", nodeID, telemetry.NodeID)
	}

	if telemetry.EventHash == "" {
		t.Error("Expected non-empty event hash")
	}

	if len(telemetry.EventID) != 16 {
		t.Errorf("Expected 16 character event ID, got %d", len(telemetry.EventID))
	}

	if telemetry.OrderedAttempts == nil {
		t.Error("Expected initialized attempts slice")
	}

	if telemetry.DeliveryAcks == nil {
		t.Error("Expected initialized delivery acks slice")
	}

	if telemetry.ErrorCodes == nil {
		t.Error("Expected initialized error codes slice")
	}
}

func TestParseTelemetryJSON(t *testing.T) {
	// Test that telemetry can be marshaled and unmarshaled
	original := &ParseTelemetry{
		EventID:       "1234567890abcdef",
		EventHash:     "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		NodeID:        "test-node",
		ParserID:      "test-parser",
		ParserVersion: "1.0.0",
		AttemptCount:  1,
		OrderedAttempts: []ParserAttempt{
			{
				ParserID:      "test-parser",
				Priority:      100,
				Outcome:       AttemptOutcomeSuccess,
				LatencyMs:     50,
				FieldsEmitted: 5,
			},
		},
		ProcessedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored ParseTelemetry
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if restored.EventID != original.EventID {
		t.Errorf("EventID mismatch: got %s, expected %s", restored.EventID, original.EventID)
	}

	if restored.NodeID != original.NodeID {
		t.Errorf("NodeID mismatch: got %s, expected %s", restored.NodeID, original.NodeID)
	}

	if len(restored.OrderedAttempts) != len(original.OrderedAttempts) {
		t.Errorf("Attempts count mismatch: got %d, expected %d",
			len(restored.OrderedAttempts), len(original.OrderedAttempts))
	}
}

func TestTelemetrySeparation(t *testing.T) {
	// Test that telemetry doesn't contain raw input or event JSON
	input := "sensitive log data with passwords"
	telemetry := NewParseTelemetry(input, "test-node")

	// Marshal to JSON and check content
	data, err := json.Marshal(telemetry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	jsonStr := string(data)

	// Should not contain the sensitive input data
	if containsSubstring(jsonStr, "sensitive") ||
		containsSubstring(jsonStr, "passwords") {
		t.Error("Telemetry JSON contains raw input data - security violation")
	}

	// Should contain hash but not raw data
	if !containsSubstring(jsonStr, "event_hash") {
		t.Error("Telemetry should contain event hash")
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) &&
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}()
}

func TestEventPayloadFailsClosedAndPreservesJSONNumbers(t *testing.T) {
	payload := EventPayload{Fields: map[string]interface{}{"count": json.Number("7")}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"count":7}` {
		t.Fatalf("json.Number was not preserved: %s", encoded)
	}
	if _, err := json.Marshal(EventPayload{Fields: map[string]interface{}{"raw": "secret"}}); err == nil {
		t.Fatal("raw field was accepted")
	}
	if _, err := json.Marshal(EventPayload{Fields: map[string]interface{}{"nested": map[string]interface{}{"raw": "secret"}}}); err == nil {
		t.Fatal("nested raw field was accepted")
	}
	if _, err := json.Marshal(EventPayload{Fields: map[string]interface{}{"unsupported": make(chan int)}}); err == nil {
		t.Fatal("non-JSON value was silently stringified")
	}
}
