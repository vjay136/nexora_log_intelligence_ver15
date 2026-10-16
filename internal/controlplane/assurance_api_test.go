package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssuranceScoreAndParserCandidateLifecycle(t *testing.T) {
	f := newAPIFixture(t)
	raw := `level=INFO user=alice`
	parser := map[string]any{
		"pattern_type": "regex",
		"pattern":      `level=(?P<level>\w+) user=(?P<user>\w+)`,
		"fields": []any{
			map[string]any{"name": "level", "source": "group:level", "type": "string"},
			map[string]any{"name": "user", "source": "group:user", "type": "string"},
		},
	}
	cases := []any{map[string]any{"raw": raw, "desired": map[string]any{"level": "INFO", "user": "alice"}}}
	response, data := f.request(http.MethodPost, "/api/v1/assurance/parsers", map[string]any{"name": "Key Value Sample", "parser": parser, "cases": cases})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("draft create %d: %s", response.StatusCode, data)
	}
	created := decodeMap(t, data)
	if created["state"] != "draft" {
		t.Fatalf("draft state: %s", data)
	}
	validation, _ := created["validation"].(map[string]any)
	if validation["passed"] != float64(1) || validation["failed"] != float64(0) {
		t.Fatalf("candidate validation: %s", data)
	}
	id, _ := created["id"].(string)
	response, data = f.request(http.MethodPost, "/api/v1/assurance/parsers/"+id+"/submit", map[string]any{})
	if response.StatusCode != http.StatusOK || decodeMap(t, data)["state"] != "in_review" {
		t.Fatalf("submit %d: %s", response.StatusCode, data)
	}
	response, data = f.request(http.MethodPost, "/api/v1/assurance/run", map[string]any{"raw": `{"level":"INFO","user":"alice"}`, "desired": map[string]any{"level": "INFO", "user": "alice"}})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("assurance run %d: %s", response.StatusCode, data)
	}
	result := decodeMap(t, data)
	if result["score"] == nil || result["coverage"] == nil || result["passed"] != true {
		t.Fatalf("assurance score/coverage missing: %s", data)
	}
}

func TestAssuranceNormalizesLineEndingsAndReturnsParserCreationHelper(t *testing.T) {
	f := newAPIFixture(t)
	raw := "method=mail.message.list user=alice@example.test   \r\nfromhost=192.0.2.10\t\r\n\r\n"
	response, data := f.request(http.MethodPost, "/api/v1/assurance/run", map[string]any{
		"raw": raw, "service": "webtop-webmail", "desired": map[string]any{},
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("assurance run %d: %s", response.StatusCode, data)
	}
	result := decodeMap(t, data)
	if got, want := result["raw"], "method=mail.message.list user=alice@example.test\nfromhost=192.0.2.10"; got != want {
		t.Fatalf("normalized raw = %#v, want %#v", got, want)
	}
	helper, ok := result["parser_creation_helper"].(map[string]any)
	if !ok {
		t.Fatalf("parser_creation_helper missing: %s", data)
	}
	if helper["normalized_sample"] != result["raw"] || helper["sample_lines"] != float64(2) {
		t.Fatalf("normalized helper sample is inconsistent: %#v", helper)
	}
	if helper["review_state"] != "draft" || helper["requires_human_approval"] != true {
		t.Fatalf("helper bypassed review gate: %#v", helper)
	}
	fields, _ := helper["fields"].([]any)
	if len(fields) == 0 {
		t.Fatalf("helper did not expose detected fields: %#v", helper)
	}
}

func TestAIProviderProtocolAdapters(t *testing.T) {
	tests := []struct {
		name           string
		provider       string
		path           string
		expectedHeader string
		response       map[string]any
	}{
		{name: "OpenAI compatible", provider: "openai", path: "/chat/completions", expectedHeader: "Authorization", response: map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"status":"ok"}`}}}}},
		{name: "Anthropic", provider: "anthropic", path: "/messages", expectedHeader: "X-API-Key", response: map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"status":"ok"}`}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					t.Errorf("path = %q, want %q", r.URL.Path, test.path)
				}
				if r.Header.Get(test.expectedHeader) == "" {
					t.Errorf("missing %s header", test.expectedHeader)
				}
				if test.provider == "anthropic" && r.Header.Get("Anthropic-Version") != "2023-06-01" {
					t.Errorf("Anthropic-Version = %q", r.Header.Get("Anthropic-Version"))
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(test.response)
			}))
			defer server.Close()
			cfg := aiProviderConfig{Provider: test.provider, BaseURL: server.URL, Model: "test-model", APIKey: "test-api-key"}
			if err := validateProvider(cfg); err != nil {
				t.Fatalf("validateProvider: %v", err)
			}
			content, err := (&Server{}).callAI(context.Background(), cfg, "return JSON")
			if err != nil {
				t.Fatalf("callAI: %v", err)
			}
			if content != `{"status":"ok"}` {
				t.Fatalf("content = %q", content)
			}
		})
	}
}

func TestAIProviderDefaultsAndAllowlist(t *testing.T) {
	openAI := normalizeProvider(aiProviderConfig{Provider: "openai", Model: "gpt-test", APIKey: "test-api-key"})
	if openAI.BaseURL != "https://api.openai.com/v1" || validateProvider(openAI) != nil {
		t.Fatalf("OpenAI default was not valid: %#v", openAI)
	}
	anthropic := normalizeProvider(aiProviderConfig{Provider: "anthropic", Model: "claude-test", APIKey: "test-api-key"})
	if anthropic.BaseURL != "https://api.anthropic.com/v1" || validateProvider(anthropic) != nil {
		t.Fatalf("Anthropic default was not valid: %#v", anthropic)
	}
	if err := validateProvider(aiProviderConfig{Provider: "arbitrary", BaseURL: "https://example.net", Model: "x", APIKey: "test-api-key"}); err == nil {
		t.Fatal("unsupported provider was accepted")
	}
	if err := validateProvider(normalizeProvider(aiProviderConfig{Provider: "custom", Model: "x", APIKey: "test-api-key"})); err == nil {
		t.Fatal("custom provider without endpoint was accepted")
	}
	if err := validateProvider(aiProviderConfig{Provider: "custom", BaseURL: "http://provider.example.com/v1", Model: "x", APIKey: "test-api-key"}); err == nil {
		t.Fatal("non-loopback plaintext provider endpoint was accepted")
	}
}

func TestAIFailureProposalIsDiagnosticReviewOnlyAndDoesNotRetainRaw(t *testing.T) {
	f := newAPIFixture(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"name":"SMTP status extractor","description":"Add a bounded status capture and validate it against the failed sample.","parser":{"pattern_type":"regex","pattern":"status=(?P<status>\\w+)","fields":[{"name":"status","source":"group:status","type":"string"}]}}`}}}})
	}))
	defer provider.Close()
	if err := f.server.storeAIProvider(context.Background(), aiProviderConfig{Provider: "openai", BaseURL: provider.URL, Model: "test-model", APIKey: "test-api-key"}, "admin"); err != nil {
		t.Fatal(err)
	}
	raw := "user=private@example.com status=FAILED"
	response, data := f.request(http.MethodPost, "/api/v1/assurance/ai-proposal", map[string]any{"raw": raw, "service": "smtp", "desired": map[string]any{"status": "DELIVERED"}})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("AI proposal %d: %s", response.StatusCode, data)
	}
	created := decodeMap(t, data)
	if created["review_state"] != "draft" || created["requires_human_approval"] != true || created["published"] != false {
		t.Fatalf("AI proposal bypassed review: %s", data)
	}
	response, stored := f.request(http.MethodGet, "/api/v1/assurance/ai-proposals", nil)
	if response.StatusCode != http.StatusOK || bytes.Contains(stored, []byte(raw)) || bytes.Contains(stored, []byte("private@example.com")) {
		t.Fatalf("AI proposal store retained raw customer data: %d %s", response.StatusCode, stored)
	}
	if !bytes.Contains(stored, []byte(`"diagnosis"`)) || !bytes.Contains(stored, []byte(`"suggested_method"`)) {
		t.Fatalf("AI proposal diagnosis is incomplete: %s", stored)
	}
	id, _ := created["id"].(string)
	response, data = f.request(http.MethodPost, "/api/v1/assurance/ai-proposals/"+id+"/approve", map[string]any{})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("proposal creator approved their own AI proposal: %d %s", response.StatusCode, data)
	}
}
