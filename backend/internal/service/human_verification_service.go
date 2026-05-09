package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/kleinai/backend/pkg/config"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/logger"
)

const (
	TurnstileActionAuth = "auth"

	turnstileSiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
)

type HumanVerificationService struct {
	cfg    *config.Config
	sys    *SystemConfigService
	client *http.Client
}

type HumanVerificationPublicConfig struct {
	Turnstile TurnstilePublicConfig `json:"turnstile"`
}

type TurnstilePublicConfig struct {
	Enabled bool   `json:"enabled"`
	SiteKey string `json:"site_key"`
}

type turnstileRuntimeConfig struct {
	Enabled          bool
	SiteKey          string
	SecretKey        string
	AllowedHostnames []string
	Timeout          time.Duration
}

type turnstileSiteverifyResp struct {
	Success     bool     `json:"success"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	Action      string   `json:"action"`
	ErrorCodes  []string `json:"error-codes"`
}

func NewHumanVerificationService(cfg *config.Config, sys *SystemConfigService) *HumanVerificationService {
	timeout := 5 * time.Second
	if cfg != nil && cfg.Turnstile.Timeout > 0 {
		timeout = cfg.Turnstile.Timeout
	}
	return &HumanVerificationService{
		cfg:    cfg,
		sys:    sys,
		client: &http.Client{Timeout: timeout},
	}
}

func (s *HumanVerificationService) PublicConfig(ctx context.Context) HumanVerificationPublicConfig {
	cfg := s.effectiveConfig(ctx)
	return HumanVerificationPublicConfig{
		Turnstile: TurnstilePublicConfig{
			Enabled: cfg.Enabled,
			SiteKey: cfg.SiteKey,
		},
	}
}

func (s *HumanVerificationService) VerifyTurnstile(ctx context.Context, token, expectedAction, remoteIP, requestHost string) error {
	cfg := s.effectiveConfig(ctx)
	if !cfg.Enabled {
		return nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errcode.InvalidParam.WithMsg("请先完成人机验证")
	}
	if len(token) > 2048 {
		return errcode.InvalidParam.WithMsg("人机验证失败，请刷新后重试")
	}
	if strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.SiteKey) == "" {
		return errcode.Internal.WithMsg("人机验证配置缺失")
	}

	body := url.Values{}
	body.Set("secret", cfg.SecretKey)
	body.Set("response", token)
	if strings.TrimSpace(remoteIP) != "" {
		body.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileSiteverifyURL, strings.NewReader(body.Encode()))
	if err != nil {
		return errcode.Internal.Wrap(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		logger.FromCtx(ctx).Warn("turnstile.siteverify_failed", zap.String("action", expectedAction), zap.Error(err))
		return errcode.InvalidParam.WithMsg("人机验证失败，请稍后重试")
	}
	defer resp.Body.Close()

	var result turnstileSiteverifyResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.FromCtx(ctx).Warn("turnstile.decode_failed", zap.String("action", expectedAction), zap.Error(err))
		return errcode.InvalidParam.WithMsg("人机验证失败，请刷新后重试")
	}
	if !result.Success {
		logger.FromCtx(ctx).Warn(
			"turnstile.rejected",
			zap.String("action", expectedAction),
			zap.String("hostname", result.Hostname),
			zap.Strings("error_codes", result.ErrorCodes),
		)
		return errcode.InvalidParam.WithMsg("人机验证失败，请刷新后重试")
	}
	if isTurnstileTestingSecret(cfg.SecretKey) {
		return nil
	}
	if expectedAction != "" && result.Action != "" && result.Action != expectedAction {
		logger.FromCtx(ctx).Warn("turnstile.action_mismatch", zap.String("expected", expectedAction), zap.String("actual", result.Action))
		return errcode.InvalidParam.WithMsg("人机验证失败，请刷新后重试")
	}
	if !s.hostnameAllowed(result.Hostname, requestHost, cfg.AllowedHostnames) {
		logger.FromCtx(ctx).Warn("turnstile.hostname_mismatch", zap.String("hostname", result.Hostname), zap.String("request_host", requestHost))
		return errcode.InvalidParam.WithMsg("人机验证失败，请刷新后重试")
	}
	return nil
}

func (s *HumanVerificationService) effectiveConfig(ctx context.Context) turnstileRuntimeConfig {
	cfg := turnstileRuntimeConfig{Timeout: 5 * time.Second}
	if s.cfg != nil {
		cfg.Enabled = s.cfg.Turnstile.Enabled
		cfg.SiteKey = s.cfg.Turnstile.SiteKey
		cfg.SecretKey = s.cfg.Turnstile.SecretKey
		cfg.AllowedHostnames = append([]string(nil), s.cfg.Turnstile.AllowedHostnames...)
		if s.cfg.Turnstile.Timeout > 0 {
			cfg.Timeout = s.cfg.Turnstile.Timeout
		}
	}
	if os.Getenv("KLEIN_TURNSTILE_ENABLED") == "" && s.sys != nil {
		cfg.Enabled = s.sys.GetBool(ctx, SettingTurnstileEnabled, cfg.Enabled)
	}
	if os.Getenv("KLEIN_TURNSTILE_SITE_KEY") == "" && s.sys != nil {
		cfg.SiteKey = s.sys.GetString(ctx, SettingTurnstileSiteKey, cfg.SiteKey)
	}
	if os.Getenv("KLEIN_TURNSTILE_SECRET_KEY") == "" && s.sys != nil {
		cfg.SecretKey = s.sys.GetString(ctx, SettingTurnstileSecretKey, cfg.SecretKey)
	}
	if os.Getenv("KLEIN_TURNSTILE_ALLOWED_HOSTNAMES") == "" && s.sys != nil {
		if raw := s.sys.GetString(ctx, SettingTurnstileHostnames, ""); strings.TrimSpace(raw) != "" {
			cfg.AllowedHostnames = splitCSV(raw)
		}
	}
	return cfg
}

func (s *HumanVerificationService) hostnameAllowed(actual, requestHost string, allowed []string) bool {
	actual = normalizeHostname(actual)
	if actual == "" {
		return false
	}
	normalizedAllowed := make(map[string]struct{}, len(allowed)+1)
	for _, host := range allowed {
		if h := normalizeHostname(host); h != "" {
			normalizedAllowed[h] = struct{}{}
		}
	}
	if len(normalizedAllowed) == 0 {
		if h := normalizeHostname(requestHost); h != "" {
			normalizedAllowed[h] = struct{}{}
		}
	}
	_, ok := normalizedAllowed[actual]
	return ok
}

func normalizeHostname(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return ""
	}
	if u, err := url.Parse(v); err == nil && u.Host != "" {
		v = u.Host
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	return strings.Trim(v, "[]")
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func isTurnstileTestingSecret(secret string) bool {
	switch strings.TrimSpace(secret) {
	case "1x0000000000000000000000000000000AA",
		"2x0000000000000000000000000000000AA",
		"3x0000000000000000000000000000000AA":
		return true
	default:
		return false
	}
}
