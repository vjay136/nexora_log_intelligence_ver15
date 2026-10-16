package quarantine

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendPreservesExactRawAndStableSourceIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quarantine.jsonl")
	raw := "  20261010 123456789+0530 host SMTP key=value  \ncontinued"
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	first := New("node-1", "smtp", "/var/log/mail.log", 41, raw, "rev-a", 3, []string{"no_match"}, at)
	second := New("node-1", "smtp", "/var/log/mail.log", 41, raw, "rev-a", 3, []string{"no_match"}, at.Add(10*time.Minute))
	if first.ID != second.ID || first.EventHash == "" {
		t.Fatalf("stable identity/hash missing: first=%+v second=%+v", first, second)
	}
	if err := Append(path, first); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatalf("record missing: %v", scanner.Err())
	}
	var stored Record
	if err := json.Unmarshal(scanner.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Raw != raw || stored.Status != "waiting" || stored.AttemptCount != 3 || stored.ParserRevision != "rev-a" {
		t.Fatalf("stored record lost retry details or exact raw: %+v", stored)
	}
	if strings.Contains(string(scanner.Bytes()), `"telemetry"`) {
		t.Fatal("unexpected telemetry wrapper in durable quarantine record")
	}
}
