// Package grok implements a lightweight Grok-style named pattern engine in
// pure Go. Patterns are re-implemented from scratch; no GPL code copied.
package grok

import (
	"fmt"
	"regexp"
	"strings"
)

// BuiltinPatterns are semantic named patterns inspired by logstash-patterns-core.
// All patterns are original Go regexp syntax implementations.
var BuiltinPatterns = map[string]string{
	"USERNAME":          `[a-zA-Z0-9._-]+`,
	"USER":              `%{USERNAME}`,
	"INT":               `[+-]?[0-9]+`,
	"BASE10NUM":         `[+-]?(?:[0-9]+(?:\.[0-9]+)?)`,
	"NUMBER":            `%{BASE10NUM}`,
	"WORD":              `\b\w+\b`,
	"NOTSPACE":          `\S+`,
	"SPACE":             `\s+`,
	"DATA":              `.*?`,
	"GREEDYDATA":        `.*`,
	"QUOTEDSTRING":      `"(?:[^"\\]|\\.)*"`,
	"UUID":              `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`,
	"MAC":               `[0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){5}`,
	"IPV4":              `\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`,
	"IPV6":              `(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|(?:[0-9a-fA-F]{1,4}:)*::(?:[0-9a-fA-F]{1,4}:)*[0-9a-fA-F]{1,4}|::1|::`,
	"IP":                `(?:%{IPV6}|%{IPV4})`,
	"IPORHOST":          `(?:%{IP}|%{HOSTNAME})`,
	"HOSTNAME":          `\b(?:[0-9A-Za-z][0-9A-Za-z-]{0,62})(?:\.(?:[0-9A-Za-z][0-9A-Za-z-]{0,62}))*\b`,
	"HOST":              `%{HOSTNAME}`,
	"HOSTPORT":          `%{IPORHOST}:%{POSINT}`,
	"URIPATH":           `(?:/[^\s?#]*)+`,
	"URIQUERY":          `[^\s#]*`,
	"URIPROTO":          `[A-Za-z][A-Za-z0-9+\-.]+`,
	"URI":               `%{URIPROTO}://(?:[^\s"'<>]+)`,
	"EMAILADDRESS":      `[^\s@]+@[^\s@]+\.[^\s@]+`,
	"POSINT":            `\b[1-9][0-9]*\b`,
	"NONNEGINT":         `\b[0-9]+\b`,
	"PATH":              `(?:/[^\s"';\[\]{}]*)+`,
	"UNIXPATH":          `(?:/[^\s"';\[\]{}]*)+`,
	"WINPATH":           `[A-Za-z]:\\[^\s"';\[\]{}]+`,
	"YEAR":              `\d{4}`,
	"MONTH":             `\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\b`,
	"MONTHNUM":          `0?[1-9]|1[0-2]`,
	"MONTHDAY":          `(?:0[1-9]|[12][0-9]|3[01]|[1-9])`,
	"HOUR":              `[01]?[0-9]|2[0-3]`,
	"MINUTE":            `[0-5][0-9]`,
	"SECOND":            `(?:[0-5][0-9]|60)(?:[.,][0-9]+)?`,
	"TIME":              `%{HOUR}:%{MINUTE}(?::%{SECOND})?`,
	"DATE_US":           `%{MONTHNUM}[/-]%{MONTHDAY}[/-]%{YEAR}`,
	"DATE_EU":           `%{MONTHDAY}[./-]%{MONTHNUM}[./-]%{YEAR}`,
	"ISO8601_TIMEZONE":  `(?:Z|[+-]%{HOUR}:?%{MINUTE})`,
	"TIMESTAMP_ISO8601": `%{YEAR}-%{MONTHNUM}-%{MONTHDAY}[T ]%{TIME}%{ISO8601_TIMEZONE}?`,
	"SYSLOGTIMESTAMP":   `%{MONTH}\s+%{MONTHDAY}\s+%{TIME}`,
	"HTTPDATE":          `%{MONTHDAY}/%{MONTH}/%{YEAR}:%{TIME}\s[+-]\d{4}`,
	"LOGLEVEL":          `(?i)(?:TRACE|DEBUG|INFO|NOTICE|WARN(?:ING)?|ERR(?:OR)?|CRIT(?:ICAL)?|FATAL|EMERG(?:ENCY)?)`,
	"SYSLOGBASE":        `%{SYSLOGTIMESTAMP:timestamp}\s+%{HOSTNAME:host}\s+%{WORD:program}(?:\[%{POSINT:pid}\])?: `,
	"COMMONAPACHELOG":   `%{IPORHOST:client_ip}\s+%{NOTSPACE:ident}\s+%{NOTSPACE:auth}\s+\[%{HTTPDATE:timestamp}\]\s+"%{WORD:method}\s+%{NOTSPACE:uri}\s+HTTP/%{NUMBER:http_version}"\s+%{INT:status}\s+(?:%{INT:bytes}|-)`,
	"COMBINEDAPACHELOG": `%{COMMONAPACHELOG}\s+%{QUOTEDSTRING:referrer}\s+%{QUOTEDSTRING:agent}`,
}

// Field holds a captured field name and its type coercion hint.
type Field struct {
	Name string
	Type string // "", "int", "float"
}

// Compiled is a compiled Grok pattern.
type Compiled struct {
	RE     *regexp.Regexp
	Fields []Field
}

// Engine compiles and applies Grok patterns.
type Engine struct {
	patterns map[string]string
}

// New creates a Grok engine with all built-in patterns plus any extras.
func New(extra map[string]string) *Engine {
	p := make(map[string]string, len(BuiltinPatterns)+len(extra))
	for k, v := range BuiltinPatterns {
		p[k] = v
	}
	for k, v := range extra {
		p[k] = v
	}
	return &Engine{patterns: p}
}

// Compile compiles a Grok pattern string into a regexp + field list.
// Pattern syntax: %{PATTERN_NAME} or %{PATTERN_NAME:field} or %{PATTERN_NAME:field:type}
func (e *Engine) Compile(pattern string) (*Compiled, error) {
	var fields []Field
	re, err := e.expand(pattern, &fields, make(map[string]bool), 0)
	if err != nil {
		return nil, err
	}
	compiled, err := regexp.Compile(re)
	if err != nil {
		return nil, fmt.Errorf("grok compile %q: %w", pattern, err)
	}
	return &Compiled{RE: compiled, Fields: fields}, nil
}

// Match applies a compiled pattern and returns the named captures.
func (c *Compiled) Match(s string) map[string]string {
	m := c.RE.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	result := make(map[string]string)
	names := c.RE.SubexpNames()
	for i, name := range names {
		if i > 0 && name != "" && i < len(m) {
			result[name] = m[i]
		}
	}
	return result
}

// expand recursively substitutes %{PATTERN} references.
func (e *Engine) expand(pattern string, fields *[]Field, seen map[string]bool, depth int) (string, error) {
	if depth > 10 {
		return "", fmt.Errorf("grok pattern expansion too deep (cycle?)")
	}

	var b strings.Builder
	i := 0
	for i < len(pattern) {
		if i+1 < len(pattern) && pattern[i] == '%' && pattern[i+1] == '{' {
			// Find closing brace
			end := strings.Index(pattern[i:], "}")
			if end < 0 {
				return "", fmt.Errorf("unclosed %% in grok pattern")
			}
			ref := pattern[i+2 : i+end]
			i += end + 1

			parts := strings.SplitN(ref, ":", 3)
			patName := parts[0]
			fieldName := ""
			fieldType := ""
			if len(parts) >= 2 {
				fieldName = parts[1]
			}
			if len(parts) >= 3 {
				fieldType = parts[2]
			}

			patBody, ok := e.patterns[patName]
			if !ok {
				return "", fmt.Errorf("unknown grok pattern %q", patName)
			}

			// Detect cycles
			if seen[patName] {
				return "", fmt.Errorf("grok pattern cycle involving %q", patName)
			}
			seen[patName] = true
			expanded, err := e.expand(patBody, fields, seen, depth+1)
			delete(seen, patName)
			if err != nil {
				return "", err
			}

			if fieldName != "" {
				*fields = append(*fields, Field{Name: fieldName, Type: fieldType})
				b.WriteString("(?P<" + fieldName + ">" + expanded + ")")
			} else {
				b.WriteString("(?:" + expanded + ")")
			}
		} else {
			b.WriteByte(pattern[i])
			i++
		}
	}
	return b.String(), nil
}
