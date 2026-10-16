package semanticextract

import (
	"strings"
	"testing"
)

func TestExtractNetworkAndEmailEntitiesWithSafeURLCredentials(t *testing.T) {
	input := "peer=192.0.2.44 ip6=[2001:0db8:0:0:0:0:0:1] host=mail.example.org url=https://alice:secret@example.org/path?token=private&view=full email=ops@example.org"
	got := Extract(input, Config{IPv4: true, IPv6: true, Domain: true, URL: true, Email: true})
	if len(got.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", got.Errors)
	}
	if values := got.Entities["ipv4"]; len(values) != 1 || values[0].Value != "192.0.2.44" {
		t.Fatalf("IPv4 extraction: %+v", values)
	}
	if values := got.Entities["ipv6"]; len(values) != 1 || values[0].Value != "2001:db8::1" {
		t.Fatalf("IPv6 extraction: %+v", values)
	}
	if values := got.Entities["email"]; len(values) != 1 || values[0].Value != "ops@example.org" {
		t.Fatalf("email extraction: %+v", values)
	}
	if values := got.Entities["url"]; len(values) != 1 || strings.Contains(values[0].Value, "alice") || strings.Contains(values[0].Value, "secret") || strings.Contains(values[0].Value, "private") {
		t.Fatalf("URL credentials escaped redaction: %+v", values)
	}
	for _, entity := range got.Entities["ipv4"] {
		if entity.Start < 0 || entity.End > len(input) || input[entity.Start:entity.End] != entity.Value {
			t.Fatalf("IPv4 source evidence is invalid: %+v", entity)
		}
	}
}

func TestDecodeMIMEHeaderQAndBAndUnfoldSubject(t *testing.T) {
	got := Extract("Subject: =?UTF-8?Q?H=C3=A9llo?=\r\n\t=?UTF-8?B?Vjg=?=\r\nFrom: sender@example.org\r\n\r\nbody", Config{DecodeMIMEHeaders: true})
	if got.Subject != "HélloV8" || got.SubjectStart <= 0 || got.SubjectEnd <= got.SubjectStart {
		t.Fatalf("unfolded subject decode/evidence: %+v", got)
	}
	latin, err := DecodeMIMEHeader("=?windows-1252?Q?caf=E9?=", 128)
	if err != nil || latin != "café" {
		t.Fatalf("windows-1252 decode = %q, %v", latin, err)
	}
}

func TestMalformedAndOversizedDataFailBoundedly(t *testing.T) {
	bad := Extract("Subject: =?UTF-8?Q?bad=ZZ?=\r\n\r\n", Config{DecodeMIMEHeaders: true, MaxHeaderBytes: 32})
	if len(bad.Errors) == 0 {
		t.Fatal("malformed encoded subject did not report a diagnostic")
	}
	large := Extract(strings.Repeat("x", 16)+" 192.0.2.4", Config{IPv4: true, MaxInputBytes: 8})
	if len(large.Entities["ipv4"]) != 0 || len(large.Errors) == 0 {
		t.Fatalf("oversized input was not bounded: %+v", large)
	}
	ordinaryLog := Extract("2026-10-09T12:00:00Z INFO request from 192.0.2.5", Config{DecodeMIMEHeaders: true, IPv4: true})
	if len(ordinaryLog.Errors) != 0 || len(ordinaryLog.Entities["ipv4"]) != 1 {
		t.Fatalf("ordinary log line should not be treated as malformed MIME: %+v", ordinaryLog)
	}
}
