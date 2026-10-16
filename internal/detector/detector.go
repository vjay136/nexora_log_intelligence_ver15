// Package detector identifies the format of a raw log line.
package detector

import (
	"regexp"
	"strings"
)

// Format constants returned by Detect.
const (
	FormatJSON          = "json"
	FormatSyslogRFC5424 = "syslog_rfc5424"
	FormatSyslogRFC3164 = "syslog_rfc3164"
	FormatLogfmt        = "logfmt"
	FormatCLF           = "clf"
	FormatNginx         = "nginx"
	FormatCEF           = "cef"
	FormatSMTPNative    = "smtp_native"
	FormatKVPairs       = "kvpairs"
	FormatPlain         = "plain"
)

var (
	// Syslog RFC5424: <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID
	reSyslog5424 = regexp.MustCompile(`^<\d{1,3}>\d `)
	// Syslog RFC3164: <PRI>TIMESTAMP or just TIMESTAMP HOST PROC:
	reSyslog3164PRI  = regexp.MustCompile(`^<\d{1,3}>`)
	reSyslog3164Date = regexp.MustCompile(`^(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\s+\S+\s+\S+:`)
	// SMTP native: YYYYMMDD HHMMSS.sss±HHMM host service pid ...
	reSMTPNative = regexp.MustCompile(`^\d{8}\s+\d{9}[+-]\d{4}\s+\S+\s+\S+\s+\d+\s+\d+\s+\d+\s+\w+;`)
	// Apache CLF: IP - - [date] "METHOD URI HTTP/x.y" STATUS BYTES
	reCLF = regexp.MustCompile(`^\S+\s+\S+\s+\S+\s+\[\d{2}/\w+/\d{4}:\d{2}:\d{2}:\d{2}\s[+-]\d{4}\]\s+"[A-Z]+\s+\S+\s+HTTP/\d`)
	// Nginx extends CLF with upstream_addr, request_time etc.
	reNginx = regexp.MustCompile(`^\S+\s+\S+\s+\S+\s+\[.*?\]\s+"[A-Z]+.*?"\s+\d+\s+\d+\s+".*?"\s+".*?"\s+(?:rt=|\d+\.\d+)`)
	// CEF
	reCEF = regexp.MustCompile(`^CEF:\d+\|`)
)

// Detect returns the most specific format for the given line.
func Detect(line string) string {
	if line == "" {
		return FormatPlain
	}

	// 1. JSON
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "{") {
		return FormatJSON
	}

	// 2. Syslog RFC5424 (<PRI>VERSION ...)
	if reSyslog5424.MatchString(trimmed) {
		return FormatSyslogRFC5424
	}

	// 3. Syslog RFC3164 (<PRI>... or datestamp HOST PROC:)
	if reSyslog3164PRI.MatchString(trimmed) || reSyslog3164Date.MatchString(trimmed) {
		return FormatSyslogRFC3164
	}

	// 4. CEF
	if reCEF.MatchString(trimmed) {
		return FormatCEF
	}

	// 5. SMTP native
	if reSMTPNative.MatchString(trimmed) {
		return FormatSMTPNative
	}

	// 6. Nginx (check before CLF, it's a superset)
	if reNginx.MatchString(trimmed) {
		return FormatNginx
	}

	// 7. Apache CLF
	if reCLF.MatchString(trimmed) {
		return FormatCLF
	}

	// 8. Logfmt: majority of space-separated tokens are key=value
	if isLogfmt(trimmed) {
		return FormatLogfmt
	}

	// 9. kvpairs: at least 2 key=value or key:value pairs
	if isKVPairs(trimmed) {
		return FormatKVPairs
	}

	return FormatPlain
}

// isLogfmt returns true when ≥60% of space-separated tokens look like key=value.
func isLogfmt(line string) bool {
	reKV := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]*=`)
	tokens := strings.Fields(line)
	if len(tokens) < 2 {
		return false
	}
	matches := 0
	for _, t := range tokens {
		if reKV.MatchString(t) {
			matches++
		}
	}
	return float64(matches)/float64(len(tokens)) >= 0.6
}

// isKVPairs returns true when the line contains ≥2 key=value or key:value pairs.
func isKVPairs(line string) bool {
	reKV := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.\-]*\s*[:=]\s*\S`)
	matches := reKV.FindAllString(line, -1)
	return len(matches) >= 2
}

// Candidates returns parser strategies in deterministic priority order. The
// detector's best match is always first; remaining formats are fallbacks used
// by the assurance engine. The list is deliberately bounded and duplicate-free.
func Candidates(line string) []string {
	primary := Detect(line)
	ordered := []string{
		primary,
		FormatJSON,
		FormatSyslogRFC5424,
		FormatSyslogRFC3164,
		FormatCEF,
		FormatSMTPNative,
		FormatNginx,
		FormatCLF,
		FormatLogfmt,
		FormatKVPairs,
		FormatPlain,
	}
	seen := make(map[string]bool, len(ordered))
	out := make([]string, 0, len(ordered))
	for _, format := range ordered {
		if !seen[format] {
			seen[format] = true
			out = append(out, format)
		}
	}
	return out
}
