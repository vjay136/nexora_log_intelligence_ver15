package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func testManifest(t *testing.T) (ImmutableManifest, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0).UTC()
	manifest := ImmutableManifest{
		SchemaVersion: SyncProtocolVersion, ID: "manifest-1", Revision: 7,
		Desired:              HashState{PackHash: strings.Repeat("a", 64), ConfigHash: strings.Repeat("b", 64), ReleaseHash: strings.Repeat("c", 64)},
		Rollback:             HashState{PackHash: strings.Repeat("d", 64), ConfigHash: strings.Repeat("e", 64), ReleaseHash: strings.Repeat("f", 64)},
		Artifacts:            []ArtifactRef{{Name: "parser-pack.json", URL: "/api/v1/artifacts/parser-pack.json", SHA256: strings.Repeat("a", 64), Size: 42, Type: "parser_pack"}},
		MinimumClientVersion: "5.0.0", RequiredCapabilities: []string{"stage", "validate", "activate", "rollback"},
		IssuedAt: now, ExpiresAt: now.Add(time.Hour), KeyID: "controller-1",
	}
	return manifest, publicKey, privateKey
}

func TestImmutableManifestSignVerifyAndTamper(t *testing.T) {
	manifest, publicKey, privateKey := testManifest(t)
	if err := manifest.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Verify(map[string]ed25519.PublicKey{"controller-1": publicKey}, manifest.IssuedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	manifest.Desired.PackHash = strings.Repeat("9", 64)
	if err := manifest.Verify(map[string]ed25519.PublicKey{"controller-1": publicKey}, manifest.IssuedAt.Add(time.Minute)); err == nil {
		t.Fatal("tampered manifest verified")
	}
}

func TestImmutableManifestRejectsExpiryAndInvalidHash(t *testing.T) {
	manifest, publicKey, privateKey := testManifest(t)
	if err := manifest.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Verify(map[string]ed25519.PublicKey{"controller-1": publicKey}, manifest.ExpiresAt); err == nil {
		t.Fatal("expired manifest verified")
	}
	manifest, _, privateKey = testManifest(t)
	manifest.Desired.ConfigHash = "short"
	if err := manifest.Sign(privateKey); err == nil {
		t.Fatal("invalid hash was signed")
	}
}

func TestCompareSemverAndCapabilities(t *testing.T) {
	if comparison, err := CompareSemver("5.1.0", "5.0.9"); err != nil || comparison != 1 {
		t.Fatalf("comparison=%d err=%v", comparison, err)
	}
	if _, err := CompareSemver("5.01.0", "5.0.0"); err == nil {
		t.Fatal("non-canonical version accepted")
	}
	capabilities := NodeCapabilities{Commands: []string{"stage", "activate"}}
	if !capabilities.SupportsCommand("activate") || capabilities.SupportsCommand("rollback") {
		t.Fatal("capability lookup incorrect")
	}
}
