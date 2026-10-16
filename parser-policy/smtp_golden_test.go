package parserpolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

// containsSubstring reports whether s contains sub.
func containsSubstring(s, sub string) bool { return strings.Contains(s, sub) }

// TestSMTPGoldenImplemented tests the actual SMTP parser implementation
func TestSMTPGoldenImplemented(t *testing.T) {
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

	// Create parser
	parser, err := NewSMTPParser()
	if err != nil {
		t.Fatalf("Failed to create SMTP parser: %v", err)
	}

	// Parse input
	eventPayload, parseTelemetry, err := parser.Parse(input, "test-node")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Validate event payload against golden case
	goldenCase := GoldenCase{
		Name:     "SMTP_SIEVE_AUTH_DISCONNECT",
		Input:    input,
		Expected: expected,
		ParserID: "smtp_native_v5/smtp_sieve_rule",
		Priority: 100,
		Route:    "smtp_events",
		Telemetry: &ExpectedTelemetry{
			ParserAttempts: []string{"smtp_native_v5/smtp_sieve_rule"},
			FallbackDepth:  0,
			SourceFields:   26,
			OutputFields:   26,
		},
	}

	ValidateGoldenCase(t, eventPayload.Fields, goldenCase)

	// Validate telemetry
	if parseTelemetry.ParserID != goldenCase.ParserID {
		t.Errorf("Parser ID: got %s, expected %s", parseTelemetry.ParserID, goldenCase.ParserID)
	}

	if parseTelemetry.AttemptCount != 1 {
		t.Errorf("Attempt count: got %d, expected 1", parseTelemetry.AttemptCount)
	}

	if len(parseTelemetry.OrderedAttempts) != 1 {
		t.Errorf("Ordered attempts: got %d, expected 1", len(parseTelemetry.OrderedAttempts))
	}

	if parseTelemetry.OutputFieldCount != goldenCase.Telemetry.OutputFields {
		t.Errorf("Output field count: got %d, expected %d",
			parseTelemetry.OutputFieldCount, goldenCase.Telemetry.OutputFields)
	}

	if parseTelemetry.SourceFieldCount != goldenCase.Telemetry.SourceFields {
		t.Errorf("Source field count: got %d, expected %d",
			parseTelemetry.SourceFieldCount, goldenCase.Telemetry.SourceFields)
	}

	// Validate separation: telemetry should not contain raw input
	telemetryJSON, err := json.Marshal(parseTelemetry)
	if err != nil {
		t.Fatalf("Failed to marshal telemetry: %v", err)
	}

	telemetryStr := string(telemetryJSON)
	if containsSubstring(telemetryStr, "badpassword") ||
		containsSubstring(telemetryStr, "managers999") {
		t.Error("Telemetry contains sensitive raw input data - security violation")
	}

	// Validate EventPayload contains no metadata
	eventJSON, err := json.Marshal(eventPayload)
	if err != nil {
		t.Fatalf("Failed to marshal event payload: %v", err)
	}

	eventStr := string(eventJSON)
	if containsSubstring(eventStr, "parser_id") ||
		containsSubstring(eventStr, "attempt_count") ||
		containsSubstring(eventStr, "latency") {
		t.Error("EventPayload contains parser metadata - should be pure customer fields only")
	}

	t.Logf("✓ SMTP Golden test passed")
	t.Logf("  - Parsed %d fields correctly", eventPayload.FieldCount())
	t.Logf("  - Parser: %s", parseTelemetry.ParserID)
	t.Logf("  - Attempts: %d", parseTelemetry.AttemptCount)
	t.Logf("  - Telemetry separation: validated")
}
