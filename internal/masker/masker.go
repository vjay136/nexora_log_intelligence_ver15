// Package masker applies variable masking before template mining and
// secret-field redaction on output fields.
package masker

import (
	"regexp"
	"strings"
)

// Token replacements for template mining (before Drain clustering).
const (
	TokenIP    = "<IP>"
	TokenIPv6  = "<IPv6>"
	TokenEmail = "<EMAIL>"
	TokenUUID  = "<UUID>"
	TokenURL   = "<URL>"
	TokenHex   = "<HEX>"
	TokenNum   = "<NUM>"
	TokenPath  = "<PATH>"
	Redacted   = "[REDACTED]"
)

// MaskType identifies which variable types to mask.
type MaskType string

const (
	MaskIPv4   MaskType = "ipv4"
	MaskIPv6T  MaskType = "ipv6"
	MaskEmailT MaskType = "email"
	MaskUUIDT  MaskType = "uuid"
	MaskURLT   MaskType = "url"
	MaskHexT   MaskType = "hex"
	MaskIntT   MaskType = "integer"
	MaskPathT  MaskType = "path"
)

var (
	reIPv4  = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`)
	reIPv6  = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}\b|\b(?:[0-9a-f]{1,4}:)*::(?:[0-9a-f]{1,4}:)*[0-9a-f]{1,4}\b|\b::1\b|\b::\b`)
	reEmail = regexp.MustCompile(`[^\s@]+@[^\s@]+\.[^\s@]+`)
	reUUID  = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	reURL   = regexp.MustCompile(`https?://\S+`)
	reHex   = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	reHexID = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	reInt   = regexp.MustCompile(`\b\d{2,}\b`) // 2+ digit standalone integers
	rePath  = regexp.MustCompile(`(?:/[^\s"';\[\]{}]+)+|(?:[A-Za-z]:\\[^\s"';\[\]{}]+)`)

	reSecretKey   = regexp.MustCompile(`(?i)password|secret|token|api_?key|credential|passwd|auth`)
	reSecretValue = regexp.MustCompile(`(?i)((?:password|secret|token|api_?key|credential|passwd|auth)\s*[:=]\s*)("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s:,;{}\[\]]+)`)
)

// Config controls which mask types are active. All enabled by default.
type Config struct {
	DisableTypes []MaskType `json:"disable_types"`
}

// Masker applies masking rules to log lines.
type Masker struct {
	enabled map[MaskType]bool
}

// New creates a Masker from config. Pass nil for all-enabled defaults.
func New(cfg *Config) *Masker {
	m := &Masker{enabled: map[MaskType]bool{
		MaskIPv4:   true,
		MaskIPv6T:  true,
		MaskEmailT: true,
		MaskUUIDT:  true,
		MaskURLT:   true,
		MaskHexT:   true,
		MaskIntT:   true,
		MaskPathT:  true,
	}}
	if cfg != nil {
		for _, t := range cfg.DisableTypes {
			m.enabled[t] = false
		}
	}
	return m
}

// MaskForTemplate masks a log line for use in Drain template mining.
// Order matters: URL before IP (URLs contain IPs), UUID before hex, email before domain.
func (m *Masker) MaskForTemplate(s string) string {
	// A disabled type must survive every later rule. For example, disabling
	// IPv4 must not allow the integer rule to turn 10.0.0.1 into <NUM>.0.0.1.
	protected := make([]string, 0)
	protect := func(re *regexp.Regexp) {
		s = re.ReplaceAllStringFunc(s, func(value string) string {
			marker := "\x00NLI_PROTECTED_" + string(rune(len(protected)+0xE000)) + "\x00"
			protected = append(protected, value)
			return marker
		})
	}
	if !m.enabled[MaskURLT] {
		protect(reURL)
	}
	if !m.enabled[MaskEmailT] {
		protect(reEmail)
	}
	if !m.enabled[MaskUUIDT] {
		protect(reUUID)
	}
	if !m.enabled[MaskIPv6T] {
		protect(reIPv6)
	}
	if !m.enabled[MaskIPv4] {
		protect(reIPv4)
	}
	if !m.enabled[MaskHexT] {
		protect(reHex)
		protect(reHexID)
	}
	if !m.enabled[MaskPathT] {
		protect(rePath)
	}
	if !m.enabled[MaskIntT] {
		protect(reInt)
	}

	if m.enabled[MaskURLT] {
		s = reURL.ReplaceAllString(s, TokenURL)
	}
	if m.enabled[MaskEmailT] {
		s = reEmail.ReplaceAllString(s, TokenEmail)
	}
	if m.enabled[MaskUUIDT] {
		s = reUUID.ReplaceAllString(s, TokenUUID)
	}
	if m.enabled[MaskIPv6T] {
		s = reIPv6.ReplaceAllString(s, TokenIPv6)
	}
	if m.enabled[MaskIPv4] {
		s = reIPv4.ReplaceAllString(s, TokenIP)
	}
	if m.enabled[MaskHexT] {
		s = reHex.ReplaceAllString(s, TokenHex)
		s = reHexID.ReplaceAllStringFunc(s, func(h string) string {
			if strings.HasPrefix(h, "<") {
				return h
			}
			return TokenHex
		})
	}
	if m.enabled[MaskIntT] {
		s = reInt.ReplaceAllString(s, TokenNum)
	}
	if m.enabled[MaskPathT] {
		s = rePath.ReplaceAllString(s, TokenPath)
	}

	for i, value := range protected {
		marker := "\x00NLI_PROTECTED_" + string(rune(i+0xE000)) + "\x00"
		s = strings.ReplaceAll(s, marker, value)
	}
	return s
}

// MaskSecrets redacts secret values in a string (for output fields).
func MaskSecrets(s string) string {
	// Avoid the comparatively expensive case-insensitive replacement engine
	// for ordinary payloads. The precheck uses the exact marker vocabulary of
	// reSecretValue; a possible match still goes through the authoritative RE2
	// expression below.
	lower := strings.ToLower(s)
	if !strings.Contains(lower, "password") &&
		!strings.Contains(lower, "secret") &&
		!strings.Contains(lower, "token") &&
		!strings.Contains(lower, "apikey") &&
		!strings.Contains(lower, "api_key") &&
		!strings.Contains(lower, "credential") &&
		!strings.Contains(lower, "passwd") &&
		!strings.Contains(lower, "auth") {
		return s
	}
	return reSecretValue.ReplaceAllString(s, `${1}`+Redacted)
}

// MaskSecretKey returns true if a field key looks like it holds a secret.
func MaskSecretKey(key string) bool {
	return reSecretKey.MatchString(key)
}

// MaskValue recursively masks secret fields in a map/slice/string value.
func MaskValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		for k, x := range v {
			if MaskSecretKey(k) {
				v[k] = Redacted
			} else {
				v[k] = MaskValue(x)
			}
		}
		return v
	case []interface{}:
		for i := range v {
			v[i] = MaskValue(v[i])
		}
		return v
	case string:
		return MaskSecrets(v)
	default:
		return v
	}
}
