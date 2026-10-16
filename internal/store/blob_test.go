package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestSupabaseBlobStoreUsesControllerSecretAndImmutableVerifiedObjects(t *testing.T) {
	secret := "sb_secret_" + strings.Repeat("s", 40)
	content := []byte("signed artifact bytes")
	sum := sha256.Sum256(content)
	var stored []byte
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != secret || r.Header.Get("Authorization") != "Bearer "+secret {
			t.Errorf("Storage request did not use only controller secret")
		}
		if strings.Contains(r.Header.Get("apikey"), "publishable") {
			t.Errorf("publishable credential used for artifact object")
		}
		switch r.Method {
		case http.MethodPost:
			if r.Header.Get("x-upsert") != "false" {
				t.Errorf("object is not immutable: x-upsert=%q", r.Header.Get("x-upsert"))
			}
			stored, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Write(stored)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	blobs, err := NewSupabaseBlobStore(server.URL, secret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	object := "rpm/sha256/" + hex.EncodeToString(sum[:]) + ".rpm"
	if err := blobs.PutVerified(context.Background(), "nli-rollout-artifacts", object, bytes.NewReader(content), int64(len(content)), "application/x-rpm", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("stored content mismatch: %q", stored)
	}
	reader, _, err := blobs.Open(context.Background(), "nli-rollout-artifacts", object)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, content) {
		t.Fatalf("download mismatch: %q", got)
	}
	if err := blobs.PutVerified(context.Background(), "nli-rollout-artifacts", object, bytes.NewReader([]byte("changed")), 7, "application/x-rpm", hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("mismatched digest accepted")
	}
}

func TestSupabaseBlobStoreUsesTusForLargeRPMs(t *testing.T) {
	secret := "sb_secret_" + strings.Repeat("k", 40)
	// Supabase recommends resumable uploads for objects larger than 6 MiB.
	content := bytes.Repeat([]byte("rpm"), (2<<20)+1)
	sum := sha256.Sum256(content)
	var received bytes.Buffer
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("TUS did not use controller secret")
		}
		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/storage/v1/upload/resumable" || r.Header.Get("Upload-Length") != strconv.Itoa(len(content)) || r.Header.Get("x-upsert") != "false" {
				t.Errorf("bad TUS initialization: %s %+v", r.URL.Path, r.Header)
			}
			w.Header().Set("Location", server.URL+"/tus/session")
			w.WriteHeader(http.StatusCreated)
		case http.MethodPatch:
			if r.Header.Get("Tus-Resumable") != "1.0.0" || r.Header.Get("Content-Type") != "application/offset+octet-stream" {
				t.Error("bad TUS patch headers")
			}
			_, _ = io.Copy(&received, r.Body)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	blobs, err := NewSupabaseBlobStore(server.URL, secret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := blobs.PutVerified(context.Background(), "nli-rollout-artifacts", "rpm/sha256/"+hex.EncodeToString(sum[:])+".rpm", bytes.NewReader(content), int64(len(content)), "application/x-rpm", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received.Bytes(), content) {
		t.Fatalf("TUS transfer mismatch: got=%d want=%d", received.Len(), len(content))
	}
}

func TestSupabaseBlobStoreRejectsPublicOrUnsafeAccess(t *testing.T) {
	if _, err := NewSupabaseBlobStore("https://project.supabase.co", "sb_publishable_"+strings.Repeat("p", 40), nil); err == nil {
		t.Fatal("publishable key accepted as Storage secret")
	}
	if _, err := NewSupabaseBlobStore("http://project.supabase.co", "sb_secret_"+strings.Repeat("s", 40), nil); err == nil {
		t.Fatal("insecure remote URL accepted")
	}
	secret := "sb_secret_" + strings.Repeat("s", 40)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") == "" {
			t.Error("request had no key")
		}
		http.Error(w, "anonymous denied", http.StatusUnauthorized)
	}))
	defer server.Close()
	blobs, err := NewSupabaseBlobStore(server.URL, secret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := blobs.Open(context.Background(), "nli-rollout-artifacts", "../secret"); err == nil {
		t.Fatal("unsafe object path accepted")
	}
	if _, _, err := blobs.Open(context.Background(), "nli-assurance-fixtures", "missing"); err == nil {
		t.Fatal("unauthorized Storage request accepted")
	}
}

func TestGeoIPStorageBucketAcceptsOnlyPinnedMMDBArtifacts(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, name := range []string{"GeoLite2-Country.mmdb", "GeoLite2-ASN.mmdb", "GeoLite2-City.mmdb"} {
		object := "sha256/" + digest + "-" + name
		if err := validateObjectPurpose("nli-geoip-databases", object, "application/octet-stream", 1024, digest); err != nil {
			t.Errorf("valid %s rejected: %v", name, err)
		}
	}
	for _, tc := range []struct {
		object string
		mime   string
		size   int64
	}{
		{"sha256/" + digest + "-other.mmdb", "application/octet-stream", 1024},
		{"sha256/" + digest + "-GeoLite2-City.mmdb", "application/json", 1024},
		{"sha256/" + digest + "-GeoLite2-City.mmdb", "application/octet-stream", (128 << 20) + 1},
		{"sha256/" + strings.Repeat("b", 64) + "-GeoLite2-City.mmdb", "application/octet-stream", 1024},
	} {
		if err := validateObjectPurpose("nli-geoip-databases", tc.object, tc.mime, tc.size, digest); err == nil {
			t.Errorf("invalid GeoIP object accepted: %+v", tc)
		}
	}
}
