package routing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nexora.local/nli-v15/internal/output"
	"nexora.local/nli-v15/telemetry"
)

func TestRouteV5QuarantineIsTelemetryOnly(t *testing.T) {
	root := t.TempDir()
	customerPath := filepath.Join(root, "customer.jsonl")
	metadataPath := filepath.Join(root, "telemetry.jsonl")
	router, err := NewV5Router(
		[]output.Config{{Type: "file", Path: customerPath}},
		[]output.Config{{Type: "file", Path: metadataPath}},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	metadata := telemetry.NewParseTelemetry("secret raw input", "node")
	metadata.ParserID = "pack/rule"
	if err := router.RouteV5(&V5ParseResult{ParseTelemetry: metadata, Quarantined: true}); err != nil {
		t.Fatal(err)
	}
	if err := router.Flush(); err != nil {
		t.Fatal(err)
	}
	customer, err := os.ReadFile(customerPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(customer) != 0 {
		t.Fatalf("quarantine emitted customer data: %s", customer)
	}
	encoded, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored telemetry.ParseTelemetry
	if err := json.Unmarshal(encoded, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Route != "quarantine" || stored.Result != "quarantined" || len(stored.DeliveryAcks) != 1 || stored.DeliveryAcks[0].Status != "skipped_quarantine" {
		t.Fatalf("quarantine telemetry incomplete: %+v", stored)
	}
}
