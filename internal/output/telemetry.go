// Package output implements TelemetryWriter for strict bounded telemetry endpoint
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

// TelemetryWriter posts ParseTelemetry to the controller telemetry endpoint
type TelemetryWriter struct {
	cfg    Config
	client *http.Client
	mu     sync.Mutex
}

// NewTelemetryWriter creates a writer for the strict telemetry endpoint
func NewTelemetryWriter(cfg Config) (*TelemetryWriter, error) {
	if cfg.URL == "" || cfg.TokenFile == "" || (cfg.NodeID == "" && cfg.NodeIDFile == "") {
		return nil, fmt.Errorf("telemetry writer requires url, token_file, and node_id or node_id_file")
	}
	client, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}

	return &TelemetryWriter{
		cfg:    cfg,
		client: client,
	}, nil
}

func (w *TelemetryWriter) Write(telemetryData []byte, route string) error {
	if !w.acceptsRoute(route) {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Parse the telemetry data to validate it's metadata-only and bind it to
	// the enrolled node identity. Parser telemetry may identify a host name;
	// controller authorization must use the controller-issued node id.
	var telemetry map[string]interface{}
	if err := json.Unmarshal(telemetryData, &telemetry); err != nil {
		return fmt.Errorf("invalid telemetry data: %w", err)
	}

	// Ensure no forbidden customer content in telemetry (recursive check)
	if err := validateTelemetryContent(telemetry); err != nil {
		return err
	}
	nodeID := w.cfg.NodeID
	if nodeID == "" {
		idBytes, err := os.ReadFile(w.cfg.NodeIDFile)
		if err != nil {
			return fmt.Errorf("failed to read node id file: %w", err)
		}
		nodeID = strings.TrimSpace(string(idBytes))
	}
	if nodeID == "" {
		return fmt.Errorf("controller node identity is not enrolled")
	}
	telemetry["node_id"] = nodeID
	payload, err := json.Marshal(telemetry)
	if err != nil {
		return fmt.Errorf("encode telemetry data: %w", err)
	}
	tokenBytes, err := os.ReadFile(w.cfg.TokenFile)
	if err != nil {
		return fmt.Errorf("failed to read token file: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return fmt.Errorf("token file is empty")
	}

	// POST to telemetry endpoint
	endpoint := strings.TrimRight(w.cfg.URL, "/") + "/api/v1/telemetry"
	if err := post(w.client, w.cfg, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	}); err != nil {
		return fmt.Errorf("telemetry post failed: %w", err)
	}

	return nil
}

// validateTelemetryContent recursively checks for forbidden fields in telemetry data
func validateTelemetryContent(data interface{}) error {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			// Check for forbidden keys (case-insensitive)
			lowerKey := strings.ToLower(key)
			forbiddenKeys := []string{"raw", "_raw", "raw_text", "original", "data", "event", "payload", "fields"}
			for _, forbidden := range forbiddenKeys {
				if lowerKey == forbidden {
					return fmt.Errorf("telemetry contains forbidden field '%s'", key)
				}
			}
			// Check for message field containing entire original (common leak pattern)
			if lowerKey == "message" {
				if str, ok := value.(string); ok && len(str) > 1000 {
					return fmt.Errorf("message field too large, likely contains original log")
				}
			}
			// Recursively validate nested structures
			if err := validateTelemetryContent(value); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, item := range v {
			if err := validateTelemetryContent(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *TelemetryWriter) acceptsRoute(route string) bool {
	if len(w.cfg.Routes) == 0 {
		return true
	}
	for _, r := range w.cfg.Routes {
		if r == route {
			return true
		}
	}
	return false
}

func (w *TelemetryWriter) Close() error {
	return nil // HTTP client doesn't need explicit close
}
