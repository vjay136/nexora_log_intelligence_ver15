// Package geoipdb acquires, validates, stages and activates offline GeoLite
// MMDB generations. It never performs a network request for an individual log
// line. Supabase distribution is a separate, explicitly consent-gated action.
package geoipdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

const (
	SourceRepository = "https://github.com/P3TERX/GeoLite.mmdb"
	LicenseEULA      = "https://www.maxmind.com/en/geolite2/eula"
	LicenseCCBYSA    = "https://creativecommons.org/licenses/by-sa/4.0/"
	Attribution      = "GeoLite2 database and contents Copyright (c) MaxMind, Inc.; sourced from P3TERX/GeoLite.mmdb."
	Bucket           = "nli-geoip-databases"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

var sizeLimits = map[string]int64{
	"GeoLite2-Country.mmdb": 24 << 20,
	"GeoLite2-ASN.mmdb":     48 << 20,
	"GeoLite2-City.mmdb":    128 << 20,
}

// Artifact describes one fixed, allowlisted GeoLite database.
type Artifact struct {
	Name       string `json:"name"`
	Database   string `json:"database_type"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size_bytes"`
	ETag       string `json:"etag,omitempty"`
	ModifiedAt string `json:"last_modified,omitempty"`
}

// Manifest records provenance for one immutable candidate generation.
type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	Generation    string     `json:"generation"`
	Source        string     `json:"source"`
	SourceCommit  string     `json:"source_commit"`
	FetchedAt     time.Time  `json:"fetched_at"`
	License       string     `json:"license"`
	LicenseEULA   string     `json:"license_eula"`
	Attribution   string     `json:"attribution"`
	Artifacts     []Artifact `json:"artifacts"`
	CandidateOnly bool       `json:"candidate_only"`
}

// Config controls candidate acquisition. The root contains immutable
// generations and metadata. The client-facing directory is kept at
// /etc/geoip-db and has one symlink per configured MMDB path.
type Config struct {
	Root       string
	HTTPClient *http.Client
	MaxBytes   map[string]int64
}

func defaults(c Config) Config {
	if c.Root == "" {
		c.Root = "/var/lib/nli-geoip"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{
			Timeout: 3 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				host := req.URL.Hostname()
				if len(via) >= 1 || req.URL.Scheme != "https" || host != "raw.githubusercontent.com" && host != "api.github.com" {
					return errors.New("GeoIP download redirect rejected")
				}
				return nil
			},
		}
	}
	if c.MaxBytes == nil {
		c.MaxBytes = make(map[string]int64, len(sizeLimits))
		for name, size := range sizeLimits {
			c.MaxBytes[name] = size
		}
	} else {
		merged := make(map[string]int64, len(sizeLimits))
		for name, size := range sizeLimits {
			merged[name] = size
			if configured := c.MaxBytes[name]; configured != 0 {
				merged[name] = configured
			}
		}
		c.MaxBytes = merged
	}
	return c
}

// FetchCandidate downloads a single version-pinned generation over HTTPS,
// validates the MMDB readers and writes manifest.json before atomically
// publishing the completed directory under generations/<commit>.
func FetchCandidate(ctx context.Context, config Config) (Manifest, error) {
	c := defaults(config)
	c.HTTPClient = lockedHTTPClient(c.HTTPClient)
	if err := validateRoot(c.Root); err != nil {
		return Manifest{}, err
	}
	commit, err := sourceCommit(ctx, c.HTTPClient)
	if err != nil {
		return Manifest{}, err
	}
	genRoot := filepath.Join(c.Root, "generations")
	if err := os.MkdirAll(genRoot, 0750); err != nil {
		return Manifest{}, err
	}
	final := filepath.Join(genRoot, commit)
	if existing, err := ReadManifest(final); err == nil {
		return existing, nil
	}
	stage, err := os.MkdirTemp(genRoot, ".stage-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(stage)

	manifest := Manifest{SchemaVersion: 1, Generation: commit, Source: SourceRepository, SourceCommit: commit, FetchedAt: time.Now().UTC(), License: "CC-BY-SA-4.0; GeoLite2 EULA applies", LicenseEULA: LicenseEULA, Attribution: Attribution, CandidateOnly: true}
	for _, item := range []struct{ name, database string }{
		{"GeoLite2-Country.mmdb", "GeoLite2-Country"},
		{"GeoLite2-ASN.mmdb", "GeoLite2-ASN"},
		{"GeoLite2-City.mmdb", "GeoLite2-City"},
	} {
		limit, err := effectiveLimit(c.MaxBytes, item.name)
		if err != nil {
			return Manifest{}, err
		}
		artifact, err := downloadArtifact(ctx, c.HTTPClient, commit, item.name, item.database, filepath.Join(stage, item.name), limit)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if err := writeSynced(filepath.Join(stage, "manifest.json"), data, 0640); err != nil {
		return Manifest{}, err
	}
	if err := syncDir(stage); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(stage, final); err != nil {
		if existing, readErr := ReadManifest(final); readErr == nil {
			return existing, nil
		}
		return Manifest{}, fmt.Errorf("publish candidate generation: %w", err)
	}
	if err := syncDir(genRoot); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func effectiveLimit(configured map[string]int64, name string) (int64, error) {
	maximum, exists := sizeLimits[name]
	if !exists {
		return 0, fmt.Errorf("unsupported GeoIP artifact %q", name)
	}
	limit := configured[name]
	if limit == 0 {
		return maximum, nil
	}
	if limit < 0 || limit > maximum {
		return 0, fmt.Errorf("size limit for %s must be between 1 and %d", name, maximum)
	}
	return limit, nil
}

func sourceCommit(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/P3TERX/GeoLite.mmdb/commits/download", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Nexora-Log-Intelligence/11 GeoIP updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("read GeoIP source version: %w", err)
	}
	defer resp.Body.Close()
	if resp.Request == nil || resp.Request.URL.Scheme != "https" || resp.Request.URL.Hostname() != "api.github.com" {
		return "", errors.New("GeoIP source-version host rejected")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GeoIP source-version endpoint returned HTTP %d", resp.StatusCode)
	}
	var value struct {
		SHA string `json:"sha"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if err := dec.Decode(&value); err != nil {
		return "", fmt.Errorf("decode GeoIP source version: %w", err)
	}
	value.SHA = strings.ToLower(value.SHA)
	if !commitPattern.MatchString(value.SHA) {
		return "", errors.New("GeoIP source returned an invalid commit identifier")
	}
	return value.SHA, nil
}

func downloadArtifact(ctx context.Context, client *http.Client, commit, name, database, destination string, limit int64) (Artifact, error) {
	if filepath.Base(name) != name || strings.Contains(name, "..") {
		return Artifact{}, errors.New("invalid GeoIP artifact name")
	}
	urlValue := "https://raw.githubusercontent.com/P3TERX/GeoLite.mmdb/" + commit + "/" + name
	u, err := url.Parse(urlValue)
	if err != nil || u.Scheme != "https" || u.Hostname() != "raw.githubusercontent.com" {
		return Artifact{}, errors.New("GeoIP source URL rejected")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Artifact{}, err
	}
	req.Header.Set("User-Agent", "Nexora-Log-Intelligence/11 GeoIP updater")
	resp, err := client.Do(req)
	if err != nil {
		return Artifact{}, fmt.Errorf("download %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.Request == nil || resp.Request.URL.Scheme != "https" || resp.Request.URL.Hostname() != "raw.githubusercontent.com" || resp.StatusCode != http.StatusOK {
		return Artifact{}, fmt.Errorf("GeoIP download rejected for %s (HTTP %d)", name, resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return Artifact{}, fmt.Errorf("%s exceeds %d byte limit", name, limit)
	}
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if err != nil {
		return Artifact{}, err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, limit+1))
	if copyErr == nil && size > limit {
		copyErr = fmt.Errorf("%s exceeds %d byte limit", name, limit)
	}
	if copyErr == nil {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil {
		return Artifact{}, copyErr
	}
	if closeErr != nil {
		return Artifact{}, closeErr
	}
	reader, err := maxminddb.Open(destination)
	if err != nil {
		return Artifact{}, fmt.Errorf("%s is not a valid MMDB: %w", name, err)
	}
	actualType := reader.Metadata.DatabaseType
	_ = reader.Close()
	if actualType != database {
		return Artifact{}, fmt.Errorf("%s has database type %q, expected %q", name, actualType, database)
	}
	return Artifact{Name: name, Database: actualType, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: size, ETag: sanitizeHeader(resp.Header.Get("ETag")), ModifiedAt: sanitizeHeader(resp.Header.Get("Last-Modified"))}, nil
}

// ReadManifest validates manifest identity and only reads metadata from an
// immutable generation directory; database content is validated on use too.
func ReadManifest(generationDir string) (Manifest, error) {
	var m Manifest
	data, err := os.ReadFile(filepath.Join(generationDir, "manifest.json"))
	if err != nil {
		return m, err
	}
	if len(data) > 64<<10 {
		return m, errors.New("GeoIP manifest exceeds 64 KiB")
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.SchemaVersion != 1 || !commitPattern.MatchString(m.Generation) || m.SourceCommit != m.Generation || m.Source != SourceRepository || !m.CandidateOnly || len(m.Artifacts) != 3 {
		return m, errors.New("invalid GeoIP candidate manifest")
	}
	seen := map[string]bool{}
	for _, item := range m.Artifacts {
		if _, ok := maxDatabase[item.Name]; !ok || seen[item.Name] || item.Database != maxDatabase[item.Name] || item.Size <= 0 || item.Size > sizeLimits[item.Name] || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(item.SHA256) {
			return m, errors.New("invalid GeoIP artifact metadata")
		}
		seen[item.Name] = true
	}
	return m, nil
}

// ValidateGeneration verifies every manifest entry, file size, digest, and
// MMDB type before an operator-approved generation can be activated.
func ValidateGeneration(root, generation string) (Manifest, error) {
	if !commitPattern.MatchString(generation) {
		return Manifest{}, errors.New("GeoIP generation must be a source commit SHA")
	}
	base := filepath.Join(root, "generations", generation)
	m, err := ReadManifest(base)
	if err != nil {
		return m, err
	}
	for _, artifact := range m.Artifacts {
		path := filepath.Join(base, artifact.Name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != artifact.Size {
			return m, fmt.Errorf("GeoIP candidate file %s is missing or has changed size", artifact.Name)
		}
		file, err := os.Open(path)
		if err != nil {
			return m, err
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, io.LimitReader(file, artifact.Size+1))
		closeErr := file.Close()
		if copyErr != nil {
			return m, copyErr
		}
		if closeErr != nil {
			return m, closeErr
		}
		if hex.EncodeToString(h.Sum(nil)) != artifact.SHA256 {
			return m, fmt.Errorf("GeoIP candidate file %s failed SHA-256 validation", artifact.Name)
		}
		reader, err := maxminddb.Open(path)
		if err != nil {
			return m, fmt.Errorf("validate %s: %w", artifact.Name, err)
		}
		databaseType := reader.Metadata.DatabaseType
		_ = reader.Close()
		if databaseType != artifact.Database || databaseType != maxDatabase[artifact.Name] {
			return m, fmt.Errorf("GeoIP candidate file %s has unexpected MMDB type %q", artifact.Name, databaseType)
		}
	}
	return m, nil
}

// Activation records the previously active generation for rollback.
type Activation struct {
	ActiveGeneration   string    `json:"active_generation"`
	PreviousGeneration string    `json:"previous_generation,omitempty"`
	ActivatedAt        time.Time `json:"activated_at"`
	ApprovedBy         string    `json:"approved_by"`
}

// Activate replaces only absent files or symlinks already owned by this
// updater. Existing regular MMDBs are first copied and validated into an
// immutable legacy generation, so rollback remains possible. Arbitrary
// symlinks, directories, and non-MMDB files are refused.
func Activate(root, clientDir, generation, approvedBy string) (Activation, error) {
	if strings.TrimSpace(approvedBy) == "" {
		return Activation{}, errors.New("administrator approval identity is required")
	}
	if err := validateRoot(root); err != nil {
		return Activation{}, err
	}
	if !filepath.IsAbs(clientDir) || strings.ContainsAny(clientDir, "\r\n\x00") {
		return Activation{}, errors.New("GeoIP client directory must be absolute")
	}
	manifest, err := ValidateGeneration(root, generation)
	if err != nil {
		return Activation{}, err
	}
	_ = manifest
	if err := os.MkdirAll(clientDir, 0750); err != nil {
		return Activation{}, err
	}
	info, err := os.Lstat(clientDir)
	if err != nil {
		return Activation{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Activation{}, errors.New("GeoIP client directory must be a real directory")
	}
	statePath := filepath.Join(root, "activation.json")
	previous, _ := readActivation(statePath)
	oldTargets := map[string]string{}
	for name := range maxDatabase {
		path := filepath.Join(clientDir, name)
		info, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			oldTargets[name] = ""
			continue
		}
		if statErr != nil {
			return Activation{}, statErr
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return Activation{}, fmt.Errorf("refusing to replace existing non-NLI GeoIP file %s; move or import it explicitly first", path)
		}
		target, readErr := os.Readlink(path)
		if readErr != nil {
			return Activation{}, readErr
		}
		target, _ = filepath.Abs(filepath.Join(filepath.Dir(path), target))
		managedRoot, _ := filepath.Abs(filepath.Join(root, "generations"))
		rel, relErr := filepath.Rel(managedRoot, target)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return Activation{}, fmt.Errorf("refusing unmanaged GeoIP symlink: %s", path)
		}
		generationID := strings.Split(rel, string(os.PathSeparator))[0]
		if filepath.Clean(target) != filepath.Join(managedRoot, generationID, name) {
			return Activation{}, fmt.Errorf("refusing mismatched GeoIP symlink target: %s", path)
		}
		if _, err := ValidateGeneration(root, generationID); err != nil {
			return Activation{}, fmt.Errorf("refusing unverified prior GeoIP symlink %s: %w", path, err)
		}
		oldTargets[name] = target
	}
	// Swap each fixed public path atomically. If any step fails, restore all
	// already-swapped paths before returning, then keep the current agent view.
	swapped := make([]string, 0, len(maxDatabase))
	restore := func() {
		for i := len(swapped) - 1; i >= 0; i-- {
			name := swapped[i]
			_ = replaceSymlink(filepath.Join(clientDir, name), oldTargets[name])
		}
	}
	names := []string{"GeoLite2-ASN.mmdb", "GeoLite2-City.mmdb", "GeoLite2-Country.mmdb"}
	for _, name := range names {
		path := filepath.Join(clientDir, name)
		target := filepath.Join(root, "generations", generation, name)
		if err := replaceSymlink(path, target); err != nil {
			restore()
			return Activation{}, err
		}
		swapped = append(swapped, name)
	}
	if err := syncDir(clientDir); err != nil {
		restore()
		return Activation{}, err
	}
	activation := Activation{ActiveGeneration: generation, PreviousGeneration: previous.ActiveGeneration, ActivatedAt: time.Now().UTC(), ApprovedBy: approvedBy}
	encoded, err := json.MarshalIndent(activation, "", "  ")
	if err == nil {
		err = atomicWrite(statePath, encoded, 0640)
	}
	if err != nil {
		restore()
		return Activation{}, err
	}
	return activation, nil
}

func lockedHTTPClient(source *http.Client) *http.Client {
	clone := *source
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 || req.URL.Scheme != "https" || (req.URL.Hostname() != "api.github.com" && req.URL.Hostname() != "raw.githubusercontent.com") {
			return errors.New("GeoIP HTTP redirect or host rejected")
		}
		return http.ErrUseLastResponse
	}
	return &clone
}

// Rollback switches to the retained previous generation. It still requires a
// named administrator and revalidates the generation before changing paths.
func Rollback(root, clientDir, approvedBy string) (Activation, error) {
	state, err := readActivation(filepath.Join(root, "activation.json"))
	if err != nil {
		return Activation{}, err
	}
	if state.PreviousGeneration == "" {
		return Activation{}, errors.New("no previous GeoIP generation is available")
	}
	return Activate(root, clientDir, state.PreviousGeneration, approvedBy)
}

func readActivation(path string) (Activation, error) {
	var state Activation
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if len(data) > 16<<10 {
		return state, errors.New("GeoIP activation state exceeds 16 KiB")
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func replaceSymlink(path, target string) error {
	if target == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	// Check the destination type immediately before replacing it. Refuse
	// anything except a regular file or symlink; target ownership is checked by
	// the caller before any swapping begins.
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("refusing unsafe GeoIP destination: %s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp := path + ".nli-next"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("create GeoIP symlink: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("activate GeoIP symlink: %w", err)
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nli-geoip-state-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

var maxDatabase = map[string]string{"GeoLite2-Country.mmdb": "GeoLite2-Country", "GeoLite2-ASN.mmdb": "GeoLite2-ASN", "GeoLite2-City.mmdb": "GeoLite2-City"}

func validateRoot(root string) error {
	if !filepath.IsAbs(root) || strings.ContainsAny(root, "\r\n\x00") {
		return errors.New("GeoIP root must be an absolute path")
	}
	if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("GeoIP root cannot be a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func sanitizeHeader(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	return value
}

func writeSynced(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
