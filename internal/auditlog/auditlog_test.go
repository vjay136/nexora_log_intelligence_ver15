package auditlog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestChainLogPersistsAndVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	key := bytes.Repeat([]byte{0x42}, 32)
	log, err := NewChainLog(path, key)
	if err != nil {
		t.Fatal(err)
	}
	first, err := log.Append(Event{Action: "parser.create", Actor: "alice", Role: "parser_author", Resource: "parser-packs/mail", Outcome: "success"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := log.Append(Event{Action: "parser.approve", Actor: "bob", Role: "reviewer", Resource: "parser-packs/mail", Outcome: "success"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Sequence != 2 || second.Previous != first.Hash {
		t.Fatalf("invalid chain: %+v", second)
	}
	if err := log.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewChainLog(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(reopened.List(10)) != 2 {
		t.Fatal("audit records not restored")
	}
}

func TestChainLogRejectsTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	key := bytes.Repeat([]byte{0x23}, 32)
	log, err := NewChainLog(path, key)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = log.Append(Event{Action: "login", Actor: "admin", Resource: "session", Outcome: "success"})
	_ = log.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"actor":"admin"`), []byte(`"actor":"other"`), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewChainLog(path, key); err == nil {
		t.Fatal("tampered chain was accepted")
	}
}
