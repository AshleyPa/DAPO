package service

import (
	"context"
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
