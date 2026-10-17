package controlclient

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nexora.local/nli-v14/internal/protocol"
	"nexora.local/nli-v14/internal/rollout"
)

type fakeExecutor struct {
	mu           sync.Mutex
	restarts     int
	restartError error
	replayPath   string
	replayIDs    []string
}

func (executor *fakeExecutor) RestartAgent(context.Context) error {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	executor.restarts++
	return executor.restartError
}
func (executor *fakeExecutor) Replay(_ context.Context, path string, ids []string) (string, error) {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	executor.replayPath = path
	executor.replayIDs = append([]string(nil), ids...)
	return `{"resolved_ids":["q-123"],"waiting_ids":[]}`, nil
}
func (*fakeExecutor) UpgradePackage(context.Context, string) error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestRunRetriesEnrollmentUntilControllerReady(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NLI_ENROLLMENT_TOKEN", "retry-enrollment-token-123456")
	client, err := New(Config{
		ControllerURL: "http://127.0.0.1:12382", AllowInsecureHTTP: true,
		NodeTokenFile: filepath.Join(root, "node.token"), InstallIDFile: filepath.Join(root, "install.id"),
		JournalFile: filepath.Join(root, "journal.json"), AgentConfigPath: filepath.Join(root, "nli.json"),
		ParserDir: filepath.Join(root, "parsers"), PackageSpoolDir: filepath.Join(root, "packages"),
		MetricsURL: "http://127.0.0.1:1/metrics", PollSeconds: 5,
	}, &fakeExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	client.pollInterval = 10 * time.Millisecond

	var attempts atomic.Int32
	cycleComplete := make(chan struct{})
	var completed sync.Once
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			return nil, errors.New("controller is starting")
		}
		status := http.StatusOK
		body := "{}"
		switch request.URL.Path {
		case "/api/v1/node/register":
			body = `{"node_token":"retry-node-token-12345678901234567890"}`
		case "/api/v1/node/heartbeat":
		case "/api/v1/node/commands/next":
			status = http.StatusNoContent
			body = ""
			completed.Do(func() { close(cycleComplete) })
		default:
			return nil, errors.New("unexpected request path: " + request.URL.Path)
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	select {
	case <-cycleComplete:
	case <-ctx.Done():
		t.Fatal("client did not retry enrollment before the test deadline")
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("long-running client returned a startup error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client did not stop after cancellation")
	}
	if attempts.Load() < 4 {
		t.Fatalf("HTTP attempts = %d, want initial failure plus enrollment cycle", attempts.Load())
	}
	if token, readErr := os.ReadFile(filepath.Join(root, "node.token")); readErr != nil || strings.TrimSpace(string(token)) == "" {
		t.Fatalf("successful retry did not persist a node token: token=%q err=%v", token, readErr)
	}
}

func TestClientConfigValidationActivationAndRestart(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "nli.json")
	oldConfig := validAgentConfig(t, root, "old-service")
	newConfig := validAgentConfig(t, root, "new-service")
	if err := os.WriteFile(configPath, oldConfig, 0o640); err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{}
	client, err := New(Config{
		ControllerURL: "http://127.0.0.1:12345", AllowInsecureHTTP: true,
		NodeTokenFile: filepath.Join(root, "node.token"), InstallIDFile: filepath.Join(root, "install.id"),
		JournalFile: filepath.Join(root, "journal.json"), AgentConfigPath: configPath,
		ParserDir: filepath.Join(root, "parsers"), PackageSpoolDir: filepath.Join(root, "packages"), PollSeconds: 5,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]json.RawMessage{"config": newConfig})
	if _, err := client.applyConfig(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	applied, _ := os.ReadFile(configPath)
	if !json.Valid(applied) || !containsBytes(applied, []byte("new-service")) {
		t.Fatalf("new configuration not applied: %s", applied)
	}
	backup, _ := os.ReadFile(configPath + ".backup")
	if !containsBytes(backup, []byte("old-service")) || executor.restarts != 1 {
		t.Fatalf("backup/restart mismatch backup=%s restarts=%d", backup, executor.restarts)
	}
}

func TestClientConfigPatchPreservesUnchangedSettings(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "nli.json")
	oldConfig := validAgentConfig(t, root, "old-service")
	var current map[string]any
	if err := json.Unmarshal(oldConfig, &current); err != nil {
		t.Fatal(err)
	}
	current["telemetry_outputs"] = []map[string]any{{"type": "discard", "routes": []string{"telemetry"}}}
	oldConfig, _ = json.Marshal(current)
	if err := os.WriteFile(configPath, oldConfig, 0o640); err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{}
	client, err := New(Config{
		ControllerURL: "http://127.0.0.1:12345", AllowInsecureHTTP: true,
		NodeTokenFile: filepath.Join(root, "node.token"), InstallIDFile: filepath.Join(root, "install.id"),
		JournalFile: filepath.Join(root, "journal.json"), AgentConfigPath: configPath,
		ParserDir: filepath.Join(root, "parsers"), PackageSpoolDir: filepath.Join(root, "packages"), PollSeconds: 5,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	patch, _ := json.Marshal(map[string]any{"config_patch": map[string]any{
		"inputs": []map[string]any{{"path": filepath.Join(root, "imap.log"), "service": "imap"}},
	}})
	if _, err := client.applyConfig(context.Background(), patch); err != nil {
		t.Fatal(err)
	}
	applied, _ := os.ReadFile(configPath)
	var result map[string]any
	if err := json.Unmarshal(applied, &result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result["telemetry_outputs"]; !ok || !containsBytes(applied, []byte("imap.log")) || containsBytes(applied, []byte("old-service")) {
		t.Fatalf("patch did not preserve telemetry or replace inputs: %s", applied)
	}
	if executor.restarts != 1 {
		t.Fatalf("agent restarted %d times", executor.restarts)
	}
}

func TestClientRestoresConfigurationWhenRestartFails(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "nli.json")
	oldConfig := validAgentConfig(t, root, "old-service")
	if err := os.WriteFile(configPath, oldConfig, 0o640); err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{restartError: errors.New("restart failed")}
	client, err := New(Config{
		ControllerURL: "http://127.0.0.1:12345", AllowInsecureHTTP: true,
		NodeTokenFile: filepath.Join(root, "node.token"), InstallIDFile: filepath.Join(root, "install.id"),
		JournalFile: filepath.Join(root, "journal.json"), AgentConfigPath: configPath,
		ParserDir: filepath.Join(root, "parsers"), PackageSpoolDir: filepath.Join(root, "packages"), PollSeconds: 5,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}
	newConfig := validAgentConfig(t, root, "new-service")
	if _, err := client.applyConfig(context.Background(), newConfig); err == nil {
		t.Fatal("restart failure was ignored")
	}
	restored, _ := os.ReadFile(configPath)
	if !containsBytes(restored, []byte("old-service")) {
		t.Fatalf("configuration was not restored: %s", restored)
	}
}

func validAgentConfig(t *testing.T, root, service string) []byte {
	t.Helper()
	value := map[string]interface{}{
		"version":         2,
		"inputs":          []map[string]interface{}{{"path": filepath.Join(root, "input.log"), "service": service}},
		"state_dir":       filepath.Join(root, "state"),
		"outputs":         []map[string]interface{}{{"type": "file", "path": filepath.Join(root, "output.jsonl"), "max_mb": 10}},
		"metrics_address": "127.0.0.1:12380", "poll_ms": 500,
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func containsBytes(value, fragment []byte) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		matched := true
		for offset := range fragment {
			if value[index+offset] != fragment[offset] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func TestControllerConfigurationRejectsPlaceholderAndTrustsPrivateCA(t *testing.T) {
	root := t.TempDir()
	base := Config{
		ControllerURL: "https://nli-controller.example.com:12382",
		NodeTokenFile: filepath.Join(root, "node.token"), InstallIDFile: filepath.Join(root, "install.id"),
		JournalFile: filepath.Join(root, "journal.json"), AgentConfigPath: filepath.Join(root, "nli.json"),
		ParserDir: filepath.Join(root, "parsers"), PackageSpoolDir: filepath.Join(root, "packages"), PollSeconds: 5,
	}
	if _, err := New(base, &fakeExecutor{}); err == nil || !strings.Contains(err.Error(), "documentation-only placeholder") {
		t.Fatalf("placeholder controller was not rejected: %v", err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer server.Close()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	caFile := filepath.Join(root, "controller-ca.crt")
	if err := os.WriteFile(caFile, certificate, 0o644); err != nil {
		t.Fatal(err)
	}
	base.ControllerURL = server.URL
	base.CAFile = caFile
	client, err := New(base, &fakeExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.http.Get(server.URL)
	if err != nil {
		t.Fatalf("private controller CA was not trusted: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("private-CA response status=%d", response.StatusCode)
	}
}

func TestReplayCommandTargetsRecordsAndReturnsPerRecordSummary(t *testing.T) {
	executor := &fakeExecutor{}
	client := &Client{cfg: Config{QuarantinePath: "/var/log/nli/quarantine.jsonl"}, executor: executor}
	command := &protocol.Command{
		Type:    rollout.TypeReplay,
		Payload: json.RawMessage(`{"record_ids":["q-123","q-456"]}`),
	}
	message, err := client.apply(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(message), &summary); err != nil || summary["resolved_ids"] == nil {
		t.Fatalf("per-record replay result missing: %s err=%v", message, err)
	}
	if executor.replayPath != client.cfg.QuarantinePath || strings.Join(executor.replayIDs, ",") != "q-123,q-456" {
		t.Fatalf("replay did not pass selected ids/path: path=%s ids=%v", executor.replayPath, executor.replayIDs)
	}
}
