// Package mixedkv implements a bounded, configuration-driven parser for
// key/value streams that use more than one delimiter.
package mixedkv

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxInputBytes = 1024 * 1024
	defaultMaxFields     = 256
	defaultMaxKeyBytes   = 128
	defaultMaxValueBytes = 1024 * 1024
	hardMaxInputBytes    = 16 * 1024 * 1024
	hardMaxFields        = 4096
	hardMaxKeyBytes      = 1024
	hardMaxValueBytes    = 16 * 1024 * 1024
	maxSeparatorCount    = 32
	maxPatternCount      = 64
	maxPatternBytes      = 4096
)

// DuplicatePolicy controls what happens when the same output key is found
// more than once. Aliases are applied before duplicate detection.
type DuplicatePolicy string

const (
	DuplicateError DuplicatePolicy = "error"
	DuplicateFirst DuplicatePolicy = "first"
	DuplicateLast  DuplicatePolicy = "last"
	DuplicateArray DuplicatePolicy = "array"
)

// Config is safe to serialize as part of a declarative parser pack. A zero
// value receives conservative defaults from DefaultConfig.
type Config struct {
	PairSeparators       []string          `json:"pair_separators"`
	AssignmentSeparators []string          `json:"assignment_separators"`
	QuoteCharacters      string            `json:"quote_characters,omitempty"`
	EscapeCharacter      string            `json:"escape_character,omitempty"`
	BracketPairs         map[string]string `json:"bracket_pairs,omitempty"`
	KeyPattern           string            `json:"key_pattern,omitempty"`
	KeyAllowPatterns     []string          `json:"key_allow_patterns,omitempty"`
	KeyDenyPatterns      []string          `json:"key_deny_patterns,omitempty"`
	StopKeys             []string          `json:"stop_keys,omitempty"`
	KeyAliases           map[string]string `json:"key_aliases,omitempty"`
	ValueTypes           map[string]string `json:"value_types,omitempty"`
	DuplicatePolicy      DuplicatePolicy   `json:"duplicate_policy,omitempty"`
	RejectEmptyValues    bool              `json:"reject_empty_values,omitempty"`
	AllowLeadingText     bool              `json:"allow_leading_text,omitempty"`
	MaxInputBytes        int               `json:"max_input_bytes,omitempty"`
	MaxFields            int               `json:"max_fields,omitempty"`
	MaxKeyBytes          int               `json:"max_key_bytes,omitempty"`
	MaxValueBytes        int               `json:"max_value_bytes,omitempty"`
}

// DefaultConfig enables the common separators without treating commas as pair
// separators. Commas are intentionally opt-in because they are frequently part
// of list values such as mailto:a@example.com,mailto:b@example.com.
func DefaultConfig() Config {
	return Config{
		PairSeparators:       []string{":", ";", "|", " ", "\t"},
		AssignmentSeparators: []string{"=", ":"},
		QuoteCharacters:      `"'`,
		EscapeCharacter:      `\`,
		BracketPairs:         map[string]string{"[": "]", "{": "}", "(": ")"},
		KeyPattern:           `[A-Za-z_][A-Za-z0-9_.-]*`,
		DuplicatePolicy:      DuplicateError,
		MaxInputBytes:        defaultMaxInputBytes,
		MaxFields:            defaultMaxFields,
		MaxKeyBytes:          defaultMaxKeyBytes,
		MaxValueBytes:        defaultMaxValueBytes,
	}
}

// Span is a half-open byte range into the original input. Byte offsets avoid
// ambiguity and remain valid for UTF-8 input.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Entry records the exact source evidence for one recognized key/value pair.
type Entry struct {
	SourceKey           string      `json:"source_key"`
	Key                 string      `json:"key"`
	Value               interface{} `json:"value"`
	KeySpan             Span        `json:"key_span"`
	ValueSpan           Span        `json:"value_span"`
	PairSpan            Span        `json:"pair_span"`
	AssignmentSeparator string      `json:"assignment_separator"`
	PairSeparator       string      `json:"pair_separator,omitempty"`
	Emitted             bool        `json:"emitted"`
	SkipReason          string      `json:"skip_reason,omitempty"`
}

// Result contains emitted fields plus accounting data. It never stores the
// whole input; callers may use the spans while they still hold that input.
type Result struct {
	Fields        map[string]interface{} `json:"fields"`
	Entries       []Entry                `json:"entries"`
	ConsumedSpans []Span                 `json:"consumed_spans"`
	StoppedBy     string                 `json:"stopped_by,omitempty"`
	LeadingSpan   *Span                  `json:"leading_span,omitempty"`
	RemainderSpan *Span                  `json:"remainder_span,omitempty"`
}

// Error is a stable, machine-readable parse or configuration failure.
type Error struct {
	Code   string
	Offset int
	Key    string
	Detail string
}

func (e *Error) Error() string {
	location := ""
	if e.Offset >= 0 {
		location = fmt.Sprintf(" at byte %d", e.Offset)
	}
	key := ""
	if e.Key != "" {
		key = fmt.Sprintf(" for key %q", e.Key)
	}
	return fmt.Sprintf("mixedkv %s%s%s: %s", e.Code, location, key, e.Detail)
}

const (
	CodeInvalidConfig    = "invalid_config"
	CodeInputTooLarge    = "input_too_large"
	CodeNoKeyValue       = "no_key_value"
	CodeMalformedQuote   = "malformed_quote"
	CodeMalformedBracket = "malformed_bracket"
	CodeDanglingEscape   = "dangling_escape"
	CodeTooManyFields    = "too_many_fields"
	CodeValueTooLarge    = "value_too_large"
	CodeEmptyValue       = "empty_value"
	CodeDuplicateKey     = "duplicate_key"
	CodeTypeConversion   = "type_conversion"
)

// Parser is an immutable, compiled mixed-KV stage and is safe for concurrent
// use by clients.
type Parser struct {
	config       Config
	keyRE        *regexp.Regexp
	allowRE      []*regexp.Regexp
	denyRE       []*regexp.Regexp
	pairSeps     []string
	assignSeps   []string
	whitespace   map[byte]bool
	quotes       map[byte]bool
	escape       byte
	openBrackets map[byte]byte
	closers      map[byte]bool
	stopKeys     map[string]bool
}

type header struct {
	key         string
	keyStart    int
	keyEnd      int
	assignStart int
	assignEnd   int
	valueStart  int
	separator   string
}

type boundary struct {
	start     int
	next      int
	separator string
}

// Compile validates Config and returns an immutable parser.
func Compile(input Config) (*Parser, error) {
	cfg := applyDefaults(input)
	if cfg.MaxInputBytes < 1 || cfg.MaxFields < 1 || cfg.MaxKeyBytes < 1 || cfg.MaxValueBytes < 1 {
		return nil, configError("resource limits must be positive")
	}
	if cfg.MaxInputBytes > hardMaxInputBytes || cfg.MaxFields > hardMaxFields || cfg.MaxKeyBytes > hardMaxKeyBytes || cfg.MaxValueBytes > hardMaxValueBytes {
		return nil, configError("resource limits exceed hard safety caps")
	}
	if len(cfg.KeyAllowPatterns) > maxPatternCount || len(cfg.KeyDenyPatterns) > maxPatternCount {
		return nil, configError("key allow/deny patterns may contain at most 64 entries each")
	}
	if len(cfg.KeyAliases) > hardMaxFields || len(cfg.ValueTypes) > hardMaxFields || len(cfg.StopKeys) > hardMaxFields {
		return nil, configError("aliases, value types, or stop keys exceed hard field cap")
	}
	if len(cfg.QuoteCharacters) > 16 || len(cfg.BracketPairs) > 16 {
		return nil, configError("quote characters and bracket pairs may contain at most 16 entries")
	}
	if len(cfg.PairSeparators) == 0 || len(cfg.PairSeparators) > maxSeparatorCount {
		return nil, configError("pair_separators must contain 1..32 values")
	}
	if len(cfg.AssignmentSeparators) == 0 || len(cfg.AssignmentSeparators) > maxSeparatorCount {
		return nil, configError("assignment_separators must contain 1..32 values")
	}
	if len(cfg.KeyPattern) > maxPatternBytes {
		return nil, configError("key_pattern exceeds 4096 bytes")
	}
	keyRE, err := regexp.Compile(`^(?:` + cfg.KeyPattern + `)$`)
	if err != nil {
		return nil, configError("invalid key_pattern: " + err.Error())
	}
	allowRE, err := compilePatterns(cfg.KeyAllowPatterns)
	if err != nil {
		return nil, configError("invalid key allow pattern: " + err.Error())
	}
	denyRE, err := compilePatterns(cfg.KeyDenyPatterns)
	if err != nil {
		return nil, configError("invalid key deny pattern: " + err.Error())
	}
	pairSeps, whitespace, err := normalizeSeparators(cfg.PairSeparators)
	if err != nil {
		return nil, configError("invalid pair separator: " + err.Error())
	}
	assignSeps, _, err := normalizeSeparators(cfg.AssignmentSeparators)
	if err != nil {
		return nil, configError("invalid assignment separator: " + err.Error())
	}
	if len(cfg.EscapeCharacter) != 1 {
		return nil, configError("escape_character must be exactly one byte")
	}
	quotes := make(map[byte]bool, len(cfg.QuoteCharacters))
	for i := 0; i < len(cfg.QuoteCharacters); i++ {
		c := cfg.QuoteCharacters[i]
		if c >= 0x80 || quotes[c] {
			return nil, configError("quote_characters must contain unique ASCII bytes")
		}
		quotes[c] = true
	}
	openBrackets := make(map[byte]byte, len(cfg.BracketPairs))
	closers := make(map[byte]bool, len(cfg.BracketPairs))
	for open, close := range cfg.BracketPairs {
		if len(open) != 1 || len(close) != 1 || open == close || open[0] >= 0x80 || close[0] >= 0x80 {
			return nil, configError("bracket pairs must be distinct one-byte ASCII characters")
		}
		if _, exists := openBrackets[open[0]]; exists || closers[close[0]] {
			return nil, configError("bracket characters must be unique")
		}
		openBrackets[open[0]] = close[0]
		closers[close[0]] = true
	}
	if !validDuplicatePolicy(cfg.DuplicatePolicy) {
		return nil, configError("duplicate_policy must be error, first, last, or array")
	}
	for source, alias := range cfg.KeyAliases {
		if !keyRE.MatchString(source) || !keyRE.MatchString(alias) {
			return nil, configError(fmt.Sprintf("invalid key alias %q -> %q", source, alias))
		}
	}
	for key, kind := range cfg.ValueTypes {
		if !keyRE.MatchString(key) || !validValueType(kind) {
			return nil, configError(fmt.Sprintf("invalid value type %q for key %q", kind, key))
		}
	}
	stopKeys := make(map[string]bool, len(cfg.StopKeys))
	for _, key := range cfg.StopKeys {
		if !keyRE.MatchString(key) {
			return nil, configError(fmt.Sprintf("invalid stop key %q", key))
		}
		stopKeys[key] = true
	}
	return &Parser{
		config: cfg, keyRE: keyRE, allowRE: allowRE, denyRE: denyRE,
		pairSeps: pairSeps, assignSeps: assignSeps, whitespace: whitespace,
		quotes: quotes, escape: cfg.EscapeCharacter[0], openBrackets: openBrackets,
		closers: closers, stopKeys: stopKeys,
	}, nil
}

// MustCompile is intended for package-level parsers with static configuration.
func MustCompile(config Config) *Parser {
	parser, err := Compile(config)
	if err != nil {
		panic(err)
	}
	return parser
}

// Parse compiles config and parses one input record.
func Parse(input string, config Config) (Result, error) {
	parser, err := Compile(config)
	if err != nil {
		return Result{}, err
	}
	return parser.Parse(input)
}

// Parse parses one input record in linear space with bounded fields and input.
func (p *Parser) Parse(input string) (Result, error) {
	result := Result{Fields: make(map[string]interface{})}
	if len(input) > p.config.MaxInputBytes {
		return result, parseError(CodeInputTooLarge, p.config.MaxInputBytes, "", "input exceeds configured maximum")
	}

	position := skipSpace(input, 0)
	if p.config.AllowLeadingText {
		if _, ok := p.headerAt(input, position); !ok {
			first, found, err := p.findFirstHeader(input, position)
			if err != nil {
				return result, err
			}
			if !found {
				return result, parseError(CodeNoKeyValue, position, "", "no allowlisted key/value boundary was recognized")
			}
			result.LeadingSpan = &Span{Start: position, End: first}
			position = first
		}
	}
	for position < len(input) {
		h, ok := p.headerAt(input, position)
		if !ok {
			if len(result.Entries) == 0 {
				return result, parseError(CodeNoKeyValue, position, "", "expected a configured key and assignment separator")
			}
			span := Span{Start: position, End: len(input)}
			result.RemainderSpan = &span
			return result, nil
		}
		if p.stopKeys[h.key] {
			span := Span{Start: h.keyStart, End: len(input)}
			result.StoppedBy = h.key
			result.RemainderSpan = &span
			return result, nil
		}
		if len(result.Entries) >= p.config.MaxFields {
			return result, parseError(CodeTooManyFields, h.keyStart, h.key, "maximum field count exceeded")
		}

		fieldType := p.valueType(h.key)
		end, err := p.findBoundary(input, h.valueStart, fieldType)
		if err != nil {
			return result, err
		}
		valueEnd := len(input)
		pairSeparator := ""
		next := len(input)
		if end != nil {
			valueEnd = end.start
			pairSeparator = end.separator
			next = end.next
		}
		valueStartTrimmed, valueEndTrimmed := trimSpaceBounds(input, h.valueStart, valueEnd)
		if valueEndTrimmed-valueStartTrimmed > p.config.MaxValueBytes {
			return result, parseError(CodeValueTooLarge, valueStartTrimmed, h.key, "value exceeds configured maximum")
		}
		rawValue := input[valueStartTrimmed:valueEndTrimmed]
		if rawValue == "" && p.config.RejectEmptyValues {
			return result, parseError(CodeEmptyValue, valueStartTrimmed, h.key, "empty values are disabled")
		}
		decoded, err := p.decodeValue(rawValue)
		if err != nil {
			return result, parseError(CodeDanglingEscape, valueStartTrimmed, h.key, err.Error())
		}
		outputKey := h.key
		if alias, exists := p.config.KeyAliases[h.key]; exists {
			outputKey = alias
		}
		kind := p.valueTypeForOutput(h.key, outputKey)
		value, err := convertValue(decoded, rawValue, kind)
		if err != nil {
			return result, parseError(CodeTypeConversion, valueStartTrimmed, h.key, err.Error())
		}

		entry := Entry{
			SourceKey: h.key, Key: outputKey, Value: value,
			KeySpan:             Span{Start: h.keyStart, End: h.keyEnd},
			ValueSpan:           Span{Start: valueStartTrimmed, End: valueEndTrimmed},
			PairSpan:            Span{Start: h.keyStart, End: valueEnd},
			AssignmentSeparator: h.separator, PairSeparator: pairSeparator,
			Emitted: true,
		}
		if p.isDenied(h.key) {
			entry.Emitted = false
			entry.SkipReason = "denied"
		} else if err := p.emit(&result, &entry); err != nil {
			return result, err
		}
		result.Entries = append(result.Entries, entry)
		result.ConsumedSpans = append(result.ConsumedSpans, entry.PairSpan)
		position = next
	}

	if len(result.Entries) == 0 && result.StoppedBy == "" {
		return result, parseError(CodeNoKeyValue, 0, "", "no key/value pairs were recognized")
	}
	return result, nil
}

func (p *Parser) emit(result *Result, entry *Entry) error {
	existing, duplicate := result.Fields[entry.Key]
	if !duplicate {
		result.Fields[entry.Key] = entry.Value
		return nil
	}
	switch p.config.DuplicatePolicy {
	case DuplicateFirst:
		entry.Emitted = false
		entry.SkipReason = "duplicate_first_wins"
	case DuplicateLast:
		result.Fields[entry.Key] = entry.Value
	case DuplicateArray:
		if values, ok := existing.([]interface{}); ok {
			result.Fields[entry.Key] = append(values, entry.Value)
		} else {
			result.Fields[entry.Key] = []interface{}{existing, entry.Value}
		}
	default:
		return parseError(CodeDuplicateKey, entry.KeySpan.Start, entry.Key, "duplicate output key")
	}
	return nil
}

func (p *Parser) findFirstHeader(input string, start int) (int, bool, error) {
	var quote byte
	stack := make([]byte, 0, 4)
	for i := start; i < len(input); {
		c := input[i]
		if c == p.escape {
			if i+1 >= len(input) {
				return 0, false, parseError(CodeDanglingEscape, i, "", "escape character has no following byte")
			}
			i += 2
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			i++
			continue
		}
		if p.quotes[c] && quoteCanOpen(input, start, i) {
			quote = c
			i++
			continue
		}
		if closer, opens := p.openBrackets[c]; opens {
			stack = append(stack, closer)
			i++
			continue
		}
		if p.closers[c] {
			if len(stack) == 0 || stack[len(stack)-1] != c {
				return 0, false, parseError(CodeMalformedBracket, i, "", "unexpected or mismatched closing bracket")
			}
			stack = stack[:len(stack)-1]
			i++
			continue
		}
		if len(stack) == 0 {
			_, after, matched := p.pairSeparatorAt(input, i)
			if matched {
				next := skipSpace(input, after)
				if _, valid := p.headerAt(input, next); valid {
					return next, true, nil
				}
				i = after
				continue
			}
		}
		i++
	}
	if quote != 0 {
		return 0, false, parseError(CodeMalformedQuote, len(input), "", "unterminated quoted leading text")
	}
	if len(stack) > 0 {
		return 0, false, parseError(CodeMalformedBracket, len(input), "", "unterminated bracketed leading text")
	}
	return 0, false, nil
}

func (p *Parser) findBoundary(input string, valueStart int, fieldType string) (*boundary, error) {
	var quote byte
	stack := make([]byte, 0, 4)
	for i := valueStart; i < len(input); {
		c := input[i]
		if c == p.escape {
			if i+1 >= len(input) {
				return nil, parseError(CodeDanglingEscape, i, "", "escape character has no following byte")
			}
			i += 2
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			i++
			continue
		}
		if p.quotes[c] && quoteCanOpen(input, valueStart, i) {
			quote = c
			i++
			continue
		}
		if closer, opens := p.openBrackets[c]; opens {
			stack = append(stack, closer)
			i++
			continue
		}
		if p.closers[c] {
			if len(stack) == 0 || stack[len(stack)-1] != c {
				return nil, parseError(CodeMalformedBracket, i, "", "unexpected or mismatched closing bracket")
			}
			stack = stack[:len(stack)-1]
			i++
			continue
		}
		if len(stack) == 0 {
			separator, after, matched := p.pairSeparatorAt(input, i)
			if matched {
				next := skipSpace(input, after)
				nextHeader, valid := p.headerAt(input, next)
				if valid {
					candidateStart, candidateEnd := trimSpaceBounds(input, valueStart, i)
					candidate := input[candidateStart:candidateEnd]
					if fieldType == "ip" && net.ParseIP(stripIPZone(candidate)) == nil {
						i++
						continue
					}
					if p.stopKeys[nextHeader.key] || p.keyAllowed(nextHeader.key) {
						return &boundary{start: i, next: next, separator: separator}, nil
					}
				}
			}
		}
		i++
	}
	if quote != 0 {
		return nil, parseError(CodeMalformedQuote, len(input), "", "unterminated quoted value")
	}
	if len(stack) > 0 {
		return nil, parseError(CodeMalformedBracket, len(input), "", "unterminated bracketed value")
	}
	return nil, nil
}

func (p *Parser) headerAt(input string, position int) (header, bool) {
	start := skipSpace(input, position)
	limit := start + p.config.MaxKeyBytes + 16
	if limit > len(input) {
		limit = len(input)
	}
	for i := start; i < limit; i++ {
		for _, separator := range p.assignSeps {
			if !strings.HasPrefix(input[i:], separator) {
				continue
			}
			_, keyEnd := trimSpaceBounds(input, start, i)
			if keyEnd <= start {
				return header{}, false
			}
			key := strings.Clone(input[start:keyEnd])
			if len(key) > p.config.MaxKeyBytes || !p.keyRE.MatchString(key) {
				return header{}, false
			}
			if !p.stopKeys[key] && !p.keyAllowed(key) {
				return header{}, false
			}
			return header{
				key: key, keyStart: start, keyEnd: keyEnd,
				assignStart: i, assignEnd: i + len(separator),
				valueStart: i + len(separator), separator: separator,
			}, true
		}
		if input[i] == '\n' || input[i] == '\r' {
			return header{}, false
		}
	}
	return header{}, false
}

func (p *Parser) pairSeparatorAt(input string, position int) (string, int, bool) {
	if position < len(input) && p.whitespace[input[position]] {
		end := position + 1
		for end < len(input) && p.whitespace[input[end]] {
			end++
		}
		return strings.Clone(input[position:end]), end, true
	}
	for _, separator := range p.pairSeps {
		if len(separator) == 1 && p.whitespace[separator[0]] {
			continue
		}
		if strings.HasPrefix(input[position:], separator) {
			return separator, position + len(separator), true
		}
	}
	return "", position, false
}

func (p *Parser) keyAllowed(key string) bool {
	if len(p.allowRE) == 0 {
		return true
	}
	for _, expression := range p.allowRE {
		if expression.MatchString(key) {
			return true
		}
	}
	return false
}

func (p *Parser) isDenied(key string) bool {
	for _, expression := range p.denyRE {
		if expression.MatchString(key) {
			return true
		}
	}
	return false
}

func (p *Parser) valueType(key string) string {
	if kind, exists := p.config.ValueTypes[key]; exists {
		return strings.ToLower(kind)
	}
	if alias, exists := p.config.KeyAliases[key]; exists {
		return strings.ToLower(p.config.ValueTypes[alias])
	}
	return "string"
}

func (p *Parser) valueTypeForOutput(source, output string) string {
	if kind, exists := p.config.ValueTypes[output]; exists {
		return strings.ToLower(kind)
	}
	if kind, exists := p.config.ValueTypes[source]; exists {
		return strings.ToLower(kind)
	}
	return "string"
}

func (p *Parser) decodeValue(raw string) (string, error) {
	if len(raw) >= 2 && p.quotes[raw[0]] && raw[len(raw)-1] == raw[0] {
		if raw[0] == '"' {
			decoded, err := strconv.Unquote(raw)
			if err != nil {
				return "", err
			}
			return decoded, nil
		}
		return p.unescape(raw[1 : len(raw)-1])
	}
	if len(raw) > 0 {
		if _, bracketed := p.openBrackets[raw[0]]; bracketed {
			return strings.Clone(raw), nil
		}
	}
	return p.unescape(raw)
}

func (p *Parser) unescape(value string) (string, error) {
	if !strings.ContainsRune(value, rune(p.escape)) {
		return strings.Clone(value), nil
	}
	var output strings.Builder
	output.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] != p.escape {
			output.WriteByte(value[i])
			continue
		}
		if i+1 >= len(value) {
			return "", fmt.Errorf("escape character has no following byte")
		}
		next := value[i+1]
		if p.isEscapable(next) {
			output.WriteByte(next)
			i++
			continue
		}
		output.WriteByte(value[i])
	}
	return output.String(), nil
}

func (p *Parser) isEscapable(value byte) bool {
	if value == p.escape || p.quotes[value] || p.closers[value] {
		return true
	}
	if _, exists := p.openBrackets[value]; exists {
		return true
	}
	for _, separator := range p.pairSeps {
		if len(separator) > 0 && separator[0] == value {
			return true
		}
	}
	for _, separator := range p.assignSeps {
		if len(separator) > 0 && separator[0] == value {
			return true
		}
	}
	return false
}

func convertValue(decoded, raw, kind string) (interface{}, error) {
	switch strings.ToLower(kind) {
	case "", "string":
		return decoded, nil
	case "integer", "int":
		value, err := strconv.ParseInt(decoded, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected integer: %w", err)
		}
		return value, nil
	case "unsigned", "uint":
		value, err := strconv.ParseUint(decoded, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected unsigned integer: %w", err)
		}
		return value, nil
	case "float", "number":
		value, err := strconv.ParseFloat(decoded, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("expected finite number")
		}
		return value, nil
	case "boolean", "bool":
		switch strings.ToLower(decoded) {
		case "true", "yes", "on", "1":
			return true, nil
		case "false", "no", "off", "0":
			return false, nil
		default:
			return nil, fmt.Errorf("expected boolean")
		}
	case "ip":
		if net.ParseIP(stripIPZone(decoded)) == nil {
			return nil, fmt.Errorf("expected IP address")
		}
		return decoded, nil
	case "timestamp":
		if !validTimestamp(decoded) {
			return nil, fmt.Errorf("expected timestamp")
		}
		return decoded, nil
	case "json":
		var value interface{}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, fmt.Errorf("expected JSON value: %w", err)
		}
		return value, nil
	case "auto":
		if value, err := strconv.ParseBool(strings.ToLower(decoded)); err == nil {
			return value, nil
		}
		if value, err := strconv.ParseInt(decoded, 10, 64); err == nil {
			return value, nil
		}
		if value, err := strconv.ParseFloat(decoded, 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value, nil
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unsupported type %q", kind)
	}
}

func validTimestamp(value string) bool {
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05",
		"2006-01-02", "20060102 150405.000-0700",
	} {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

func validValueType(kind string) bool {
	switch strings.ToLower(kind) {
	case "", "string", "integer", "int", "unsigned", "uint", "float", "number", "boolean", "bool", "ip", "timestamp", "json", "auto":
		return true
	default:
		return false
	}
}

func validDuplicatePolicy(policy DuplicatePolicy) bool {
	return policy == DuplicateError || policy == DuplicateFirst || policy == DuplicateLast || policy == DuplicateArray
}

func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern == "" || len(pattern) > maxPatternBytes {
			return nil, fmt.Errorf("pattern is empty or too long")
		}
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, expression)
	}
	return compiled, nil
}

func normalizeSeparators(input []string) ([]string, map[byte]bool, error) {
	seen := make(map[string]bool, len(input))
	whitespace := make(map[byte]bool)
	output := make([]string, 0, len(input))
	for _, separator := range input {
		if separator == "" || len(separator) > 16 || strings.ContainsRune(separator, '\x00') {
			return nil, nil, fmt.Errorf("separators must contain 1..16 non-NUL bytes")
		}
		if seen[separator] {
			return nil, nil, fmt.Errorf("duplicate separator %q", separator)
		}
		seen[separator] = true
		output = append(output, separator)
		if len(separator) == 1 && isSpace(separator[0]) {
			whitespace[separator[0]] = true
		}
	}
	sort.Slice(output, func(i, j int) bool {
		if len(output[i]) == len(output[j]) {
			return output[i] < output[j]
		}
		return len(output[i]) > len(output[j])
	})
	return output, whitespace, nil
}

func applyDefaults(input Config) Config {
	defaults := DefaultConfig()
	if len(input.PairSeparators) == 0 {
		input.PairSeparators = defaults.PairSeparators
	}
	if len(input.AssignmentSeparators) == 0 {
		input.AssignmentSeparators = defaults.AssignmentSeparators
	}
	if input.QuoteCharacters == "" {
		input.QuoteCharacters = defaults.QuoteCharacters
	}
	if input.EscapeCharacter == "" {
		input.EscapeCharacter = defaults.EscapeCharacter
	}
	if input.BracketPairs == nil {
		input.BracketPairs = defaults.BracketPairs
	}
	if input.KeyPattern == "" {
		input.KeyPattern = defaults.KeyPattern
	}
	if input.DuplicatePolicy == "" {
		input.DuplicatePolicy = defaults.DuplicatePolicy
	}
	if input.MaxInputBytes == 0 {
		input.MaxInputBytes = defaults.MaxInputBytes
	}
	if input.MaxFields == 0 {
		input.MaxFields = defaults.MaxFields
	}
	if input.MaxKeyBytes == 0 {
		input.MaxKeyBytes = defaults.MaxKeyBytes
	}
	if input.MaxValueBytes == 0 {
		input.MaxValueBytes = defaults.MaxValueBytes
	}
	return input
}

func trimSpaceBounds(input string, start, end int) (int, int) {
	for start < end && isSpace(input[start]) {
		start++
	}
	for end > start && isSpace(input[end-1]) {
		end--
	}
	return start, end
}

func skipSpace(input string, position int) int {
	for position < len(input) && isSpace(input[position]) {
		position++
	}
	return position
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func quoteCanOpen(input string, valueStart, position int) bool {
	if position == valueStart {
		return true
	}
	for i := valueStart; i < position; i++ {
		if !isSpace(input[i]) {
			previous := input[position-1]
			return isSpace(previous) || strings.ContainsRune("=:[{(,", rune(previous))
		}
	}
	return true
}

func stripIPZone(value string) string {
	if index := strings.LastIndexByte(value, '%'); index >= 0 {
		return value[:index]
	}
	return value
}

func configError(detail string) error {
	return &Error{Code: CodeInvalidConfig, Offset: -1, Detail: detail}
}

func parseError(code string, offset int, key, detail string) error {
	return &Error{Code: code, Offset: offset, Key: key, Detail: detail}
}
