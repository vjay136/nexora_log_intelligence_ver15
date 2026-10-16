package staticlayout

import (
	"testing"
)

func TestParser_ConfigurableWhitespacePrefix(t *testing.T) {
	config := Config{
		Separator: "", // Whitespace
		Tokens: []TokenSpec{
			{Index: 0, Name: "field1", Type: TokenTypeString, Required: true},
			{Index: 1, Name: "field2", Type: TokenTypeInt, Required: true},
			{Index: 2, Name: "field3", Type: TokenTypeString, Required: false},
		},
	}

	parser, err := NewParser(config)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected map[string]interface{}
		wantErr  bool
	}{
		{
			name:  "basic whitespace parsing",
			input: "hello 42 world",
			expected: map[string]interface{}{
				"field1": "hello",
				"field2": 42,
				"field3": "world",
			},
		},
		{
			name:  "multiple whitespace",
			input: "test    123   data",
			expected: map[string]interface{}{
				"field1": "test",
				"field2": 123,
				"field3": "data",
			},
		},
		{
			name:  "missing optional field",
			input: "alpha 789",
			expected: map[string]interface{}{
				"field1": "alpha",
				"field2": 789,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				for key, expectedVal := range tt.expected {
					actualVal, exists := result.Fields[key]
					if !exists {
						t.Errorf("Missing field: %s", key)
						continue
					}
					if actualVal != expectedVal {
						t.Errorf("Field %s: got %v, expected %v", key, actualVal, expectedVal)
					}
				}
			}
		})
	}
}

func TestParser_FixedWidthAndLiteralTokens(t *testing.T) {
	config := Config{
		FailOnMissing: true,
		Tokens: []TokenSpec{
			{Index: 0, Name: "record_type", Type: TokenTypeLiteral, Required: true, Config: LiteralConfig{Value: "HDR", Emit: true}},
			{Index: 0, Name: "id", Type: TokenTypeFixedWidth, Required: true, Config: FixedWidthConfig{Start: 4, Width: 5}},
			{Index: 0, Name: "name", Type: TokenTypeFixedWidth, Required: true, Config: FixedWidthConfig{Start: 9, Width: 10}, Transform: []Transform{{Type: TransformTrim}}},
		},
	}
	parser, err := NewParser(config)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	result, err := parser.Parse("HDR ID001  John Doe  ")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if result.Fields["record_type"] != "HDR" || result.Fields["id"] != "ID001" || result.Fields["name"] != "John Doe" {
		t.Fatalf("unexpected fixed-width result: %#v", result.Fields)
	}
	for _, evidence := range result.Evidence {
		if evidence.End <= evidence.Start {
			t.Fatalf("field %s lacks a source span: %#v", evidence.FieldName, evidence)
		}
	}
}

func TestParser_RegexToken(t *testing.T) {
	config := Config{
		Tokens: []TokenSpec{
			{Index: 0, Name: "timestamp", Type: TokenTypeString},
			{Index: 1, Name: "level", Type: TokenTypeString},
			{Index: 2, Name: "message", Type: TokenTypeRegex,
				Config: map[string]interface{}{
					"pattern": `^(.+)$`, // Match the whole token content
				}},
		},
	}

	parser, err := NewParser(config)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	result, err := parser.Parse("2024-01-01 ERROR message_content")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.Fields["timestamp"] != "2024-01-01" {
		t.Errorf("Timestamp: got %v, expected '2024-01-01'", result.Fields["timestamp"])
	}

	// Regex should extract the message part
	messageResult := result.Fields["message"]
	if messageArray, ok := messageResult.([]interface{}); ok {
		if len(messageArray) > 0 {
			if messageArray[0] != "message_content" {
				t.Errorf("Regex result: got %v, expected 'message_content'", messageArray[0])
			}
		} else {
			t.Error("Regex result array is empty")
		}
	} else {
		t.Errorf("Regex result not array: got %T, value: %v", messageResult, messageResult)
	}
}

func TestParser_RestOfLine(t *testing.T) {
	config := Config{
		Tokens: []TokenSpec{
			{Index: 0, Name: "date", Type: TokenTypeString, Required: true},
			{Index: 1, Name: "time", Type: TokenTypeString, Required: true},
			{Index: 2, Name: "remaining", Type: TokenTypeRestOfLine, Required: true},
		},
	}

	parser, err := NewParser(config)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	result, err := parser.Parse("2024-01-01 12:34:56 This is the rest of the line with many words")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	expected := map[string]interface{}{
		"date":      "2024-01-01",
		"time":      "12:34:56",
		"remaining": "This is the rest of the line with many words",
	}

	for key, expectedVal := range expected {
		actualVal, exists := result.Fields[key]
		if !exists {
			t.Errorf("Missing field: %s", key)
			continue
		}
		if actualVal != expectedVal {
			t.Errorf("Field %s: got %v, expected %v", key, actualVal, expectedVal)
		}
	}

	// Verify token counts
	if result.TokensFound != 12 { // Should find all individual words
		t.Errorf("TokensFound: got %d, expected 12", result.TokensFound)
	}
	if result.TokensUsed != 12 { // 1 + 1 + 10 for rest-of-line
		t.Errorf("TokensUsed: got %d, expected 12", result.TokensUsed)
	}
}

func TestParser_OptionalTokens(t *testing.T) {
	config := Config{
		FailOnMissing: false, // Important for optional tokens
		Tokens: []TokenSpec{
			{Index: 0, Name: "required1", Type: TokenTypeString, Required: true},
			{Index: 1, Name: "optional1", Type: TokenTypeString, Required: false},
			{Index: 2, Name: "optional2", Type: TokenTypeInt, Required: false},
		},
	}

	parser, err := NewParser(config)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	tests := []struct {
		name         string
		input        string
		expectedKeys []string
	}{
		{
			name:         "all tokens present",
			input:        "alpha beta 123",
			expectedKeys: []string{"required1", "optional1", "optional2"},
		},
		{
			name:         "only required token",
			input:        "gamma",
			expectedKeys: []string{"required1"},
		},
		{
			name:         "required plus one optional",
			input:        "delta epsilon",
			expectedKeys: []string{"required1", "optional1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			// Check that expected keys exist
			for _, key := range tt.expectedKeys {
				if _, exists := result.Fields[key]; !exists {
					t.Errorf("Missing expected field: %s", key)
				}
			}

			// Check that no extra keys exist
			for key := range result.Fields {
				found := false
				for _, expectedKey := range tt.expectedKeys {
					if key == expectedKey {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Unexpected field: %s", key)
				}
			}
		})
	}
}

func TestParser_Transforms(t *testing.T) {
	config := Config{
		Tokens: []TokenSpec{
			{
				Index: 0,
				Name:  "field1",
				Type:  TokenTypeString,
				Transform: []Transform{
					{Type: TransformTrim},
					{Type: TransformStripPrefix, Config: map[string]string{"prefix": "prefix_"}},
				},
			},
			{
				Index: 1,
				Name:  "field2",
				Type:  TokenTypeString,
				Transform: []Transform{
					{Type: TransformReplace, Config: map[string]string{"old": "-", "new": "_"}},
				},
			},
		},
	}

	parser, err := NewParser(config)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	result, err := parser.Parse("  prefix_test  hello-world")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.Fields["field1"] != "test" {
		t.Errorf("field1: got %v, expected 'test'", result.Fields["field1"])
	}

	if result.Fields["field2"] != "hello_world" {
		t.Errorf("field2: got %v, expected 'hello_world'", result.Fields["field2"])
	}
}

func TestParser_FailurePolicy(t *testing.T) {
	config := Config{
		FailOnMissing: true,
		Tokens: []TokenSpec{
			{Index: 0, Name: "field1", Type: TokenTypeString, Required: true},
			{Index: 1, Name: "field2", Type: TokenTypeInt, Required: true},
		},
	}

	parser, err := NewParser(config)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	// Should fail when required token is missing
	_, err = parser.Parse("onlyonefield")
	if err == nil {
		t.Error("Expected error for missing required token, got nil")
	}

	// Should succeed with all required tokens
	result, err := parser.Parse("alpha 42")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.Fields["field1"] != "alpha" || result.Fields["field2"] != 42 {
		t.Errorf("Unexpected field values: %v", result.Fields)
	}
}

func TestParser_GrokOptionalAndExactRestEvidence(t *testing.T) {
	parser, err := NewParser(Config{
		FailOnMissing: true,
		Tokens: []TokenSpec{
			{Index: 0, Name: "_level", Type: TokenTypeGrok, Required: true, Config: GrokConfig{Pattern: `%{WORD:level}`}},
			{Index: 1, Name: "message", Type: TokenTypeRestOfLine, Required: true},
			{Index: 20, Name: "missing", Type: TokenTypeString, Optional: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := "INFO   message with  preserved spacing"
	result, err := parser.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 || result.Fields["level"] != "INFO" || result.Fields["message"] != "message with  preserved spacing" {
		t.Fatalf("unexpected declarative parse: fields=%#v errors=%v", result.Fields, result.Errors)
	}
	for _, evidence := range result.Evidence {
		if evidence.Start < 0 || evidence.End > len(input) || evidence.End <= evidence.Start {
			t.Fatalf("invalid evidence span: %#v", evidence)
		}
		if input[evidence.Start:evidence.End] != evidence.RawValue {
			t.Fatalf("evidence span does not identify raw token: %#v", evidence)
		}
	}
}
