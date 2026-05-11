package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	switch provider {
	case "", defaultOSSProvider, "oss":
		return uploadFileToAliyunOSS(ctx, cfg, filePath, key, contentType, endpoint, bucket, accessKeyID, accessKeySecret)
	case "s3", "minio", "sealos":
		return uploadFileToS3Compatible(ctx, cfg, filePath, key, contentType, endpoint, bucket, accessKeyID, accessKeySecret)
	default:
		return "", fmt.Errorf("unsupported oss provider %s", provider)
	}
}

func uploadFileToAliyunOSS(ctx context.Context, cfg *SystemConfigService, filePath, key, contentType, endpoint, bucket, accessKeyID, accessKeySecret string) (string, error) {
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
	if publicURL := ossPublicURL(ctx, cfg, key); publicURL != "" {
		return publicURL, nil
	}
	return ossObjectURL(endpoint, bucket, key), nil
}

func uploadFileToS3Compatible(ctx context.Context, cfg *SystemConfigService, filePath, key, contentType, endpoint, bucket, accessKeyID, accessKeySecret string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	payloadHash, err := fileSHA256Hex(f)
	if err != nil {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	putURL := s3ObjectURL(endpoint, bucket, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, f)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := strings.TrimSpace(cfg.GetString(ctx, "oss.region", "us-east-1"))
	if region == "" {
		region = "us-east-1"
	}
	req.ContentLength = st.Size()
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("Authorization", s3AuthorizationHeader(req.URL, accessKeyID, accessKeySecret, region, contentType, payloadHash, amzDate, dateStamp))

	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("oss upload HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if publicURL := ossPublicURL(ctx, cfg, key); publicURL != "" {
		return publicURL, nil
	}
	return putURL, nil
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

func ossPublicURL(ctx context.Context, cfg *SystemConfigService, key string) string {
	publicBase := strings.TrimRight(strings.TrimSpace(cfg.GetString(ctx, "oss.public_base_url", "")), "/")
	if publicBase == "" {
		return ""
	}
	return publicBase + "/" + escapePathSegments(key)
}

func s3ObjectURL(endpoint, bucket, key string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint + "/" + url.PathEscape(bucket) + "/" + escapePathSegments(key)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + url.PathEscape(bucket) + "/" + escapePathSegments(key)
	u.RawQuery = ""
	return u.String()
}

func s3AuthorizationHeader(u *url.URL, accessKeyID, accessKeySecret, region, contentType, payloadHash, amzDate, dateStamp string) string {
	canonicalURI := u.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "content-type:" + contentType + "\n" +
		"host:" + u.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	canonicalRequest := strings.Join([]string{
		http.MethodPut,
		canonicalURI,
		"",
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	credentialScope := dateStamp + "/" + region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(s3SigningKey(accessKeySecret, dateStamp, region), []byte(stringToSign)))
	return "AWS4-HMAC-SHA256 Credential=" + accessKeyID + "/" + credentialScope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

func s3SigningKey(secret, dateStamp, region string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileSHA256Hex(f *os.File) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
