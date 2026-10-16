package parserpolicy

import (
	"encoding/json"
	"fmt"
	"testing"
)

// GoldenCase represents an exact regression test case
type GoldenCase struct {
	Name      string                 `json:"name"`
	Input     string                 `json:"input"`
	Expected  map[string]interface{} `json:"expected"`
	ParserID  string                 `json:"parser_id"`
	Priority  int                    `json:"priority"`
	Route     string                 `json:"route,omitempty"`
	Telemetry *ExpectedTelemetry     `json:"telemetry,omitempty"`
}

// TestSMTPGolden tests the exact SMTP golden case from the spec
func TestSMTPGolden(t *testing.T) {
	input := "20260822 050048557+0200 severrgo001 smtp 230675 0 140347436861184 Note;SieveRuleMsg(2005/1) id=6A291BF5048F23DE:policy=outbound:checkpoint=AUTH:rule=INOUT_action:msg=PolicyMsgInfo:eventType=PolicyMsgInfo:action=disconnect:reason=badpassword:fromhost=11.22.33.44:geo=IL:helo=Site6:auth_ok=false:auth_user=managers999@012.meg.in:auth_fail=6:auth_tries=7:score=0"

	expected := map[string]interface{}{
		"date":       "20260822",
		"time":       "050048557+0200",
		"hostname":   "severrgo001",
		"service":    "smtp",
		"major_pid":  "230675",
		"pid_status": 0,
		"minor_pid":  "140347436861184",
		"event_type": "Note",
		"event":      "SieveRuleMsg",
		"code":       "2005/1",
		"id":         "6A291BF5048F23DE",
		"policy":     "outbound",
		"checkpoint": "AUTH",
		"rule":       "INOUT_action",
		"msg":        "PolicyMsgInfo",
		"eventType":  "PolicyMsgInfo",
		"action":     "disconnect",
		"reason":     "badpassword",
		"fromhost":   "11.22.33.44",
		"geo":        "IL",
		"helo":       "Site6",
		"auth_ok":    false,
		"auth_user":  "managers999@012.meg.in",
		"auth_fail":  6,
		"auth_tries": 7,
		"score":      0,
	}

	goldenCase := GoldenCase{
		Name:     "SMTP_SIEVE_AUTH_DISCONNECT",
		Input:    input,
		Expected: expected,
		ParserID: "smtp_native_v5/smtp_sieve_rule",
		Priority: 100,
		Route:    "normal",
		Telemetry: &ExpectedTelemetry{
			ParserAttempts: []string{"smtp_native_v5/smtp_sieve_rule"},
			FallbackDepth:  0,
			SourceFields:   26,
			OutputFields:   26,
		},
	}

	t.Logf("Golden case: %s", goldenCase.Name)
	t.Logf("Input: %s", goldenCase.Input)

	expectedJSON, err := json.MarshalIndent(goldenCase.Expected, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal expected JSON: %v", err)
	}
	t.Logf("Expected output: %s", expectedJSON)

	executor, err := NewSMTPParserPackExecutor()
	if err != nil {
		t.Fatalf("create SMTP executor: %v", err)
	}
	result, err := executor.Execute(&ExecutionRequest{Input: goldenCase.Input, Service: "smtp", NodeID: "golden"})
	if err != nil {
		t.Fatalf("execute SMTP golden case: %v", err)
	}
	if result.Quarantined || result.EventPayload == nil {
		t.Fatalf("SMTP golden case was quarantined: %+v", result.ParseTelemetry)
	}
	ValidateGoldenCase(t, result.EventPayload.Fields, goldenCase)
	if result.ParserID != goldenCase.ParserID || result.Priority != goldenCase.Priority || result.ParseTelemetry.Route != goldenCase.Route {
		t.Fatalf("identity/route mismatch: parser=%s priority=%d route=%s", result.ParserID, result.Priority, result.ParseTelemetry.Route)
	}
}

// ValidateGoldenCase validates a golden case result against expected output
func ValidateGoldenCase(t *testing.T, actual map[string]interface{}, golden GoldenCase) {
	// Validate field count
	if len(actual) != len(golden.Expected) {
		t.Errorf("Field count mismatch: got %d, expected %d", len(actual), len(golden.Expected))
	}

	// Validate each field
	for key, expectedVal := range golden.Expected {
		actualVal, exists := actual[key]
		if !exists {
			t.Errorf("Missing field: %s", key)
			continue
		}

		if !deepEqual(actualVal, expectedVal) {
			t.Errorf("Field %s: got %v (%T), expected %v (%T)",
				key, actualVal, actualVal, expectedVal, expectedVal)
		}
	}

	// Check for extra fields
	for key := range actual {
		if _, exists := golden.Expected[key]; !exists {
			t.Errorf("Unexpected field: %s = %v", key, actual[key])
		}
	}
}

// deepEqual compares two values considering type coercion
func deepEqual(a, b interface{}) bool {
	// Handle nil cases
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Try direct equality first
	if a == b {
		return true
	}

	// Handle numeric comparisons
	switch av := a.(type) {
	case int:
		switch bv := b.(type) {
		case int:
			return av == bv
		case float64:
			return float64(av) == bv
		case string:
			if i, err := json.Number(bv).Int64(); err == nil {
				return av == int(i)
			}
		}
	case float64:
		switch bv := b.(type) {
		case int:
			return av == float64(bv)
		case float64:
			return av == bv
		case string:
			if f, err := json.Number(bv).Float64(); err == nil {
				return av == f
			}
		}
	case string:
		return av == fmt.Sprintf("%v", b)
	case bool:
		if bv, ok := b.(bool); ok {
			return av == bv
		}
		if bv, ok := b.(string); ok {
			return fmt.Sprintf("%t", av) == bv
		}
	}

	return false
}
