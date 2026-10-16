package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nexora.local/nli-v13/internal/output"
)

func TestSourceWorkersKeepEachInputOutputOrder(t *testing.T) {
	root := t.TempDir()
	inputA, inputB := filepath.Join(root, "a.log"), filepath.Join(root, "b.log")
	outputA, outputB := filepath.Join(root, "a.jsonl"), filepath.Join(root, "b.jsonl")
	writeInput := func(path, prefix string) {
		t.Helper()
		var lines []string
		for i := 0; i < 12; i++ {
			lines = append(lines, `{"service":"test","id":"`+prefix+`-`+string(rune('a'+i))+`"}`)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeInput(inputA, "a")
	writeInput(inputB, "b")
	cfg := Config{
		Version: 2, Inputs: []InputConfig{{Path: inputA, Service: "source-a"}, {Path: inputB, Service: "source-b"}},
		StateDir: filepath.Join(root, "state"), PollMS: 50, SourceWorkers: 2, DisableV5Parser: true,
		Outputs: []output.Config{
			{Type: "file", Path: outputA, Routes: []string{"normal"}, Families: []string{"source-a"}},
			{Type: "file", Path: outputB, Routes: []string{"normal"}, Families: []string{"source-b"}},
		},
		TelemetryOutputs: []output.Config{{Type: "discard", Routes: []string{"telemetry"}}},
	}
	if err := Run(context.Background(), cfg, true); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ path, prefix string }{{outputA, "a"}, {outputB, "b"}} {
		data, err := os.ReadFile(item.path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 12 {
			t.Fatalf("%s output has %d records, want 12: %s", item.prefix, len(lines), data)
		}
		for i, line := range lines {
			var payload map[string]any
			if err := json.Unmarshal([]byte(line), &payload); err != nil {
				t.Fatal(err)
			}
			want := item.prefix + "-" + string(rune('a'+i))
			if payload["id"] != want {
				t.Fatalf("%s output[%d] = %v, want %s", item.prefix, i, payload["id"], want)
			}
		}
	}
}
