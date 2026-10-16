package output

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileWriterChangesHourlyTemplateAtUTCBoundary(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 10, 14, 8, 59, 59, 0, time.UTC)
	w, err := NewFileWriter(Config{Type: "file", PathTemplate: filepath.Join(dir, "imap_normalised_{hour_utc}.jsonl"), Routes: []string{"normal"}})
	if err != nil {
		t.Fatal(err)
	}
	w.now = func() time.Time { return clock }
	// Re-resolve after installing the deterministic clock used by this test.
	_ = w.file.Close()
	w.file = nil
	w.currentPath = ""
	if err := w.Write([]byte(`{"line":1}`), "normal"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if err := w.Write([]byte(`{"line":2}`), "normal"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"imap_normalised_20261014_0800.jsonl", "imap_normalised_20261014_0900.jsonl"} {
		if data, readErr := os.ReadFile(filepath.Join(dir, name)); readErr != nil || len(data) == 0 {
			t.Fatalf("hourly output %s missing: %q, %v", name, data, readErr)
		}
	}
}

func TestResolveFilePathRejectsAmbiguousTemplate(t *testing.T) {
	for _, cfg := range []Config{
		{Path: "/tmp/a.jsonl", PathTemplate: "/tmp/a_{hour_utc}.jsonl"},
		{PathTemplate: "/tmp/a.jsonl"},
		{PathTemplate: "relative_{hour_utc}.jsonl"},
	} {
		if _, err := resolveFilePath(cfg, time.Now()); err == nil {
			t.Fatalf("accepted invalid file config: %#v", cfg)
		}
	}
}
