package staticlayout

import (
	"fmt"
	"regexp"
)

// CreateSMTPConfig returns the complete declarative prefix layout. The event
// token is split by named RE2 captures and the tail preserves its source span.
func CreateSMTPConfig() Config {
	return Config{
		FailOnMissing: true,
		Tokens: []TokenSpec{
			{Index: 0, Name: "date", Type: TokenTypeString, Required: true},
			{Index: 1, Name: "time", Type: TokenTypeString, Required: true},
			{Index: 2, Name: "hostname", Type: TokenTypeString, Required: true},
			{Index: 3, Name: "service", Type: TokenTypeString, Required: true},
			{Index: 4, Name: "major_pid", Type: TokenTypeString, Required: true},
			{Index: 5, Name: "pid_status", Type: TokenTypeInt, Required: true},
			{Index: 6, Name: "minor_pid", Type: TokenTypeString, Required: true},
			{
				Index: 7, Name: "_event_capture", Type: TokenTypeRegex, Required: true,
				Config: RegexConfig{
					Pattern: `^(?P<event_type>[^;]+);(?P<event>[^()]+)\((?P<code>[^()]+)\)$`,
				},
			},
			{Index: 8, Name: "_kv_tail", Type: TokenTypeRestOfLine, Optional: true},
		},
	}
}

var eventRegex = regexp.MustCompile(`^([^(]+)\(([^)]+)\)$`)

// PostProcessSMTPFields remains for callers that supply the legacy split-array
// shape; declarative parsers already emit event_type/event/code directly.
func PostProcessSMTPFields(fields map[string]interface{}) error {
	if _, complete := fields["event"]; complete {
		delete(fields, "_event_part")
		return nil
	}
	eventPart, exists := fields["_event_part"]
	if !exists {
		return nil
	}
	parts, ok := eventPart.([]interface{})
	if !ok || len(parts) < 2 {
		return fmt.Errorf("invalid SMTP event token")
	}
	left, leftOK := parts[0].(string)
	right, rightOK := parts[1].(string)
	if !leftOK || !rightOK {
		return fmt.Errorf("invalid SMTP event token values")
	}
	match := eventRegex.FindStringSubmatch(right)
	if match == nil {
		return fmt.Errorf("invalid SMTP event/code token")
	}
	fields["event_type"], fields["event"], fields["code"] = left, match[1], match[2]
	delete(fields, "_event_part")
	return nil
}

type SMTPLayoutParser struct {
	parser *Parser
}

func NewSMTPParser() (*SMTPLayoutParser, error) {
	parser, err := NewParser(CreateSMTPConfig())
	if err != nil {
		return nil, err
	}
	return &SMTPLayoutParser{parser: parser}, nil
}

func (s *SMTPLayoutParser) Parse(input string) (*ParseResult, error) {
	return s.parser.Parse(input)
}

func (s *SMTPLayoutParser) GetKVTail(input string) (string, error) {
	result, err := s.Parse(input)
	if err != nil {
		return "", err
	}
	value, exists := result.Fields["_kv_tail"]
	if !exists {
		return "", fmt.Errorf("no KV tail found")
	}
	tail, ok := value.(string)
	if !ok || tail == "" {
		return "", fmt.Errorf("invalid KV tail")
	}
	return tail, nil
}
