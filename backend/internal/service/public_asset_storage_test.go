package service

import (
	"context"
	"net/url"
	"os"
	"path"
	"strings"
	"testing"
)

func TestPublicAssetUsesOSS(t *testing.T) {
	cfg := testSystemConfig(map[string]string{
		"storage.result_cache_driver": "oss",
		"oss.enabled":                 "true",
	})
	if !PublicAssetUsesOSS(context.Background(), cfg) {
		t.Fatal("PublicAssetUsesOSS() = false, want true")
	}
}

func TestPublicAssetUsesLocalWhenOSSDisabled(t *testing.T) {
	cfg := testSystemConfig(map[string]string{
		"storage.result_cache_driver": "oss",
		"oss.enabled":                 "false",
	})
	if PublicAssetUsesOSS(context.Background(), cfg) {
		t.Fatal("PublicAssetUsesOSS() = true, want false")
	}
}

func TestUploadPublicAssetToOSSMissingConfig(t *testing.T) {
	file := path.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(file, []byte("cover"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := testSystemConfig(map[string]string{
		"storage.result_cache_driver": "oss",
		"oss.enabled":                 "true",
	})
	_, err := UploadPublicAssetToOSS(context.Background(), cfg, file, "prompt-gallery/2026/05/11/cover.png", "image/png")
	if err == nil || !strings.Contains(err.Error(), "oss config incomplete") {
		t.Fatalf("UploadPublicAssetToOSS() error = %v, want incomplete config error", err)
	}
}

func TestPublicAssetOSSObjectKeyDefaultPrefix(t *testing.T) {
	got := ossObjectKeyFromConfig(context.Background(), nil, "prompt-gallery/2026/05/11/cover.png", defaultOSSPublicAssetPrefix)
	if !strings.HasPrefix(got, "uploads/") || !strings.HasSuffix(got, "/cover.png") {
		t.Fatalf("ossObjectKeyFromConfig() = %q, want uploads prefix and cover filename", got)
	}
}

func TestS3CompatibleObjectURLAndAuthorization(t *testing.T) {
	cfg := testSystemConfig(map[string]string{
		"oss.public_base_url": "https://static.example.com",
	})
	key := "uploads/2026/05/11/cover.png"
	gotURL := s3ObjectURL("http://object-storage.local", "dapo-public", key)
	parsed, err := url.Parse(gotURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", gotURL, err)
	}
	if parsed.Path != "/dapo-public/"+key {
		t.Fatalf("path = %q, want path-style S3 object URL", parsed.Path)
	}
	payloadHash := sha256Hex([]byte("cover"))
	gotAuth := s3AuthorizationHeader(parsed, "test-access-key", "test-secret-key", "us-east-1", "image/png", payloadHash, "20260511T120000Z", "20260511")
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=test-access-key/20260511/us-east-1/s3/aws4_request") {
		t.Fatalf("Authorization = %q, want AWS SigV4 credential scope", gotAuth)
	}
	if !strings.Contains(gotAuth, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date") {
		t.Fatalf("Authorization = %q, want signed S3 headers", gotAuth)
	}
	if gotPublicURL := ossPublicURL(context.Background(), cfg, key); gotPublicURL != "https://static.example.com/"+key {
		t.Fatalf("ossPublicURL() = %q, want public asset URL", gotPublicURL)
	}
}
