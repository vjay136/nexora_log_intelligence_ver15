// Package semanticextract provides bounded, opt-in extraction of common
// network and email identities from untrusted log text.
package semanticextract

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	defaultMaxInput  = 1 << 20
	defaultMaxHeader = 64 << 10
	hardMaxInput     = 1 << 20
	hardMaxHeader    = 64 << 10
)

type Config struct {
	IPv4              bool
	IPv6              bool
	Domain            bool
	URL               bool
	Email             bool
	DecodeMIMEHeaders bool
	MaxInputBytes     int
	MaxHeaderBytes    int
}

type Entity struct {
	Value string `json:"value"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type Result struct {
	Entities     map[string][]Entity `json:"entities"`
	Subject      string              `json:"subject,omitempty"`
	SubjectStart int                 `json:"subject_start,omitempty"`
	SubjectEnd   int                 `json:"subject_end,omitempty"`
	Errors       []string            `json:"errors,omitempty"`
}

var (
	ipv4Candidate   = regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}`)
	ipv6Candidate   = regexp.MustCompile(`(?i)\[?[0-9a-f:]{2,}(?:%[a-z0-9_.-]+)?\]?`)
	domainCandidate = regexp.MustCompile(`(?i)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.?`)
	urlCandidate    = regexp.MustCompile(`(?i)\b(?:https?|ftp)://[^\s<>"']+`)
	emailCandidate  = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+`)
)

func normalizedLimits(config Config) (int, int, error) {
	input, header := config.MaxInputBytes, config.MaxHeaderBytes
	if input == 0 {
		input = defaultMaxInput
	}
	if header == 0 {
		header = defaultMaxHeader
	}
	if input < 1 || input > hardMaxInput || header < 1 || header > hardMaxHeader || header > input {
		return 0, 0, errors.New("semantic extraction limits exceed the 1 MiB input / 64 KiB header bounds")
	}
	return input, header, nil
}

// Extract returns normalized entity values and byte offsets into input. The
// input is never retained. URL user information is removed before emission.
func Extract(input string, config Config) Result {
	result := Result{Entities: map[string][]Entity{}}
	maxInput, maxHeader, err := normalizedLimits(config)
	if err != nil {
		result.Errors = append(result.Errors, "invalid_limits")
		return result
	}
	if len(input) > maxInput {
		result.Errors = append(result.Errors, "input_too_large")
		return result
	}
	if config.IPv4 {
		result.Entities["ipv4"] = scanIPs(input, ipv4Candidate, false)
	}
	if config.IPv6 {
		result.Entities["ipv6"] = scanIPs(input, ipv6Candidate, true)
	}
	if config.Domain {
		result.Entities["domain"] = scanDomains(input)
	}
	if config.URL {
		result.Entities["url"] = scanURLs(input)
	}
	if config.Email {
		result.Entities["email"] = scanEmails(input)
	}
	if config.DecodeMIMEHeaders {
		decoded, start, end, decodeErr := DecodeSubject(input, maxHeader)
		if decodeErr != nil {
			result.Errors = append(result.Errors, errorCode(decodeErr))
		} else if decoded != "" {
			result.Subject, result.SubjectStart, result.SubjectEnd = decoded, start, end
			result.Entities["email_subject_decoded"] = []Entity{{Value: decoded, Start: start, End: end}}
		}
	}
	return result
}

func scanIPs(input string, pattern *regexp.Regexp, ipv6 bool) []Entity {
	matches := pattern.FindAllStringIndex(input, 1024)
	out := make([]Entity, 0, len(matches))
	seen := map[string]bool{}
	for _, span := range matches {
		candidate := strings.Trim(input[span[0]:span[1]], "[]")
		ip := net.ParseIP(candidate)
		if ip == nil || ipv6 != (ip.To4() == nil) {
			continue
		}
		value := ip.String() // RFC 5952 canonical IPv6 form; IPv4 is dotted decimal.
		if !seen[value] {
			seen[value] = true
			out = append(out, Entity{Value: value, Start: span[0], End: span[1]})
		}
	}
	return out
}

func scanDomains(input string) []Entity {
	matches := domainCandidate.FindAllStringIndex(input, 1024)
	out := make([]Entity, 0, len(matches))
	seen := map[string]bool{}
	for _, span := range matches {
		candidate := strings.TrimSuffix(strings.ToLower(input[span[0]:span[1]]), ".")
		if net.ParseIP(candidate) != nil || !validDomain(candidate) || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, Entity{Value: candidate, Start: span[0], End: span[1]})
	}
	return out
}

func validDomain(value string) bool {
	if len(value) == 0 || len(value) > 253 || !strings.Contains(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

func scanURLs(input string) []Entity {
	matches := urlCandidate.FindAllStringIndex(input, 1024)
	out := make([]Entity, 0, len(matches))
	seen := map[string]bool{}
	for _, span := range matches {
		start, end := span[0], span[1]
		for end > start && strings.ContainsRune(".,;:!?)]}", rune(input[end-1])) {
			end--
		}
		candidate := input[start:end]
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		parsed.User = nil
		query := parsed.Query()
		for key := range query {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "credential") || strings.Contains(lower, "api_key") || lower == "auth" {
				query.Set(key, "[REDACTED]")
			}
		}
		parsed.RawQuery = query.Encode()
		value := parsed.String()
		if !seen[value] {
			seen[value] = true
			out = append(out, Entity{Value: value, Start: start, End: end})
		}
	}
	return out
}

func scanEmails(input string) []Entity {
	matches := emailCandidate.FindAllStringIndex(input, 1024)
	urlSpans := urlCandidate.FindAllStringIndex(input, 1024)
	out := make([]Entity, 0, len(matches))
	seen := map[string]bool{}
	for _, span := range matches {
		insideURL := false
		for _, urlSpan := range urlSpans {
			if span[0] >= urlSpan[0] && span[1] <= urlSpan[1] {
				insideURL = true
				break
			}
		}
		if insideURL {
			continue
		}
		start := span[0]
		candidate := strings.Trim(input[start:span[1]], ".")
		if equals := strings.LastIndexByte(candidate, '='); equals > 0 {
			prefix := candidate[:equals]
			if knownFieldLabel(prefix) {
				start += equals + 1
				candidate = candidate[equals+1:]
			}
		}
		address, err := mail.ParseAddress(candidate)
		if err != nil || address.Address != candidate || seen[strings.ToLower(candidate)] {
			continue
		}
		seen[strings.ToLower(candidate)] = true
		out = append(out, Entity{Value: candidate, Start: start, End: start + len(candidate)})
	}
	return out
}

func knownFieldLabel(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
			continue
		}
		return false
	}
	return true
}

// DecodeMIMEHeader safely decodes RFC 2047 Q/B encoded words. The caller sets
// a byte bound; unsupported charsets and malformed words return diagnostics.
func DecodeMIMEHeader(value string, maxBytes int) (string, error) {
	if maxBytes <= 0 || maxBytes > hardMaxHeader || len(value) > maxBytes {
		return "", errors.New("header_too_large")
	}
	decoder := &mime.WordDecoder{CharsetReader: decodeCharset}
	decoded, err := decoder.DecodeHeader(value)
	if err != nil {
		return "", fmt.Errorf("mime_decode: %w", err)
	}
	if strings.Contains(value, "=?") && decoded == value {
		return "", errors.New("mime_decode: malformed encoded-word")
	}
	return decoded, nil
}

func DecodeSubject(input string, maxHeaderBytes int) (string, int, int, error) {
	if maxHeaderBytes <= 0 || maxHeaderBytes > hardMaxHeader {
		return "", 0, 0, errors.New("header_too_large")
	}
	reader := bufio.NewReader(strings.NewReader(input))
	type headerValue struct {
		name, value string
		start, end  int
	}
	headers := make([]headerValue, 0, 32)
	currentIndex := -1
	offset, used := 0, 0
	for {
		line, err := reader.ReadString('\n')
		if len(line) > maxHeaderBytes-used {
			return "", 0, 0, errors.New("header_too_large")
		}
		used += len(line)
		lineStart := offset
		offset += len(line)
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if currentIndex < 0 {
				return "", 0, 0, errors.New("malformed_header_fold")
			}
			headers[currentIndex].value += " " + strings.TrimSpace(line)
			headers[currentIndex].end = offset
		} else {
			colon := strings.IndexByte(line, ':')
			if colon <= 0 {
				if len(headers) == 0 {
					return "", 0, 0, nil
				}
				return "", 0, 0, errors.New("malformed_header")
			}
			name := line[:colon]
			if !validHeaderName(name) {
				return "", 0, 0, errors.New("malformed_header_name")
			}
			value := strings.TrimSpace(line[colon+1:])
			if strings.ContainsAny(value, "\r\x00") {
				return "", 0, 0, errors.New("malformed_header")
			}
			headers = append(headers, headerValue{name: strings.ToLower(name), value: value, start: lineStart + colon + 1, end: offset})
			currentIndex = len(headers) - 1
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", 0, 0, errors.New("malformed_header")
		}
	}
	for _, header := range headers {
		if header.name != "subject" {
			continue
		}
		decoded, err := DecodeMIMEHeader(header.value, maxHeaderBytes)
		if err != nil {
			return "", 0, 0, err
		}
		return decoded, header.start, header.end, nil
	}
	return "", 0, 0, nil
}

func validHeaderName(value string) bool {
	for _, char := range value {
		if char <= 32 || char >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?=", char) {
			return false
		}
	}
	return value != ""
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.Contains(message, "header_too_large") {
		return "header_too_large"
	}
	if strings.Contains(message, "malformed_header") {
		return "malformed_header"
	}
	return "mime_decode_failed"
}

func decodeCharset(charset string, input io.Reader) (io.Reader, error) {
	data, err := io.ReadAll(io.LimitReader(input, hardMaxHeader+1))
	if err != nil || len(data) > hardMaxHeader {
		return nil, errors.New("charset input exceeds header limit")
	}
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "utf-8", "utf8":
		if !utf8.Valid(data) {
			return nil, errors.New("invalid UTF-8 MIME header")
		}
		return strings.NewReader(string(data)), nil
	case "us-ascii", "ascii":
		for _, value := range data {
			if value > 127 {
				return nil, errors.New("non-ASCII byte in ASCII MIME header")
			}
		}
		return strings.NewReader(string(data)), nil
	case "iso-8859-1", "latin1", "windows-1252", "cp1252":
		runes := make([]rune, len(data))
		for index, value := range data {
			runes[index] = rune(value)
			if strings.Contains(charset, "1252") && value >= 0x80 && value <= 0x9f {
				runes[index] = windows1252[value-0x80]
			}
		}
		return strings.NewReader(string(runes)), nil
	default:
		return nil, fmt.Errorf("unsupported MIME charset %q", charset)
	}
}

var windows1252 = []rune{0x20ac, 0x0081, 0x201a, 0x0192, 0x201e, 0x2026, 0x2020, 0x2021, 0x02c6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008d, 0x017d, 0x008f, 0x0090, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014, 0x02dc, 0x2122, 0x0161, 0x203a, 0x0153, 0x009d, 0x017e, 0x0178}
