package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

const (
	defaultOSSProvider          = "aliyun"
	defaultOSSGeneratedPrefix   = "generated/{yyyy}/{mm}/{dd}"
	defaultOSSPublicAssetPrefix = "uploads/{yyyy}/{mm}/{dd}"
)

// PublicAssetUsesOSS returns true when the shared public asset storage should
// persist uploaded assets to OSS instead of relying only on local container
// storage.
func PublicAssetUsesOSS(ctx context.Context, cfg *SystemConfigService) bool {
	if cfg == nil {
		return false
	}
	driver := strings.ToLower(strings.TrimSpace(cfg.GetString(ctx, "storage.result_cache_driver", "local")))
	return driver == "oss" && cfg.GetBool(ctx, "oss.enabled", false)
}

// UploadCachedAssetToOSS uploads generated cache assets using the historical
// generated-object prefix default. It preserves GenerationService behavior.
func UploadCachedAssetToOSS(ctx context.Context, cfg *SystemConfigService, filePath, rel, contentType string) (string, error) {
	return uploadFileToOSS(ctx, cfg, filePath, rel, contentType, defaultOSSGeneratedPrefix)
}

// UploadPublicAssetToOSS uploads admin-managed public assets such as Prompt
// Gallery covers. It uses the same OSS credentials as generated asset caching,
// while defaulting object keys to the admin-facing uploads prefix.
func UploadPublicAssetToOSS(ctx context.Context, cfg *SystemConfigService, filePath, rel, contentType string) (string, error) {
	return uploadFileToOSS(ctx, cfg, filePath, rel, contentType, defaultOSSPublicAssetPrefix)
}

func uploadFileToOSS(ctx context.Context, cfg *SystemConfigService, filePath, rel, contentType, defaultPrefix string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("missing system config")
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.GetString(ctx, "oss.provider", defaultOSSProvider)))
	if provider != "" && provider != defaultOSSProvider && provider != "oss" {
		return "", fmt.Errorf("unsupported oss provider %s", provider)
	}
	endpoint := strings.TrimSpace(cfg.GetString(ctx, "oss.endpoint", ""))
	bucket := strings.TrimSpace(cfg.GetString(ctx, "oss.bucket", ""))
	accessKeyID := strings.TrimSpace(cfg.GetString(ctx, "oss.access_key_id", ""))
	accessKeySecret := strings.TrimSpace(cfg.GetString(ctx, "oss.access_key_secret", ""))
	if endpoint == "" || bucket == "" || accessKeyID == "" || accessKeySecret == "" {
		return "", fmt.Errorf("oss config incomplete")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	key := ossObjectKeyFromConfig(ctx, cfg, rel, defaultPrefix)
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	date := time.Now().UTC().Format(http.TimeFormat)
	resource := "/" + bucket + "/" + key
	signing := "PUT\n\n" + contentType + "\n" + date + "\n" + resource
	mac := hmac.New(sha1.New, []byte(accessKeySecret))
	_, _ = mac.Write([]byte(signing))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	putURL := ossObjectURL(endpoint, bucket, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, f)
	if err != nil {
		return "", err
	}
	req.ContentLength = st.Size()
	req.Header.Set("Date", date)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "OSS "+accessKeyID+":"+signature)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("oss upload HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	publicBase := strings.TrimRight(strings.TrimSpace(cfg.GetString(ctx, "oss.public_base_url", "")), "/")
	if publicBase != "" {
		return publicBase + "/" + key, nil
	}
	return ossObjectURL(endpoint, bucket, key), nil
}

func ossObjectKeyFromConfig(ctx context.Context, cfg *SystemConfigService, rel, defaultPrefix string) string {
	prefix := defaultPrefix
	if cfg != nil {
		prefix = strings.TrimSpace(cfg.GetString(ctx, "oss.path_prefix", prefix))
	}
	now := time.Now()
	prefix = strings.Trim(prefix, "/")
	prefix = strings.ReplaceAll(prefix, "{yyyy}", now.Format("2006"))
	prefix = strings.ReplaceAll(prefix, "{mm}", now.Format("01"))
	prefix = strings.ReplaceAll(prefix, "{dd}", now.Format("02"))
	if prefix == "" {
		return path.Base(rel)
	}
	return prefix + "/" + path.Base(rel)
}
