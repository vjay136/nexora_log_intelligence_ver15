package protocol

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const SyncProtocolVersion = "v2"

// HashState is the immutable content identity for one deployed generation.
type HashState struct {
	PackHash    string `json:"pack_hash,omitempty"`
	ConfigHash  string `json:"config_hash,omitempty"`
	ReleaseHash string `json:"release_hash,omitempty"`
}

func (state HashState) Equal(other HashState) bool {
	return state.PackHash == other.PackHash && state.ConfigHash == other.ConfigHash && state.ReleaseHash == other.ReleaseHash
}

func (state HashState) Empty() bool {
	return state.PackHash == "" && state.ConfigHash == "" && state.ReleaseHash == ""
}

func (state HashState) Validate() error {
	for name, value := range map[string]string{"pack_hash": state.PackHash, "config_hash": state.ConfigHash, "release_hash": state.ReleaseHash} {
		if value != "" && !validSHA256(value) {
			return fmt.Errorf("%s must be a 64-character SHA-256", name)
		}
	}
	return nil
}

// ArtifactRef is signed as part of its parent manifest.
type ArtifactRef struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Type      string `json:"type"` // parser_pack, config, release
	Signature string `json:"signature,omitempty"`
}

// NodeCapabilities are negotiated before commands are emitted.
type NodeCapabilities struct {
	ProtocolVersion  string   `json:"protocol_version"`
	ClientVersion    string   `json:"client_version"`
	Commands         []string `json:"commands"`
	ArtifactTypes    []string `json:"artifact_types"`
	SignatureSchemes []string `json:"signature_schemes"`
	MaxArtifactBytes int64    `json:"max_artifact_bytes"`
	SupportsRollback bool     `json:"supports_rollback"`
}

func (capabilities NodeCapabilities) SupportsCommand(command string) bool {
	for _, candidate := range capabilities.Commands {
		if candidate == command {
			return true
		}
	}
	return false
}

// ImmutableManifest binds desired, staged, active, and rollback generations to
// one monotonically versioned controller decision.
type ImmutableManifest struct {
	SchemaVersion        string        `json:"schema_version"`
	ID                   string        `json:"id"`
	Revision             uint64        `json:"revision"`
	Desired              HashState     `json:"desired"`
	Staged               HashState     `json:"staged,omitempty"`
	Active               HashState     `json:"active,omitempty"`
	Rollback             HashState     `json:"rollback,omitempty"`
	Artifacts            []ArtifactRef `json:"artifacts"`
	MinimumClientVersion string        `json:"minimum_client_version"`
	RequiredCapabilities []string      `json:"required_capabilities"`
	IssuedAt             time.Time     `json:"issued_at"`
	ExpiresAt            time.Time     `json:"expires_at"`
	KeyID                string        `json:"key_id"`
	Signature            string        `json:"signature"`
}

func (manifest ImmutableManifest) canonicalBytes() ([]byte, error) {
	manifest.Signature = ""
	return json.Marshal(manifest)
}

func (manifest *ImmutableManifest) Sign(privateKey ed25519.PrivateKey) error {
	if len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid Ed25519 private key")
	}
	if err := manifest.validate(false, time.Time{}); err != nil {
		return err
	}
	canonical, err := manifest.canonicalBytes()
	if err != nil {
		return err
	}
	manifest.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical))
	return nil
}

func (manifest ImmutableManifest) Verify(trustedKeys map[string]ed25519.PublicKey, now time.Time) error {
	if err := manifest.validate(true, now); err != nil {
		return err
	}
	publicKey, exists := trustedKeys[manifest.KeyID]
	if !exists || len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("manifest signing key %q is not trusted", manifest.KeyID)
	}
	signature, err := base64.RawStdEncoding.DecodeString(manifest.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("manifest signature is malformed")
	}
	canonical, err := manifest.canonicalBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, canonical, signature) {
		return fmt.Errorf("manifest signature verification failed")
	}
	return nil
}

func (manifest ImmutableManifest) Digest() (string, error) {
	canonical, err := manifest.canonicalBytes()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func (manifest ImmutableManifest) validate(requireSignature bool, now time.Time) error {
	if manifest.SchemaVersion != SyncProtocolVersion || manifest.ID == "" || manifest.Revision == 0 || manifest.KeyID == "" {
		return fmt.Errorf("manifest schema, id, revision, and key_id are required")
	}
	if manifest.Desired.Empty() {
		return fmt.Errorf("manifest desired state is empty")
	}
	for _, state := range []HashState{manifest.Desired, manifest.Staged, manifest.Active, manifest.Rollback} {
		if err := state.Validate(); err != nil {
			return err
		}
	}
	if !validSemver(manifest.MinimumClientVersion) {
		return fmt.Errorf("minimum_client_version must be semantic version x.y.z")
	}
	if manifest.IssuedAt.IsZero() || manifest.ExpiresAt.IsZero() || !manifest.ExpiresAt.After(manifest.IssuedAt) {
		return fmt.Errorf("manifest validity window is invalid")
	}
	if !now.IsZero() && (now.Before(manifest.IssuedAt) || !now.Before(manifest.ExpiresAt)) {
		return fmt.Errorf("manifest is outside its validity window")
	}
	if requireSignature && manifest.Signature == "" {
		return fmt.Errorf("manifest signature is required")
	}
	seenArtifacts := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		if artifact.Name == "" || seenArtifacts[artifact.Name] || !validSHA256(artifact.SHA256) || artifact.Size <= 0 {
			return fmt.Errorf("manifest contains invalid or duplicate artifact %q", artifact.Name)
		}
		seenArtifacts[artifact.Name] = true
		if artifact.Type != "parser_pack" && artifact.Type != "config" && artifact.Type != "release" {
			return fmt.Errorf("artifact %q has unsupported type %q", artifact.Name, artifact.Type)
		}
		parsed, err := url.Parse(artifact.URL)
		if err != nil || (parsed.Scheme != "https" && !(parsed.Scheme == "" && strings.HasPrefix(parsed.Path, "/"))) {
			return fmt.Errorf("artifact %q URL must be HTTPS or controller-relative", artifact.Name)
		}
	}
	return nil
}

// SyncRequest reports all local generations and the last accepted revision.
type SyncRequest struct {
	NodeID               string           `json:"node_id"`
	Capabilities         NodeCapabilities `json:"capabilities"`
	Active               HashState        `json:"active"`
	Staged               HashState        `json:"staged"`
	Rollback             HashState        `json:"rollback"`
	LastManifestID       string           `json:"last_manifest_id,omitempty"`
	LastManifestRevision uint64           `json:"last_manifest_revision"`
	LastCompletedCommand string           `json:"last_completed_command,omitempty"`
	ReportedAt           time.Time        `json:"reported_at"`
}

// SyncResponse is a reconciliation decision; the manifest remains signed and
// immutable while leased commands are idempotent delivery records.
type SyncResponse struct {
	Status              string             `json:"status"` // synced, drifted, incompatible, conflict
	Manifest            *ImmutableManifest `json:"manifest,omitempty"`
	Commands            []SyncCommand      `json:"commands,omitempty"`
	MinimumClient       string             `json:"minimum_client_version"`
	MissingCapabilities []string           `json:"missing_capabilities,omitempty"`
	RetryAfter          time.Duration      `json:"retry_after,omitempty"`
}

// SyncCommand is deterministic within a manifest revision.
type SyncCommand struct {
	ID            string    `json:"id"`
	ManifestID    string    `json:"manifest_id"`
	Revision      uint64    `json:"revision"`
	Type          string    `json:"type"` // stage, validate, activate, rollback
	Prerequisites []string  `json:"prerequisites,omitempty"`
	LeaseUntil    time.Time `json:"lease_until,omitempty"`
	Attempt       int       `json:"attempt"`
	Idempotent    bool      `json:"idempotent"`
}

// SyncCommandStatus is the durable node acknowledgement.
type SyncCommandStatus struct {
	NodeID      string    `json:"node_id"`
	CommandID   string    `json:"command_id"`
	ManifestID  string    `json:"manifest_id"`
	Revision    uint64    `json:"revision"`
	Status      string    `json:"status"` // staged, validated, activated, rolled_back, failed
	State       HashState `json:"state"`
	ErrorCode   string    `json:"error_code,omitempty"`
	Message     string    `json:"message,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

func CompareSemver(left, right string) (int, error) {
	leftParts, err := parseSemver(left)
	if err != nil {
		return 0, err
	}
	rightParts, err := parseSemver(right)
	if err != nil {
		return 0, err
	}
	for index := range leftParts {
		if leftParts[index] < rightParts[index] {
			return -1, nil
		}
		if leftParts[index] > rightParts[index] {
			return 1, nil
		}
	}
	return 0, nil
}

func parseSemver(value string) ([3]int, error) {
	var result [3]int
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("invalid semantic version %q", value)
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || (len(part) > 1 && part[0] == '0') {
			return result, fmt.Errorf("invalid semantic version %q", value)
		}
		result[index] = number
	}
	return result, nil
}

func validSemver(value string) bool {
	_, err := parseSemver(value)
	return err == nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
