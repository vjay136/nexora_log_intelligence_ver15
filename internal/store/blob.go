package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BlobStore is a controller-only immutable object boundary. Callers must pass
// verified content-addressed objects; ordinary event/log payloads are forbidden.
type BlobStore interface {
	PutVerified(context.Context, string, string, io.Reader, int64, string, string) error
	Open(context.Context, string, string) (io.ReadCloser, int64, error)
	Delete(context.Context, string, string) error
	Mode() string
}

type supabaseBlobStore struct {
	base   *url.URL
	secret string
	client *http.Client
}

// NewSupabaseBlobStore creates a Storage client using only the controller's
// Supabase secret key. The key must never be sent to browser code.
func NewSupabaseBlobStore(projectURL, secretKey string, client *http.Client) (BlobStore, error) {
	base, err := url.Parse(strings.TrimSpace(projectURL))
	if err != nil || base.Host == "" || base.Scheme != "https" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("invalid Supabase Storage project URL")
	}
	if !strings.HasPrefix(secretKey, "sb_secret_") || len(secretKey) < 20 {
		return nil, errors.New("SUPABASE_SECRET_KEY must be a server-only sb_secret_ key")
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	base.Path = ""
	return &supabaseBlobStore{base: base, secret: secretKey, client: client}, nil
}

func (s *supabaseBlobStore) Mode() string { return "supabase-private-storage" }
func validateObjectPath(bucket, object string) error {
	parts := strings.Split(object, "/")
	switch bucket {
	case "nli-rollout-artifacts":
		if len(parts) != 3 || (parts[0] != "rpm" && parts[0] != "config") || parts[1] != "sha256" || len(parts[2]) < 68 {
			return errors.New("only rollout RPM/config object paths are allowed")
		}
		digest := parts[2][:64]
		if _, err := hex.DecodeString(digest); err != nil {
			return errors.New("invalid content-addressed object path")
		}
		ext := strings.ToLower(filepath.Ext(parts[2]))
		if parts[0] == "rpm" && ext != ".rpm" || parts[0] == "config" && ext != ".json" {
			return errors.New("rollout path extension does not match purpose")
		}
	case "nli-assurance-fixtures":
		if len(parts) != 2 || parts[0] != "sha256" || len(parts[1]) != 68 || !strings.HasSuffix(parts[1], ".enc") {
			return errors.New("only encrypted Assurance fixture object paths are allowed")
		}
		if _, err := hex.DecodeString(strings.TrimSuffix(parts[1], ".enc")); err != nil {
			return errors.New("invalid fixture content-addressed path")
		}
	case "nli-geoip-databases":
		if len(parts) != 2 || parts[0] != "sha256" {
			return errors.New("GeoIP objects must use a content-addressed SHA-256 path")
		}
		name := strings.TrimPrefix(parts[1], strings.SplitN(parts[1], "-", 2)[0]+"-")
		if len(parts[1]) < 64+1 || len(parts[1]) > 128 {
			return errors.New("invalid GeoIP content-addressed path")
		}
		digest := parts[1][:64]
		if parts[1][64] != '-' || name != "GeoLite2-Country.mmdb" && name != "GeoLite2-ASN.mmdb" && name != "GeoLite2-City.mmdb" {
			return errors.New("only the three reviewed GeoLite2 MMDB filenames are allowed")
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return errors.New("invalid GeoIP SHA-256 path")
		}
	default:
		return errors.New("unsupported private NLI Storage bucket")
	}
	return nil
}
func validateObjectPurpose(bucket, object, mimeType string, size int64, expectedDigest string) error {
	if err := validateObjectPath(bucket, object); err != nil {
		return err
	}
	switch bucket {
	case "nli-rollout-artifacts":
		parts := strings.Split(object, "/")
		if len(parts) != 3 || (parts[0] != "rpm" && parts[0] != "config") || parts[1] != "sha256" {
			return errors.New("only content-addressed rollout RPM and config objects are allowed in Supabase Storage")
		}
		ext := strings.ToLower(filepath.Ext(parts[2]))
		if parts[2][:64] != expectedDigest {
			return errors.New("rollout object path must be content-addressed by SHA-256")
		}
		if parts[0] == "rpm" && (ext != ".rpm" || (mimeType != "application/x-rpm" && mimeType != "application/octet-stream")) {
			return errors.New("rollout bucket accepts only RPM artifacts")
		}
		if parts[0] == "config" && (ext != ".json" || mimeType != "application/json") {
			return errors.New("rollout bucket accepts only JSON configuration bundles")
		}
	case "nli-assurance-fixtures":
		parts := strings.Split(object, "/")
		if parts[1][:64] != expectedDigest || mimeType != "application/octet-stream" || size > 8<<20 {
			return errors.New("Assurance bucket accepts only bounded encrypted, content-addressed fixtures")
		}
	case "nli-geoip-databases":
		parts := strings.Split(object, "/")
		if len(parts) != 2 || len(parts[1]) < 65 || parts[1][64] != '-' || parts[1][:64] != expectedDigest || mimeType != "application/octet-stream" || size <= 0 || size > 128<<20 {
			return errors.New("GeoIP Storage accepts only bounded, SHA-256-addressed GeoLite2 MMDB files")
		}
	default:
		return errors.New("unsupported private NLI Storage bucket")
	}
	return nil
}
func (s *supabaseBlobStore) objectURL(bucket, object string) (*url.URL, error) {
	if object == "" || strings.HasPrefix(object, "/") || strings.Contains(object, "..") || strings.Contains(object, "\\") {
		return nil, errors.New("invalid immutable object path")
	}
	if bucket != "nli-assurance-fixtures" && bucket != "nli-rollout-artifacts" && bucket != "nli-geoip-databases" {
		return nil, errors.New("unsupported private NLI Storage bucket")
	}
	if err := validateObjectPath(bucket, object); err != nil {
		return nil, err
	}
	u := *s.base
	u.Path = "/storage/v1/object/" + bucket + "/" + object
	return &u, nil
}
func (s *supabaseBlobStore) do(ctx context.Context, method, bucket, object string, body io.Reader, size int64, mimeType string) (*http.Response, error) {
	u, err := s.objectURL(bucket, object)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", s.secret)
	req.Header.Set("Authorization", "Bearer "+s.secret)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", mimeType)
		req.ContentLength = size
		req.Header.Set("x-upsert", "false")
	}
	return s.client.Do(req)
}
func (s *supabaseBlobStore) PutVerified(ctx context.Context, bucket, object string, source io.Reader, size int64, mimeType, expectedSHA256 string) error {
	if size <= 0 || mimeType == "" || len(expectedSHA256) != 64 {
		return errors.New("size, MIME type and SHA-256 are required")
	}
	if err := validateObjectPurpose(bucket, object, mimeType, size, strings.ToLower(expectedSHA256)); err != nil {
		return err
	}
	temp, err := os.CreateTemp("", "nli-blob-*.staging")
	if err != nil {
		return err
	}
	defer func() { _ = temp.Close(); _ = os.Remove(temp.Name()) }()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(source, size+1))
	if err != nil {
		return err
	}
	if written != size {
		return errors.New("object size mismatch")
	}
	digest := hash.Sum(nil)
	if !strings.EqualFold(hex.EncodeToString(digest), expectedSHA256) {
		return errors.New("object SHA-256 mismatch")
	}
	if _, err = temp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if size > 6<<20 {
		return s.putTUS(ctx, bucket, object, temp, size, mimeType, digest)
	}
	resp, err := s.do(ctx, http.MethodPost, bucket, object, temp, size, mimeType)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return s.verifyExisting(ctx, bucket, object, digest[:])
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Supabase private object upload failed with HTTP %d", resp.StatusCode)
	}
	return nil
}

func (s *supabaseBlobStore) putTUS(ctx context.Context, bucket, object string, content *os.File, size int64, mimeType string, digest []byte) error {
	// Supabase recommends TUS for objects above 6 MiB. Use a direct storage
	// hostname when this is the standard project hostname.
	u, err := s.objectURL(bucket, object)
	if err != nil {
		return err
	}
	host := u.Host
	hostname := u.Hostname()
	if strings.HasSuffix(hostname, ".supabase.co") && !strings.HasSuffix(hostname, ".storage.supabase.co") {
		host = strings.TrimSuffix(hostname, ".supabase.co") + ".storage.supabase.co"
	}
	endpoint := *u
	endpoint.Scheme = "https"
	endpoint.Host = host
	endpoint.Path = "/storage/v1/upload/resumable"
	metadata := func(key, value string) string { return key + " " + base64.StdEncoding.EncodeToString([]byte(value)) }
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("apikey", s.secret)
	request.Header.Set("Authorization", "Bearer "+s.secret)
	request.Header.Set("Tus-Resumable", "1.0.0")
	request.Header.Set("Upload-Length", strconv.FormatInt(size, 10))
	request.Header.Set("Upload-Metadata", strings.Join([]string{metadata("bucketName", bucket), metadata("objectName", object), metadata("contentType", mimeType), metadata("cacheControl", "3600")}, ","))
	request.Header.Set("x-upsert", "false")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode == http.StatusConflict {
		return s.verifyExisting(ctx, bucket, object, digest)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Supabase resumable upload initialization failed with HTTP %d", response.StatusCode)
	}
	location := response.Header.Get("Location")
	if location == "" {
		return errors.New("Supabase resumable upload omitted its upload location")
	}
	uploadURL, err := endpoint.Parse(location)
	if err != nil || uploadURL.Scheme != "https" || !strings.EqualFold(uploadURL.Host, endpoint.Host) {
		return errors.New("Supabase resumable upload returned an unsafe location")
	}
	const chunkSize = 6 << 20
	var offset int64
	for offset < size {
		length := int64(chunkSize)
		if size-offset < length {
			length = size - offset
		}
		chunk := io.NewSectionReader(content, offset, length)
		patch, err := http.NewRequestWithContext(ctx, http.MethodPatch, uploadURL.String(), chunk)
		if err != nil {
			return err
		}
		patch.Header.Set("apikey", s.secret)
		patch.Header.Set("Authorization", "Bearer "+s.secret)
		patch.Header.Set("Tus-Resumable", "1.0.0")
		patch.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
		patch.Header.Set("Content-Type", "application/offset+octet-stream")
		patch.ContentLength = length
		response, err := s.client.Do(patch)
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			return fmt.Errorf("Supabase resumable upload chunk failed with HTTP %d", response.StatusCode)
		}
		offset += length
	}
	return nil
}
func (s *supabaseBlobStore) verifyExisting(ctx context.Context, bucket, object string, want []byte) error {
	r, _, err := s.Open(ctx, bucket, object)
	if err != nil {
		return err
	}
	defer r.Close()
	h := sha256.New()
	if _, err = io.Copy(h, r); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), hex.EncodeToString(want)) {
		return errors.New("immutable object path already contains different content")
	}
	return nil
}
func (s *supabaseBlobStore) Open(ctx context.Context, bucket, object string) (io.ReadCloser, int64, error) {
	resp, err := s.do(ctx, http.MethodGet, bucket, object, nil, 0, "")
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, 0, os.ErrNotExist
		}
		return nil, 0, fmt.Errorf("Supabase private object download failed with HTTP %d", resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}

func (s *supabaseBlobStore) Delete(ctx context.Context, bucket, object string) error {
	if bucket != "nli-assurance-fixtures" && bucket != "nli-rollout-artifacts" {
		return errors.New("unsupported private NLI Storage bucket")
	}
	if err := validateObjectPath(bucket, object); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string][]string{"prefixes": {object}})
	endpoint := *s.base
	endpoint.Path = "/storage/v1/object/" + bucket
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", s.secret)
	req.Header.Set("Authorization", "Bearer "+s.secret)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Supabase private object delete failed with HTTP %d", resp.StatusCode)
	}
	return nil
}
