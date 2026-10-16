// Package registry - built-in parser definitions.
package registry

// BuiltinParsers returns all parser definitions shipped with NLI v4.
func BuiltinParsers() []*ParserDef {
	return []*ParserDef{
		{
			ID: "generic.json", Version: 1,
			Description: "Any valid JSON object",
			Format:      "json",
			Fields:      []FieldDef{{Name: "_json", Source: "json:*", Type: "string"}},
		},
		{
			ID: "generic.kv", Version: 1,
			Description: "Generic key=value pairs",
			Format:      "kvpairs",
			Fields:      []FieldDef{{Name: "_kv", Source: "kv:*", Type: "string"}},
		},
		{
			ID: "generic.logfmt", Version: 1,
			Description: "logfmt key=value format",
			Format:      "logfmt",
			Fields:      []FieldDef{{Name: "_logfmt", Source: "kv:*", Type: "string"}},
		},
		{
			ID: "syslog.rfc3164", Version: 1,
			Description: "BSD syslog (RFC 3164)",
			Format:      "syslog_rfc3164",
			Fields: []FieldDef{
				{Name: "timestamp", Source: "grok:SYSLOGTIMESTAMP", Type: "timestamp"},
				{Name: "host", Source: "grok:HOSTNAME", Type: "string", Required: true},
				{Name: "program", Source: "grok:WORD", Type: "string"},
				{Name: "pid", Source: "grok:POSINT", Type: "integer"},
				{Name: "message", Source: "grok:GREEDYDATA", Type: "string"},
			},
			Tests: []RegressionTest{
				{Input: "Oct  2 10:00:00 myhost sshd[1234]: Accepted password for user", Expected: map[string]interface{}{"host": "myhost", "program": "sshd"}},
			},
		},
		{
			ID: "syslog.rfc5424", Version: 1,
			Description: "IETF syslog (RFC 5424)",
			Format:      "syslog_rfc5424",
			Fields: []FieldDef{
				{Name: "priority", Source: "group:1", Type: "integer"},
				{Name: "version", Source: "group:2", Type: "integer"},
				{Name: "timestamp", Source: "group:3", Type: "timestamp"},
				{Name: "host", Source: "group:4", Type: "string", Required: true},
				{Name: "app", Source: "group:5", Type: "string"},
				{Name: "procid", Source: "group:6", Type: "string"},
				{Name: "msgid", Source: "group:7", Type: "string"},
				{Name: "message", Source: "group:8", Type: "string"},
			},
		},
		{
			ID: "web.apache_clf", Version: 1,
			Description: "Apache combined log format",
			Format:      "clf",
			Fields: []FieldDef{
				{Name: "client_ip", Source: "grok:IPORHOST", Type: "ip", Required: true},
				{Name: "timestamp", Source: "grok:HTTPDATE", Type: "timestamp"},
				{Name: "method", Source: "grok:WORD", Type: "string"},
				{Name: "uri", Source: "grok:NOTSPACE", Type: "url"},
				{Name: "http_version", Source: "grok:NUMBER", Type: "float"},
				{Name: "status", Source: "grok:INT", Type: "integer"},
				{Name: "bytes", Source: "grok:INT", Type: "integer"},
				{Name: "referrer", Source: "grok:QUOTEDSTRING", Type: "string"},
				{Name: "user_agent", Source: "grok:QUOTEDSTRING", Type: "string"},
			},
			Tests: []RegressionTest{
				{Input: `192.168.1.1 - - [02/Oct/2026:10:00:00 +0000] "GET /index.html HTTP/1.1" 200 1234 "-" "Mozilla/5.0"`, Expected: map[string]interface{}{"status": int64(200), "method": "GET"}},
			},
		},
		{
			ID: "web.nginx_access", Version: 1,
			Description: "nginx access log",
			Format:      "nginx",
			Fields: []FieldDef{
				{Name: "client_ip", Source: "grok:IPORHOST", Type: "ip", Required: true},
				{Name: "timestamp", Source: "grok:HTTPDATE", Type: "timestamp"},
				{Name: "method", Source: "grok:WORD", Type: "string"},
				{Name: "uri", Source: "grok:NOTSPACE", Type: "url"},
				{Name: "status", Source: "grok:INT", Type: "integer"},
				{Name: "bytes_sent", Source: "grok:INT", Type: "integer"},
				{Name: "request_time", Source: "grok:NUMBER", Type: "float"},
			},
		},
		{
			ID: "web.nginx_error", Version: 1,
			Description: "nginx error log",
			Format:      "plain",
			Fields: []FieldDef{
				{Name: "timestamp", Source: "grok:TIMESTAMP_ISO8601", Type: "timestamp"},
				{Name: "level", Source: "grok:LOGLEVEL", Type: "string"},
				{Name: "pid", Source: "grok:POSINT", Type: "integer"},
				{Name: "message", Source: "grok:GREEDYDATA", Type: "string"},
			},
		},
		{
			ID: "smtp.dmarc.aggregate", Version: 4,
			Description: "SMTP DMARC aggregate report record (Nexora native format)",
			Format:      "smtp_native",
			Fields: []FieldDef{
				{Name: "id", Source: "kv:id", Type: "string", Required: true},
				{Name: "adkim", Source: "kv:adkim", Type: "string"},
				{Name: "aspf", Source: "kv:aspf", Type: "string"},
				{Name: "p", Source: "kv:p", Type: "string"},
				{Name: "pct", Source: "kv:pct", Type: "integer"},
				{Name: "fo", Source: "kv:fo", Type: "string"},
				{Name: "rua", Source: "kv:rua", Type: "string"},
				{Name: "ruf", Source: "kv:ruf", Type: "string"},
			},
		},
		{
			ID: "smtp.postfix", Version: 1,
			Description: "Postfix MTA log lines",
			Format:      "syslog_rfc3164",
			Fields: []FieldDef{
				{Name: "timestamp", Source: "grok:SYSLOGTIMESTAMP", Type: "timestamp"},
				{Name: "host", Source: "grok:HOSTNAME", Type: "string"},
				{Name: "queue_id", Source: "grok:WORD", Type: "string"},
				{Name: "message", Source: "grok:GREEDYDATA", Type: "string"},
			},
		},
		{
			ID: "smtp.imap", Version: 1,
			Description: "Dovecot IMAP log lines",
			Format:      "syslog_rfc3164",
			Fields: []FieldDef{
				{Name: "timestamp", Source: "grok:SYSLOGTIMESTAMP", Type: "timestamp"},
				{Name: "host", Source: "grok:HOSTNAME", Type: "string"},
				{Name: "user", Source: "kv:user", Type: "string"},
				{Name: "message", Source: "grok:GREEDYDATA", Type: "string"},
			},
		},
		{
			ID: "security.sshd", Version: 1,
			Description: "OpenSSH authentication and connection events",
			Format:      "syslog_rfc3164",
			Fields: []FieldDef{
				{Name: "timestamp", Source: "grok:SYSLOGTIMESTAMP", Type: "timestamp"},
				{Name: "host", Source: "grok:HOSTNAME", Type: "string"},
				{Name: "event", Source: "grok:WORD", Type: "string"},
				{Name: "user", Source: "grok:USERNAME", Type: "string"},
				{Name: "src_ip", Source: "grok:IPV4", Type: "ip"},
				{Name: "port", Source: "grok:POSINT", Type: "integer"},
				{Name: "message", Source: "grok:GREEDYDATA", Type: "string"},
			},
			Tests: []RegressionTest{
				{Input: "Oct  2 10:00:00 server sshd[123]: Accepted password for alice from 10.0.0.1 port 22 ssh2", Expected: map[string]interface{}{"user": "alice"}},
			},
		},
		{
			ID: "security.sudo", Version: 1,
			Description: "sudo usage events",
			Format:      "syslog_rfc3164",
			Fields: []FieldDef{
				{Name: "timestamp", Source: "grok:SYSLOGTIMESTAMP", Type: "timestamp"},
				{Name: "host", Source: "grok:HOSTNAME", Type: "string"},
				{Name: "user", Source: "grok:USERNAME", Type: "string"},
				{Name: "run_as", Source: "grok:USERNAME", Type: "string"},
				{Name: "command", Source: "grok:GREEDYDATA", Type: "string"},
			},
		},
		{
			ID: "kernel.audit", Version: 1,
			Description: "Linux auditd log lines",
			Format:      "kvpairs",
			Fields: []FieldDef{
				{Name: "type", Source: "kv:type", Type: "string", Required: true},
				{Name: "msg", Source: "kv:msg", Type: "string"},
				{Name: "pid", Source: "kv:pid", Type: "integer"},
				{Name: "uid", Source: "kv:uid", Type: "integer"},
				{Name: "auid", Source: "kv:auid", Type: "integer"},
				{Name: "exe", Source: "kv:exe", Type: "string"},
				{Name: "key", Source: "kv:key", Type: "string"},
			},
		},
		{
			ID: "docker.json", Version: 1,
			Description: "Docker JSON log driver output",
			Format:      "json",
			Fields: []FieldDef{
				{Name: "timestamp", Source: "json:time", Type: "timestamp"},
				{Name: "stream", Source: "json:stream", Type: "string"},
				{Name: "message", Source: "json:log", Type: "string"},
			},
		},
		{
			ID: "cef.generic", Version: 1,
			Description: "Common Event Format (CEF)",
			Format:      "cef",
			Fields: []FieldDef{
				{Name: "version", Source: "group:1", Type: "integer"},
				{Name: "device_vendor", Source: "group:2", Type: "string"},
				{Name: "device_product", Source: "group:3", Type: "string"},
				{Name: "device_version", Source: "group:4", Type: "string"},
				{Name: "signature_id", Source: "group:5", Type: "string"},
				{Name: "name", Source: "group:6", Type: "string"},
				{Name: "severity", Source: "group:7", Type: "integer"},
				{Name: "extension", Source: "group:8", Type: "string"},
			},
		},
	}
}

// LoadBuiltin registers all built-in parsers into the given registry.
func LoadBuiltin(r *Registry) {
	for _, p := range BuiltinParsers() {
		r.Register(p)
	}
}
