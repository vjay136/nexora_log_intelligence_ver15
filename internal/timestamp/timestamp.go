// Package timestamp extracts and normalizes timestamps from log lines.
package timestamp

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Result holds the extracted timestamp and metadata.
type Result struct {
	Time             time.Time
	OriginalTimezone string // Z, +02:00, or +0200 when present in the source
	Inferred         bool   // true if no timestamp found; ingest time used
	Format           string // which format matched
}

type formatSpec struct {
	name     string
	re       *regexp.Regexp
	layouts  []string
	withYear bool
}

var (
	reISO         = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:?\d{2})?`)
	reApache      = regexp.MustCompile(`\d{2}/(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}`)
	reSyslogYear  = regexp.MustCompile(`(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+\d{4}\s+\d{2}:\d{2}:\d{2}`)
	reSyslog      = regexp.MustCompile(`(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}`)
	reSMTPCompact = regexp.MustCompile(`\d{8}\s+\d{9}[+-]\d{4}`)
	reSMTPDotted  = regexp.MustCompile(`\d{8}\s+\d{6}(?:\.\d{1,9})[+-]\d{4}`)
	reZone        = regexp.MustCompile(`(?:Z|[+-]\d{2}:?\d{2})$`)

	formats = []formatSpec{
		{name: "iso8601", re: reISO, layouts: []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05Z07:00", "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999"}, withYear: true},
		{name: "apache_clf", re: reApache, layouts: []string{"02/Jan/2006:15:04:05 -0700"}, withYear: true},
		{name: "syslog_year", re: reSyslogYear, layouts: []string{"Jan _2 2006 15:04:05"}, withYear: true},
		{name: "syslog_rfc3164", re: reSyslog, layouts: []string{"2006 Jan _2 15:04:05"}},
		{name: "smtp_native", re: reSMTPDotted, layouts: []string{"20060102 150405.999999999-0700"}, withYear: true},
	}
)

// Extract searches s for a timestamp and returns the result. ingestTime is used
// both for RFC3164's missing year and as the fallback when no timestamp exists.
func Extract(s string, ingestTime time.Time) Result {
	if ts, ok := tryEpoch(s); ok {
		return Result{Time: ts.UTC(), Format: "epoch"}
	}

	// Nexora's native format stores HHMMSSmmm without a decimal separator.
	if raw := reSMTPCompact.FindString(s); raw != "" {
		parts := strings.Fields(raw)
		if len(parts) == 2 && len(parts[1]) == 14 {
			normalized := parts[0] + " " + parts[1][:6] + "." + parts[1][6:9] + parts[1][9:]
			if parsed, err := time.Parse("20060102 150405.000-0700", normalized); err == nil {
				return Result{Time: parsed.UTC(), OriginalTimezone: parts[1][9:], Format: "smtp_native"}
			}
		}
	}

	for _, spec := range formats {
		raw := spec.re.FindString(s)
		if raw == "" {
			continue
		}
		parseValue := raw
		if !spec.withYear {
			parseValue = strconv.Itoa(ingestTime.Year()) + " " + raw
		}
		for _, layout := range spec.layouts {
			var parsed time.Time
			var err error
			if strings.Contains(layout, "Z07") || strings.Contains(layout, "-0700") {
				parsed, err = time.Parse(layout, parseValue)
			} else {
				parsed, err = time.ParseInLocation(layout, parseValue, ingestTime.Location())
			}
			if err == nil {
				return Result{Time: parsed.UTC(), OriginalTimezone: sourceZone(raw), Format: spec.name}
			}
		}
	}

	return Result{Time: ingestTime.UTC(), Inferred: true, Format: "ingest"}
}

// tryEpoch detects Unix seconds, fractional seconds, or milliseconds in the
// first three whitespace-delimited fields. Punctuation wrappers are ignored.
func tryEpoch(s string) (time.Time, bool) {
	fields := strings.Fields(s)
	limit := len(fields)
	if limit > 3 {
		limit = 3
	}
	for _, field := range fields[:limit] {
		field = strings.Trim(field, `[](),;`)
		if v, err := strconv.ParseInt(field, 10, 64); err == nil {
			if v > 1e12 && v < 2e13 {
				return time.UnixMilli(v), true
			}
			if v > 1e9 && v < 2e10 {
				return time.Unix(v, 0), true
			}
		}
		if v, err := strconv.ParseFloat(field, 64); err == nil && v > 1e9 && v < 2e10 {
			seconds := int64(v)
			nanos := int64((v - float64(seconds)) * 1e9)
			return time.Unix(seconds, nanos), true
		}
	}
	return time.Time{}, false
}

func sourceZone(raw string) string {
	if zone := reZone.FindString(raw); zone != "" {
		return zone
	}
	return ""
}
