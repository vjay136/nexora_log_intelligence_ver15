package controlplane

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"nexora.local/nli-v14/internal/middleware"
	"nexora.local/nli-v14/internal/registry"
	metastore "nexora.local/nli-v14/internal/store"
)

type aiProviderConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
}
type sealedAIProvider struct {
	Nonce        string     `json:"nonce"`
	Ciphertext   string     `json:"ciphertext"`
	ConfiguredAt time.Time  `json:"configured_at"`
	UpdatedBy    string     `json:"updated_by"`
	LastTestAt   *time.Time `json:"last_test_at,omitempty"`
}

func (s *Server) adminOnly(w http.ResponseWriter, r *http.Request) *middleware.SecurityContext {
	p := s.authorize(w, r, middleware.PermissionManageUsers)
	if p == nil {
		return nil
	}
	if p.Role != "admin" {
		jsonError(w, "administrator role required", http.StatusForbidden)
		return nil
	}
	return p
}
func (s *Server) storeAIProvider(ctx context.Context, cfg aiProviderConfig, actor string) error {
	if s.assurance.providerAEAD == nil {
		return errors.New("Assurance encryption key is not configured")
	}
	plain, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.assurance.providerAEAD.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	sealed := sealedAIProvider{Nonce: base64.StdEncoding.EncodeToString(nonce), Ciphertext: base64.StdEncoding.EncodeToString(s.assurance.providerAEAD.Seal(nil, nonce, plain, []byte("ai-provider"))), ConfiguredAt: time.Now().UTC(), UpdatedBy: actor}
	payload, _ := json.Marshal(sealed)
	_, err = s.metadata.Put(ctx, metastore.Document{Collection: "ai-provider", ID: "default", State: "configured", Payload: payload, CreatedBy: actor})
	return err
}
func (s *Server) loadAIProvider(ctx context.Context) (aiProviderConfig, sealedAIProvider, error) {
	if s.assurance.providerAEAD == nil {
		return aiProviderConfig{}, sealedAIProvider{}, errors.New("Assurance encryption key is not configured")
	}
	doc, err := s.metadata.Get(ctx, "ai-provider", "default", 0)
	if err != nil {
		return aiProviderConfig{}, sealedAIProvider{}, err
	}
	var sealed sealedAIProvider
	if err = json.Unmarshal(doc.Payload, &sealed); err != nil {
		return aiProviderConfig{}, sealed, err
	}
	nonce, err := base64.StdEncoding.DecodeString(sealed.Nonce)
	if err != nil || len(nonce) != s.assurance.providerAEAD.NonceSize() {
		return aiProviderConfig{}, sealed, errors.New("invalid provider nonce")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(sealed.Ciphertext)
	if err != nil {
		return aiProviderConfig{}, sealed, err
	}
	plain, err := s.assurance.providerAEAD.Open(nil, nonce, ciphertext, []byte("ai-provider"))
	if err != nil {
		return aiProviderConfig{}, sealed, err
	}
	var cfg aiProviderConfig
	err = json.Unmarshal(plain, &cfg)
	return cfg, sealed, err
}
func validateProvider(cfg aiProviderConfig) error {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "openai", "openai-compatible", "anthropic", "custom":
	default:
		return errors.New("provider must be openai, anthropic, or custom OpenAI-compatible")
	}
	u, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("base_url must be an HTTP(S) URL without credentials, query or fragment")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return errors.New("base_url must use HTTPS unless the provider is on loopback")
	}
	if len(cfg.BaseURL) > 2048 || len(cfg.Model) == 0 || len(cfg.Model) > 256 {
		return errors.New("model and bounded base_url are required")
	}
	if len(cfg.APIKey) < 8 || len(cfg.APIKey) > 4096 {
		return errors.New("api_key length is invalid")
	}
	return nil
}
func normalizeProvider(cfg aiProviderConfig) aiProviderConfig {
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	if cfg.BaseURL == "" {
		switch cfg.Provider {
		case "openai":
			cfg.BaseURL = "https://api.openai.com/v1"
		case "anthropic":
			cfg.BaseURL = "https://api.anthropic.com/v1"
		}
	}
	return cfg
}
func providerEndpoint(cfg aiProviderConfig) (string, error) {
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return "", err
	}
	if cfg.Provider == "anthropic" {
		if !strings.HasSuffix(u.Path, "/messages") {
			u.Path = strings.TrimRight(u.Path, "/") + "/messages"
		}
	} else if !strings.HasSuffix(u.Path, "/chat/completions") {
		u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	}
	return u.String(), nil
}
func (s *Server) publishAIProviderStatus(status, provider, model string) {
	payload, _ := json.Marshal(map[string]any{"type": "ai_provider_changed", "status": status, "provider": provider, "model": model, "updated_at": time.Now().UTC()})
	s.hub.Publish(payload)
}
func (s *Server) handleAIProvider(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfg, sealed, err := s.loadAIProvider(r.Context())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || s.assurance.providerAEAD == nil {
				jsonOK(w, map[string]any{"provider": map[string]any{"provider": "", "model": "", "endpoint": "", "configured": false, "key_last4": "", "enabled": false, "status": "not_configured", "last_test_at": nil}})
				return
			}
			jsonError(w, "provider store unavailable", 503)
			return
		}
		last4 := cfg.APIKey
		if len(last4) > 4 {
			last4 = last4[len(last4)-4:]
		} else {
			last4 = ""
		}
		jsonOK(w, map[string]any{"provider": map[string]any{"configured": true, "enabled": true, "status": "configured", "provider": cfg.Provider, "model": cfg.Model, "endpoint": cfg.BaseURL, "key_last4": last4, "last_test_at": sealed.LastTestAt}})
	case http.MethodPut:
		var request struct {
			Provider string `json:"provider"`
			BaseURL  string `json:"base_url"`
			Endpoint string `json:"endpoint"`
			Model    string `json:"model"`
			APIKey   string `json:"api_key"`
			Enabled  *bool  `json:"enabled"`
		}
		if decodeJSON(r, &request, 16<<10) != nil {
			jsonError(w, "invalid provider configuration", 400)
			return
		}
		if request.Enabled != nil && !*request.Enabled {
			_ = s.metadata.Delete(r.Context(), "ai-provider", "default", false, p.Username)
			s.auditAction(p, "ai_provider.disable", "ai-provider/default", "success", nil)
			s.publishAIProviderStatus("disabled", "", "")
			jsonOK(w, map[string]any{"configured": false, "enabled": false, "encrypted": false})
			return
		}
		cfg := aiProviderConfig{Provider: request.Provider, BaseURL: request.BaseURL, Model: request.Model, APIKey: request.APIKey}
		if cfg.BaseURL == "" {
			cfg.BaseURL = request.Endpoint
		}
		cfg = normalizeProvider(cfg)
		if cfg.APIKey == "" {
			if previous, _, err := s.loadAIProvider(r.Context()); err == nil {
				cfg.APIKey = previous.APIKey
			}
		}
		if err := validateProvider(cfg); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if err := s.storeAIProvider(r.Context(), cfg, p.Username); err != nil {
			jsonError(w, "provider configuration could not be encrypted or stored", 503)
			return
		}
		s.auditAction(p, "ai_provider.configure", "ai-provider/default", "success", map[string]string{"provider": cfg.Provider, "model": cfg.Model})
		s.publishAIProviderStatus("configured", cfg.Provider, cfg.Model)
		last4 := cfg.APIKey[len(cfg.APIKey)-4:]
		jsonOK(w, map[string]any{"provider": map[string]any{"configured": true, "enabled": true, "status": "configured", "provider": cfg.Provider, "model": cfg.Model, "endpoint": cfg.BaseURL, "key_last4": last4, "last_test_at": nil}})
	default:
		jsonError(w, "GET or PUT required", 405)
	}
}
func (s *Server) callAI(ctx context.Context, cfg aiProviderConfig, prompt string) (string, error) {
	cfg = normalizeProvider(cfg)
	endpoint, err := providerEndpoint(cfg)
	if err != nil {
		return "", err
	}
	systemPrompt := "Return only a JSON object with keys name and parser. parser must be declarative extraction metadata only. Never include executable code."
	var body []byte
	if cfg.Provider == "anthropic" {
		body, _ = json.Marshal(map[string]any{"model": cfg.Model, "max_tokens": 2048, "temperature": 0, "system": systemPrompt, "messages": []map[string]string{{"role": "user", "content": prompt}}})
	} else {
		body, _ = json.Marshal(map[string]any{"model": cfg.Model, "temperature": 0, "messages": []map[string]string{{"role": "system", "content": systemPrompt}, {"role": "user", "content": prompt}}})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Provider == "anthropic" {
		req.Header.Set("X-API-Key", cfg.APIKey)
		req.Header.Set("Anthropic-Version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if len(data) >= 1<<20 {
		return "", errors.New("provider response exceeded 1 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	if cfg.Provider == "anthropic" {
		var envelope struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			return "", errors.New("Anthropic provider returned an invalid response")
		}
		for _, block := range envelope.Content {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				return block.Text, nil
			}
		}
		return "", errors.New("Anthropic provider returned no text content")
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &envelope) != nil || len(envelope.Choices) == 0 || strings.TrimSpace(envelope.Choices[0].Message.Content) == "" {
		return "", errors.New("OpenAI-compatible provider returned an invalid response")
	}
	return envelope.Choices[0].Message.Content, nil
}
func (s *Server) handleAIProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "POST required", 405)
		return
	}
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	var input struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Endpoint string `json:"endpoint"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
	}
	if r.ContentLength > 0 && decodeJSON(r, &input, 16<<10) != nil {
		jsonError(w, "invalid provider test request", 400)
		return
	}
	cfg, _, err := s.loadAIProvider(r.Context())
	if input.APIKey != "" {
		cfg = aiProviderConfig{Provider: input.Provider, Model: input.Model, BaseURL: input.BaseURL, APIKey: input.APIKey}
		if cfg.BaseURL == "" {
			cfg.BaseURL = input.Endpoint
		}
	} else if input.Endpoint != "" || input.BaseURL != "" {
		if input.Provider != "" {
			cfg.Provider = input.Provider
		}
		if input.Model != "" {
			cfg.Model = input.Model
		}
		if input.BaseURL != "" {
			cfg.BaseURL = input.BaseURL
		} else {
			cfg.BaseURL = input.Endpoint
		}
	}
	if err != nil && cfg.APIKey == "" {
		jsonOK(w, map[string]any{"connected": false, "status": "failed", "message": "AI provider is not configured"})
		return
	}
	cfg = normalizeProvider(cfg)
	if err = validateProvider(cfg); err != nil {
		jsonOK(w, map[string]any{"connected": false, "status": "failed", "message": "Provider URL, model or key is invalid"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 17*time.Second)
	defer cancel()
	_, err = s.callAI(ctx, cfg, "Respond with JSON: {\"status\":\"ok\"}")
	if err != nil {
		s.auditAction(p, "ai_provider.test", "ai-provider/default", "failure", nil)
		s.publishAIProviderStatus("connection_failed", cfg.Provider, cfg.Model)
		jsonOK(w, map[string]any{"connected": false, "status": "failed", "message": "Provider connection failed"})
		return
	}
	if stored, _, loadErr := s.loadAIProvider(r.Context()); loadErr == nil && stored.APIKey == cfg.APIKey {
		_ = s.markProviderTested(r.Context(), p.Username)
	}
	s.auditAction(p, "ai_provider.test", "ai-provider/default", "success", nil)
	s.publishAIProviderStatus("connected", cfg.Provider, cfg.Model)
	jsonOK(w, map[string]any{"connected": true, "status": "success", "message": "Provider connection succeeded", "model": cfg.Model})
}

func (s *Server) markProviderTested(ctx context.Context, actor string) error {
	doc, err := s.metadata.Get(ctx, "ai-provider", "default", 0)
	if err != nil {
		return err
	}
	var sealed sealedAIProvider
	if json.Unmarshal(doc.Payload, &sealed) != nil {
		return errors.New("invalid provider record")
	}
	now := time.Now().UTC()
	sealed.LastTestAt = &now
	payload, _ := json.Marshal(sealed)
	_, err = s.metadata.Put(ctx, metastore.Document{Collection: "ai-provider", ID: "default", State: "configured", Payload: payload, CreatedBy: actor})
	return err
}
func (s *Server) handleAssuranceAIProposal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "POST required", 405)
		return
	}
	var input assuranceRequest
	if decodeJSON(r, &input, 64<<10) != nil || strings.TrimSpace(input.Raw) == "" {
		jsonError(w, "raw input is required", 400)
		return
	}
	result := s.runAssurance(input)
	if result["passed"] == true {
		jsonError(w, "AI proposal is only available when deterministic assurance fails", 409)
		return
	}
	cfg, _, err := s.loadAIProvider(r.Context())
	if err != nil {
		jsonError(w, "AI provider is not configured or encryption is unavailable", 503)
		return
	}
	redacted := s.assuranceDeps.Masker.MaskForTemplate(input.Raw)
	prompt := fmt.Sprintf("Propose a declarative parser for this log. Return JSON only. Known parsed fields: %v. Expected fields: %v. Validation errors: %v. Log sample: %s", result["actual"], input.Desired, result["errors"], redacted)
	ctx, cancel := context.WithTimeout(r.Context(), 17*time.Second)
	defer cancel()
	aiStarted := time.Now()
	aiMetricsSuccess := false
	defer func() { s.aiMetrics.record(aiMetricsSuccess, time.Since(aiStarted)) }()
	raw, err := s.callAI(ctx, cfg, prompt)
	if err != nil {
		jsonError(w, "AI parser proposal failed; no parser was changed", 502)
		return
	}
	var proposal map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &proposal) != nil {
		jsonError(w, "AI returned an invalid parser proposal", 502)
		return
	}
	parser, _ := proposal["parser"].(map[string]any)
	if parser == nil {
		jsonError(w, "AI returned no declarative parser", 502)
		return
	}
	if err := unsafeDeclarative(parser); err != nil {
		jsonError(w, "AI proposal failed declarative safety validation", 422)
		return
	}
	proposal["review_state"] = "draft"
	proposal["requires_human_approval"] = true
	proposal["source_hash"] = result["raw_sha256"]
	diagnosis := map[string]any{
		"summary":  "The deterministic parser did not satisfy the requested field and validation contract.",
		"score":    result["score"],
		"coverage": result["coverage"],
		"errors":   result["errors"],
		"parser":   result["parser"],
	}
	suggestedMethod := strings.TrimSpace(fmt.Sprint(proposal["description"]))
	if suggestedMethod == "" || suggestedMethod == "<nil>" {
		suggestedMethod = strings.TrimSpace(fmt.Sprint(proposal["name"]))
	}
	if suggestedMethod == "" || suggestedMethod == "<nil>" {
		suggestedMethod = "Create a bounded declarative parser, validate it against the failing case and regression corpus, then require independent approval before publication."
	}
	// Test inputs and examples often contain customer data. Persist only the
	// declarative parser body plus the input hash; never retain prompt samples.
	storedParser := make(map[string]any, len(parser))
	for key, value := range parser {
		if key == "regression_cases" || key == "tests" || key == "samples" || key == "examples" {
			continue
		}
		storedParser[key] = value
	}
	proposalID := safeID("proposal-" + fmt.Sprint(result["raw_sha256"]))
	if proposalID == "" {
		jsonError(w, "AI proposal identifier could not be created", 500)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"name":                    proposal["name"],
		"proposal":                storedParser,
		"diagnosis":               diagnosis,
		"suggested_method":        suggestedMethod,
		"source_hash":             result["raw_sha256"],
		"review_state":            "draft",
		"requires_human_approval": true,
		"published":               false,
	})
	principal := middleware.GetSecurityContext(r)
	createdBy := "ai-proposal"
	if principal != nil {
		createdBy = principal.Username
	}
	saved, saveErr := s.metadata.Put(r.Context(), metastore.Document{Collection: "ai-proposals", ID: proposalID, State: "draft", Payload: payload, CreatedBy: createdBy})
	if saveErr != nil {
		jsonError(w, "AI proposal could not be saved for review", 503)
		return
	}
	s.auditAction(principal, "ai-proposal.create", "ai-proposals/"+saved.ID, "success", map[string]string{"state": "draft", "source_hash": fmt.Sprint(result["raw_sha256"])})
	event, _ := json.Marshal(map[string]any{"type": "ai_proposal_changed", "id": saved.ID, "status": "draft", "source_hash": result["raw_sha256"], "updated_at": time.Now().UTC()})
	s.hub.Publish(event)
	proposal["id"] = saved.ID
	proposal["diagnosis"] = diagnosis
	proposal["suggested_method"] = suggestedMethod
	aiMetricsSuccess = true
	jsonOK(w, map[string]any{"id": saved.ID, "proposal": proposal, "review_state": "draft", "requires_human_approval": true, "published": false, "stored": true})
}

type assuranceParserInput struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Version string             `json:"version"`
	Parser  map[string]any     `json:"parser"`
	Cases   []assuranceRequest `json:"cases"`
}

func validateParserCandidate(definition map[string]any, cases []assuranceRequest) map[string]any {
	encoded, _ := json.Marshal(definition)
	var parser registry.ParserDef
	if err := json.Unmarshal(encoded, &parser); err != nil {
		return map[string]any{"total": len(cases), "passed": 0, "failed": len(cases), "score": 0, "errors": []string{"parser definition is invalid"}}
	}
	if err := registry.Validate(&parser); err != nil {
		return map[string]any{"total": len(cases), "passed": 0, "failed": len(cases), "score": 0, "errors": []string{err.Error()}}
	}
	if len(cases) == 0 && len(parser.Tests) > 0 {
		for _, test := range parser.Tests {
			cases = append(cases, assuranceRequest{Raw: test.Input, Desired: test.Expected})
		}
	}
	passed := 0
	outcomes := make([]map[string]any, 0, len(cases))
	for _, item := range cases {
		actual, errors := registry.Apply(&parser, item.Raw)
		diff, _ := structuralDiff(actual, item.Desired)
		ok := len(errors) == 0 && len(diff) == 0 && len(item.Desired) > 0
		if ok {
			passed++
		}
		outcomes = append(outcomes, map[string]any{"passed": ok, "actual": actual, "diff": diff, "errors": errors, "raw_sha256": fmt.Sprintf("%x", hashBytes([]byte(item.Raw)))})
	}
	total := len(cases)
	score := 0
	if total > 0 {
		score = passed * 100 / total
	}
	return map[string]any{"total": total, "passed": passed, "failed": total - passed, "score": score, "results": outcomes}
}
func (s *Server) runParserCases(cases []assuranceRequest) map[string]any {
	passed := 0
	total := len(cases)
	results := make([]map[string]any, 0, total)
	for _, item := range cases {
		if strings.TrimSpace(item.Raw) == "" {
			continue
		}
		result := s.runAssurance(item)
		if result["passed"] == true {
			passed++
		}
		delete(result, "raw")
		results = append(results, result)
	}
	return map[string]any{"total": total, "passed": passed, "failed": total - passed, "score": func() int {
		if total == 0 {
			return 0
		}
		return passed * 100 / total
	}(), "results": results}
}
func (s *Server) handleAssuranceParsers(w http.ResponseWriter, r *http.Request) {
	perm := middleware.PermissionReadParsers
	if r.Method != http.MethodGet {
		perm = middleware.PermissionManageParsers
	}
	p := s.authorize(w, r, perm)
	if p == nil {
		return
	}
	if r.Method == http.MethodGet {
		items, err := listDocuments(r, s.metadata, "parser-packs")
		if err != nil {
			jsonError(w, "parser catalog unavailable", 503)
			return
		}
		jsonOK(w, map[string]any{"parsers": items, "count": len(items)})
		return
	}
	if r.Method != http.MethodPost {
		jsonError(w, "GET or POST required", 405)
		return
	}
	var input assuranceParserInput
	if decodeJSON(r, &input, 2<<20) != nil || strings.TrimSpace(input.Name) == "" || len(input.Cases) == 0 || len(input.Cases) > 100 {
		jsonError(w, "name and 1..100 validation cases are required", 400)
		return
	}
	if input.Version == "" {
		input.Version = "0.1.0"
	}
	if !semverPattern.MatchString(input.Version) || input.Parser == nil {
		jsonError(w, "semantic version and declarative parser are required", 400)
		return
	}
	if err := unsafeDeclarative(input.Parser); err != nil {
		jsonError(w, "unsafe declarative parser", 400)
		return
	}
	id := safeID(input.ID)
	if id == "" {
		id = safeID(input.Name)
	}
	if id == "" {
		jsonError(w, "parser id is invalid", 400)
		return
	}
	input.Parser["id"] = id
	if _, ok := input.Parser["version"]; !ok {
		input.Parser["version"] = 1
	}
	validation := validateParserCandidate(input.Parser, input.Cases)
	payload, _ := json.Marshal(map[string]any{"id": id, "name": input.Name, "version": input.Version, "parser": input.Parser, "cases": input.Cases, "validation": validation, "validation_passed": validation["failed"] == 0 && validation["total"] == len(input.Cases), "review_state": "draft", "activated": false})
	saved, err := s.metadata.Put(r.Context(), metastore.Document{Collection: "parser-packs", ID: id, State: "draft", Payload: payload, CreatedBy: p.Username})
	if err != nil {
		jsonError(w, "parser draft persistence failed", 500)
		return
	}
	s.auditAction(p, "assurance.parser_draft", "parser-packs/"+id, "success", nil)
	jsonCreated(w, map[string]any{"id": id, "revision": saved.Revision, "state": "draft", "validation": validation})
}
func (s *Server) handleAssuranceParser(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/assurance/parsers/"), "/")
	parts := strings.Split(path, "/")
	id := safeID(parts[0])
	if id == "" {
		jsonError(w, "parser id required", 400)
		return
	}
	if len(parts) < 2 {
		permission := middleware.PermissionManageParsers
		if r.Method == http.MethodGet {
			permission = middleware.PermissionReadParsers
		}
		p := s.authorize(w, r, permission)
		if p == nil {
			return
		}
		doc, err := s.metadata.Get(r.Context(), "parser-packs", id, 0)
		if err != nil {
			jsonError(w, "parser draft not found", 404)
			return
		}
		switch r.Method {
		case http.MethodGet:
			jsonOK(w, documentView(doc))
		case http.MethodPut:
			if doc.State != "draft" && doc.State != "rejected" {
				jsonError(w, "only draft or rejected parsers may be edited", 409)
				return
			}
			var input assuranceParserInput
			if decodeJSON(r, &input, 2<<20) != nil || strings.TrimSpace(input.Name) == "" || len(input.Cases) == 0 || len(input.Cases) > 100 {
				jsonError(w, "name and 1..100 validation cases are required", 400)
				return
			}
			if input.Version == "" {
				input.Version = "0.1.0"
			}
			if !semverPattern.MatchString(input.Version) || input.Parser == nil || unsafeDeclarative(input.Parser) != nil {
				jsonError(w, "safe declarative parser and semantic version are required", 400)
				return
			}
			input.Parser["id"] = id
			if _, ok := input.Parser["version"]; !ok {
				input.Parser["version"] = 1
			}
			validation := validateParserCandidate(input.Parser, input.Cases)
			payload, _ := json.Marshal(map[string]any{"id": id, "name": input.Name, "version": input.Version, "parser": input.Parser, "cases": input.Cases, "validation": validation, "validation_passed": validation["failed"] == 0 && validation["total"] == len(input.Cases), "review_state": "draft", "activated": false})
			saved, err := s.metadata.Put(r.Context(), metastore.Document{Collection: "parser-packs", ID: id, State: "draft", Payload: payload, CreatedBy: doc.CreatedBy})
			if err != nil {
				jsonError(w, "parser draft update failed", 500)
				return
			}
			s.auditAction(p, "assurance.parser_update", "parser-packs/"+id, "success", nil)
			jsonOK(w, map[string]any{"id": id, "revision": saved.Revision, "state": "draft", "validation": validation})
		case http.MethodDelete:
			if doc.State != "draft" && doc.State != "rejected" {
				jsonError(w, "only draft or rejected parsers may be deleted", 409)
				return
			}
			if err := s.metadata.Delete(r.Context(), "parser-packs", id, false, p.Username); err != nil {
				jsonError(w, "parser draft deletion failed", 500)
				return
			}
			s.auditAction(p, "assurance.parser_delete", "parser-packs/"+id, "success", nil)
			jsonOK(w, map[string]string{"status": "deleted"})
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			jsonError(w, "GET, PUT or DELETE required", 405)
		}
		return
	}
	action := parts[1]
	p := s.authorize(w, r, middleware.PermissionManageParsers)
	if action == "approve" {
		p = s.authorize(w, r, middleware.PermissionReviewParsers)
	}
	if action == "publish" {
		p = s.authorize(w, r, middleware.PermissionTriggerRollout)
	}
	if p == nil {
		return
	}
	if r.Method != http.MethodPost {
		jsonError(w, "POST required", 405)
		return
	}
	doc, err := s.metadata.Get(r.Context(), "parser-packs", id, 0)
	if err != nil {
		jsonError(w, "parser draft not found", 404)
		return
	}
	var body map[string]any
	_ = json.Unmarshal(doc.Payload, &body)
	switch action {
	case "validate":
		var cases []assuranceRequest
		if data, ok := body["cases"]; ok {
			encoded, _ := json.Marshal(data)
			_ = json.Unmarshal(encoded, &cases)
		}
		definition, _ := body["parser"].(map[string]any)
		if definition == nil {
			jsonError(w, "parser draft has no declarative parser", 409)
			return
		}
		validation := validateParserCandidate(definition, cases)
		body["validation"] = validation
		body["validation_passed"] = validation["failed"] == 0 && validation["total"] == len(cases)
	case "submit":
		if doc.State != "draft" && doc.State != "rejected" {
			jsonError(w, "parser is not in an editable state", 409)
			return
		}
		if body["validation_passed"] != true {
			jsonError(w, "all parser validation cases must pass before review", 409)
			return
		}
		body["review_state"] = "in_review"
	case "approve":
		if doc.State != "in_review" {
			jsonError(w, "parser is not awaiting review", 409)
			return
		}
		if doc.CreatedBy == p.Username {
			jsonError(w, "creator cannot approve their own parser", 409)
			return
		}
		body["review_state"] = "approved"
		body["reviewed_by"] = p.Username
	case "publish":
		if doc.State != "approved" {
			jsonError(w, "only approved parsers can be published", 409)
			return
		}
		body["review_state"] = "published"
		body["activated"] = true
		body["published_by"] = p.Username
	default:
		jsonError(w, "unknown parser action", 404)
		return
	}
	state := fmt.Sprint(body["review_state"])
	payload, _ := json.Marshal(body)
	saved, err := s.metadata.Put(r.Context(), metastore.Document{Collection: "parser-packs", ID: id, State: state, Payload: payload, CreatedBy: doc.CreatedBy})
	if err != nil {
		jsonError(w, "parser lifecycle update failed", 500)
		return
	}
	s.auditAction(p, "assurance.parser_"+action, "parser-packs/"+id, "success", nil)
	jsonOK(w, map[string]any{"id": id, "revision": saved.Revision, "state": state, "review_state": state, "published": state == "published"})
}
func (s *Server) recordAssuranceHistory(result map[string]any, actor string) {
	id := fmt.Sprintf("run-%d", time.Now().UnixNano())
	parser, _ := result["parser"].(map[string]any)
	coverage, _ := result["coverage"].(map[string]any)
	payload, _ := json.Marshal(map[string]any{"score": result["score"], "passed": result["passed"], "parser": parser, "coverage": coverage, "created_at": time.Now().UTC()})
	_, _ = s.metadata.Put(context.Background(), metastore.Document{Collection: "assurance-history", ID: id, State: "completed", Payload: payload, CreatedBy: actor})
}
func (s *Server) handleAssuranceHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "GET required", 405)
		return
	}
	docs, err := s.metadata.List(r.Context(), "assurance-history", metastore.Query{Limit: 100})
	if err != nil {
		jsonError(w, "assurance history unavailable", 503)
		return
	}
	events := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		var payload map[string]any
		_ = json.Unmarshal(doc.Payload, &payload)
		events = append(events, map[string]any{"id": doc.ID, "state": doc.State, "created_by": doc.CreatedBy, "created_at": doc.CreatedAt, "result": payload})
	}
	sort.Slice(events, func(i, j int) bool { return fmt.Sprint(events[i]["created_at"]) > fmt.Sprint(events[j]["created_at"]) })
	jsonOK(w, map[string]any{"events": events, "count": len(events)})
}
