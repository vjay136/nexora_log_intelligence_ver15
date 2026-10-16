// Package typeinfer infers field types from string values.
package typeinfer

import (
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reBool      = regexp.MustCompile(`(?i)^(?:true|false|yes|no|on|off)$`)
	reTimestamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
	reEmail     = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	reURL       = regexp.MustCompile(`^https?://`)
)

// Infer returns the most specific type for value s.
// Returns one of: "string", "integer", "float", "boolean", "ip", "timestamp", "email", "url"
func Infer(s string) string {
	if s == "" {
		return "string"
	}
	// Boolean
	if reBool.MatchString(s) {
		return "boolean"
	}
	// Integer
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return "integer"
	}
	// Float
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return "float"
	}
	// IP address
	if net.ParseIP(s) != nil {
		return "ip"
	}
	// Timestamp
	if reTimestamp.MatchString(s) {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if _, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
				return "timestamp"
			}
		}
	}
	// Email
	if reEmail.MatchString(s) {
		return "email"
	}
	// URL
	if reURL.MatchString(s) {
		return "url"
	}
	return "string"
}

// Coerce converts a string value to its inferred Go type.
func Coerce(s string) interface{} {
	switch Infer(s) {
	case "integer":
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
	case "float":
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	case "boolean":
		lower := strings.ToLower(s)
		return lower == "true" || lower == "yes" || lower == "on"
	}
	return s
}
