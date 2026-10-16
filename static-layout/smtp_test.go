package staticlayout

import (
	"testing"
)

func TestSMTPLayoutParser_ExactSMTPPrefix(t *testing.T) {
	parser, err := NewSMTPParser()
	if err != nil {
		t.Fatalf("Failed to create SMTP parser: %v", err)
	}

	input := "20260822 050048557+0200 severrgo001 smtp 230675 0 140347436861184 Note;SieveRuleMsg(2005/1) id=6A291BF5048F23DE:policy=outbound"

	result, err := parser.Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Validate static prefix fields
	expectedPrefix := map[string]interface{}{
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
	}

	for key, expectedVal := range expectedPrefix {
		actualVal, exists := result.Fields[key]
		if !exists {
			t.Errorf("Missing field: %s", key)
			continue
		}
		if actualVal != expectedVal {
			t.Errorf("Field %s: got %v (%T), expected %v (%T)",
				key, actualVal, actualVal, expectedVal, expectedVal)
		}
	}

	// Ensure no temporary fields are present
	if _, exists := result.Fields["_event_part"]; exists {
		t.Error("Temporary field _event_part should not be in output")
	}

	t.Logf("✓ SMTP prefix parsing: %d fields extracted", len(result.Fields))
}

func TestSMTPLayoutParser_EventCodeSplitting(t *testing.T) {
	parser, err := NewSMTPParser()
	if err != nil {
		t.Fatalf("Failed to create SMTP parser: %v", err)
	}

	tests := []struct {
		name          string
		eventPart     string
		expectedType  string
		expectedEvent string
		expectedCode  string
	}{
		{
			name:          "sieve rule format",
			eventPart:     "Note;SieveRuleMsg(2005/1)",
			expectedType:  "Note",
			expectedEvent: "SieveRuleMsg",
			expectedCode:  "2005/1",
		},
		{
			name:          "simple event",
			eventPart:     "Info;Connect(AUTH)",
			expectedType:  "Info",
			expectedEvent: "Connect",
			expectedCode:  "AUTH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := "2024-01-01 12:00:00 testhost smtp 12345 0 67890 " + tt.eventPart + " remaining=data"

			result, err := parser.Parse(input)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			if result.Fields["event_type"] != tt.expectedType {
				t.Errorf("event_type: got %v, expected %s", result.Fields["event_type"], tt.expectedType)
			}
			if result.Fields["event"] != tt.expectedEvent {
				t.Errorf("event: got %v, expected %s", result.Fields["event"], tt.expectedEvent)
			}
			if result.Fields["code"] != tt.expectedCode {
				t.Errorf("code: got %v, expected %s", result.Fields["code"], tt.expectedCode)
			}
		})
	}
}

func TestSMTPLayoutParser_GetKVTail(t *testing.T) {
	parser, err := NewSMTPParser()
	if err != nil {
		t.Fatalf("Failed to create SMTP parser: %v", err)
	}

	input := "20260822 050048557+0200 severrgo001 smtp 230675 0 140347436861184 Note;SieveRuleMsg(2005/1) id=6A291BF5048F23DE:policy=outbound:checkpoint=AUTH"

	kvTail, err := parser.GetKVTail(input)
	if err != nil {
		t.Fatalf("GetKVTail failed: %v", err)
	}

	expected := "id=6A291BF5048F23DE:policy=outbound:checkpoint=AUTH"
	if kvTail != expected {
		t.Errorf("KV tail: got %q, expected %q", kvTail, expected)
	}

	t.Logf("✓ KV tail extraction: %q", kvTail)
}

func TestCreateSMTPConfig(t *testing.T) {
	config := CreateSMTPConfig()

	// Validate configuration structure
	if config.Separator != "" {
		t.Errorf("Separator: got %q, expected empty string for whitespace", config.Separator)
	}

	if !config.FailOnMissing {
		t.Error("FailOnMissing should be true for strict SMTP parsing")
	}

	if len(config.Tokens) != 9 { // 8 static fields + kv_tail
		t.Errorf("Token count: got %d, expected 9", len(config.Tokens))
	}

	// Validate key token specifications
	expectedTokens := map[int]string{
		0: "date",
		1: "time",
		2: "hostname",
		3: "service",
		4: "major_pid",
		5: "pid_status",
		6: "minor_pid",
		7: "_event_capture",
		8: "_kv_tail",
	}

	for i, token := range config.Tokens {
		if expectedName, exists := expectedTokens[i]; exists {
			if token.Name != expectedName {
				t.Errorf("Token[%d] name: got %s, expected %s", i, token.Name, expectedName)
			}
			if token.Index != i {
				t.Errorf("Token[%d] index: got %d, expected %d", i, token.Index, i)
			}
		}
	}

	// Validate pid_status is typed as int
	pidStatusToken := config.Tokens[5]
	if pidStatusToken.Type != TokenTypeInt {
		t.Errorf("pid_status type: got %s, expected %s", pidStatusToken.Type, TokenTypeInt)
	}

	// Validate the event token uses declarative named regex captures.
	eventToken := config.Tokens[7]
	if eventToken.Type != TokenTypeRegex {
		t.Errorf("event capture type: got %s, expected %s", eventToken.Type, TokenTypeRegex)
	}
	regexConfig, ok := eventToken.Config.(RegexConfig)
	if !ok || regexConfig.Pattern == "" {
		t.Errorf("event capture requires a typed regex configuration")
	}

	// Validate _kv_tail is rest-of-line
	kvToken := config.Tokens[8]
	if kvToken.Type != TokenTypeRestOfLine {
		t.Errorf("_kv_tail type: got %s, expected %s", kvToken.Type, TokenTypeRestOfLine)
	}

	t.Logf("✓ SMTP config validation: %d tokens configured", len(config.Tokens))
}

func TestPostProcessSMTPFields(t *testing.T) {
	fields := map[string]interface{}{
		"date":        "20260822",
		"time":        "050048557+0200",
		"hostname":    "severrgo001",
		"_event_part": []interface{}{"Note", "SieveRuleMsg(2005/1)"},
		"_kv_tail":    "id=test",
	}

	err := PostProcessSMTPFields(fields)
	if err != nil {
		t.Fatalf("PostProcessSMTPFields failed: %v", err)
	}

	// Should have extracted event fields
	if fields["event_type"] != "Note" {
		t.Errorf("event_type: got %v, expected 'Note'", fields["event_type"])
	}
	if fields["event"] != "SieveRuleMsg" {
		t.Errorf("event: got %v, expected 'SieveRuleMsg'", fields["event"])
	}
	if fields["code"] != "2005/1" {
		t.Errorf("code: got %v, expected '2005/1'", fields["code"])
	}

	// Should have removed temporary field
	if _, exists := fields["_event_part"]; exists {
		t.Error("_event_part should be removed after processing")
	}

	// Other fields should remain
	if fields["date"] != "20260822" {
		t.Errorf("date field was modified: got %v", fields["date"])
	}

	t.Logf("✓ SMTP field post-processing: event_type=%s event=%s code=%s",
		fields["event_type"], fields["event"], fields["code"])
}
