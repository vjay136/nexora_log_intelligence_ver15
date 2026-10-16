package geoipdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestValidateRootRejectsRelativeAndSymlink(t *testing.T) {
	if err := validateRoot("relative"); err == nil {
		t.Fatal("relative root was accepted")
	}
	root := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := validateRoot(root); err == nil {
		t.Fatal("symlink root was accepted")
	}
}

func TestConfiguredSizeBoundsCanOnlyTightenDefaults(t *testing.T) {
	got, err := effectiveLimit(map[string]int64{"GeoLite2-City.mmdb": 64 << 20}, "GeoLite2-City.mmdb")
	if err != nil || got != 64<<20 {
		t.Fatalf("tightened size bound: got=%d err=%v", got, err)
	}
	got, err = effectiveLimit(nil, "GeoLite2-Country.mmdb")
	if err != nil || got != sizeLimits["GeoLite2-Country.mmdb"] {
		t.Fatalf("default size bound: got=%d err=%v", got, err)
	}
	if _, err := effectiveLimit(map[string]int64{"GeoLite2-City.mmdb": 256 << 20}, "GeoLite2-City.mmdb"); err == nil {
		t.Fatal("configured limit was allowed to exceed fixed safety bound")
	}
}

func TestCandidateManifestRejectsInvalidArtifactSet(t *testing.T) {
	root := t.TempDir()
	gen := strings.Repeat("a", 40)
	dir := filepath.Join(root, "generations", gen)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: 1, Generation: gen, SourceCommit: gen, Artifacts: []Artifact{{Name: "GeoLite2-Country.mmdb", Database: "GeoLite2-Country", SHA256: strings.Repeat("b", 64), Size: 100}}}
	data, _ := jsonMarshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(dir); err == nil {
		t.Fatal("incomplete artifact set was accepted")
	}
}

func TestDownloadRejectsInvalidMMDBAndOversize(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name  string
		body  []byte
		limit int64
		want  string
	}{
		{"malformed", []byte("not a database"), 1024, "not a valid MMDB"},
		{"oversize", bytes.Repeat([]byte("x"), 12), 8, "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(tc.body)), ContentLength: -1, Request: req}, nil
			})}
			_, err := downloadArtifact(context.Background(), client, commit, "GeoLite2-Country.mmdb", "GeoLite2-Country", filepath.Join(t.TempDir(), "candidate.mmdb"), tc.limit)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLicenseMustBeAcceptedBeforeSupabasePublish(t *testing.T) {
	_, err := PublishCandidate(context.Background(), "", strings.Repeat("a", 40), "admin", false, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "license acceptance") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRedirectAllowlistRejectsShortenerAndHTTP(t *testing.T) {
	client := defaults(Config{}).HTTPClient
	checks := []struct {
		url     string
		wantErr bool
	}{
		{"https://raw.githubusercontent.com/P3TERX/GeoLite.mmdb/download/file", false},
		{"https://api.github.com/repos/P3TERX/GeoLite.mmdb", false},
		{"https://git.io/GeoLite2-City.mmdb", true},
		{"http://raw.githubusercontent.com/P3TERX/GeoLite.mmdb/file", true},
	}
	for _, check := range checks {
		req, _ := http.NewRequest(http.MethodGet, check.url, nil)
		err := client.CheckRedirect(req, nil)
		if check.wantErr && err == nil {
			t.Errorf("redirect allowed: %s", check.url)
		}
		if !check.wantErr && err != nil {
			t.Errorf("allowed redirect rejected: %v", err)
		}
	}
}

func TestValidateGenerationRejectsMutatedCandidate(t *testing.T) {
	gen := strings.Repeat("c", 40)
	dir := filepath.Join(t.TempDir(), "generations", gen)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateGeneration(filepath.Dir(filepath.Dir(dir)), gen); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing manifest rejection, got %v", err)
	}
}

// Keep JSON encoding explicit in this focused test without exposing manifest internals.
func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }
