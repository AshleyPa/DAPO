package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/pkg/config"
)

const (
	readinessOK    = "ok"
	readinessWarn  = "warn"
	readinessError = "error"
)

type SystemReadinessService struct {
	cfg *config.Config
	sys *SystemConfigService
}

func NewSystemReadinessService(cfg *config.Config, sys *SystemConfigService) *SystemReadinessService {
	return &SystemReadinessService{cfg: cfg, sys: sys}
}

func (s *SystemReadinessService) Check(ctx context.Context) (*dto.AdminSystemReadinessResp, error) {
	checks := make([]dto.AdminSystemReadinessCheck, 0, 24)
	checks = append(checks, s.runtimeChecks()...)
	checks = append(checks, s.smtpChecks()...)
	checks = append(checks, s.paymentChecks(ctx)...)
	checks = append(checks, s.providerRouteChecks(ctx)...)
	checks = append(checks, s.storageChecks(ctx)...)

	summary := dto.AdminSystemReadinessSummary{}
	overall := readinessOK
	for _, check := range checks {
		switch check.Status {
		case readinessError:
			summary.Error++
			overall = readinessError
		case readinessWarn:
			summary.Warn++
			if overall != readinessError {
				overall = readinessWarn
			}
		default:
			summary.OK++
		}
	}

	return &dto.AdminSystemReadinessResp{
		RefreshedAt: time.Now().Unix(),
		Overall:     overall,
		Summary:     summary,
		Checks:      checks,
	}, nil
}

func (s *SystemReadinessService) runtimeChecks() []dto.AdminSystemReadinessCheck {
	cfg := s.cfg
	env := ""
	if cfg != nil {
		env = cfg.App.Env
	}
	checks := []dto.AdminSystemReadinessCheck{
		requiredCheck("runtime", "mysql", "MySQL DSN", cfg != nil && strings.TrimSpace(cfg.MySQL.DSN) != "", "已配置数据库连接", "缺少 KLEIN_DB_DSN，服务无法连接数据库", "env"),
		requiredCheck("runtime", "redis", "Redis 地址", cfg != nil && strings.TrimSpace(cfg.Redis.Addr) != "", "已配置 Redis 地址", "缺少 KLEIN_REDIS_ADDR，限流和缓存能力不可用", "env"),
		requiredCheck("runtime", "jwt_secret", "JWT 密钥", cfg != nil && len(cfg.JWT.Secret) >= 32 && len(cfg.JWT.RefreshSecret) >= 32, "JWT 密钥长度满足要求", "JWT/Refresh 密钥长度不足 32 字节", "env"),
		requiredCheck("runtime", "aes_key", "凭证加密密钥", cfg != nil && len(cfg.AESKey) >= 32, "AES 密钥长度满足要求", "KLEIN_AES_KEY 长度不足 32 字节，账号池凭证不可安全加密", "env"),
	}
	if env == "prod" {
		checks = append(checks, okCheck("runtime", "environment", "运行环境", "当前为生产环境，基础强校验已启用", "env", true))
	} else {
		checks = append(checks, warnCheck("runtime", "environment", "运行环境", fmt.Sprintf("当前环境为 %q；上线前请确认 Sealos 使用 KLEIN_ENV=prod", env), "env", false))
	}
	return checks
}

func (s *SystemReadinessService) smtpChecks() []dto.AdminSystemReadinessCheck {
	cfg := config.SMTP{}
	if s.cfg != nil {
		cfg = s.cfg.SMTP
	}
	checks := []dto.AdminSystemReadinessCheck{
		requiredCheck("smtp", "host", "SMTP Host", strings.TrimSpace(cfg.Host) != "", "已配置 SMTP Host", "缺少 KLEIN_SMTP_HOST，无法发送注册/找回密码验证码", smtpSource("KLEIN_SMTP_HOST")),
		requiredCheck("smtp", "port", "SMTP Port", cfg.Port > 0, "已配置 SMTP Port", "缺少或错误的 KLEIN_SMTP_PORT", smtpSource("KLEIN_SMTP_PORT")),
		requiredCheck("smtp", "username", "发件账号", strings.TrimSpace(cfg.Username) != "", "已配置发件账号", "缺少 KLEIN_SMTP_USERNAME", smtpSource("KLEIN_SMTP_USERNAME")),
		requiredCheck("smtp", "password", "邮箱三方密码", strings.TrimSpace(cfg.Password) != "", "已配置邮箱三方密码", "缺少 KLEIN_SMTP_PASSWORD，验证码邮件会发送失败", smtpSource("KLEIN_SMTP_PASSWORD")),
		requiredCheck("smtp", "from_email", "发件邮箱", strings.TrimSpace(cfg.FromEmail) != "", "已配置发件邮箱", "缺少 KLEIN_SMTP_FROM_EMAIL", smtpSource("KLEIN_SMTP_FROM_EMAIL")),
	}
	if strings.TrimSpace(cfg.FromName) == "" {
		checks = append(checks, warnCheck("smtp", "from_name", "发件名称", "未配置 KLEIN_SMTP_FROM_NAME，邮件发件展示不完整", smtpSource("KLEIN_SMTP_FROM_NAME"), false))
	} else {
		checks = append(checks, okCheck("smtp", "from_name", "发件名称", "已配置发件名称", smtpSource("KLEIN_SMTP_FROM_NAME"), false))
	}
	return checks
}

func (s *SystemReadinessService) paymentChecks(ctx context.Context) []dto.AdminSystemReadinessCheck {
	enabled, enabledSource := s.effectiveBool(ctx, "KLEIN_PAYMENT_ENABLED", SettingPaymentEnabled, false)
	checks := []dto.AdminSystemReadinessCheck{
		boolCheck("payment", "enabled", "在线支付开关", enabled, "在线支付已启用", "在线支付未启用；用户只能使用 CDK/人工充值等兜底方式", enabledSource, false),
	}
	provider, providerSource := s.effectiveString(ctx, "KLEIN_PAYMENT_PROVIDER", SettingPaymentProvider, "alipay")
	if strings.EqualFold(provider, "alipay") {
		checks = append(checks, okCheck("payment", "provider", "支付通道", "当前支付通道为支付宝当面付", providerSource, false))
	} else {
		checks = append(checks, warnCheck("payment", "provider", "支付通道", fmt.Sprintf("当前支付通道为 %q；DAPO V2 当前只完成支付宝当面付链路", provider), providerSource, false))
	}
	if !enabled {
		return checks
	}
	required := []struct {
		key       string
		label     string
		env       string
		configKey string
	}{
		{"notify_url", "支付回调地址", "KLEIN_PAYMENT_NOTIFY_URL", SettingPaymentNotifyURL},
		{"alipay_app_id", "支付宝 AppID", "KLEIN_ALIPAY_APP_ID", SettingAlipayAppID},
		{"alipay_seller_id", "支付宝 Seller ID", "KLEIN_ALIPAY_SELLER_ID", SettingAlipaySellerID},
		{"alipay_private_key", "支付宝应用私钥", "KLEIN_ALIPAY_PRIVATE_KEY", SettingAlipayPrivateKey},
		{"alipay_public_key", "支付宝公钥", "KLEIN_ALIPAY_PUBLIC_KEY", SettingAlipayPublicKey},
	}
	for _, item := range required {
		value, source := s.effectiveString(ctx, item.env, item.configKey, "")
		checks = append(checks, requiredCheck("payment", item.key, item.label, strings.TrimSpace(value) != "", "已配置", "在线支付已启用，但该配置缺失", source))
	}
	return checks
}

func (s *SystemReadinessService) providerRouteChecks(ctx context.Context) []dto.AdminSystemReadinessCheck {
	raw := ""
	if s.sys != nil {
		raw = s.sys.GetString(ctx, SettingProviderRoutes, "")
	}
	if strings.TrimSpace(raw) == "" {
		return []dto.AdminSystemReadinessCheck{
			warnCheck("provider_routes", "configured", "Provider 路由配置", "provider.routes 未配置，将退回代码默认账号池选择", "system_config", false),
		}
	}
	var anyRules []any
	if err := json.Unmarshal([]byte(raw), &anyRules); err != nil {
		return []dto.AdminSystemReadinessCheck{
			errorCheck("provider_routes", "valid", "Provider 路由配置", "provider.routes 不是合法 JSON 数组", "system_config", true),
		}
	}
	rules, err := NormalizeProviderRouteRulesConfig(anyRules)
	if err != nil {
		return []dto.AdminSystemReadinessCheck{
			errorCheck("provider_routes", "valid", "Provider 路由配置", err.Error(), "system_config", true),
		}
	}
	enabledRules, enabledRoutes := 0, 0
	kinds := map[string]bool{}
	for _, rule := range rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		enabledRules++
		kinds[rule.Kind] = true
		for _, route := range rule.Routes {
			if route.Enabled == nil || *route.Enabled {
				enabledRoutes++
			}
		}
	}
	checks := []dto.AdminSystemReadinessCheck{
		requiredCheck("provider_routes", "valid", "Provider 路由配置", enabledRules > 0 && enabledRoutes > 0, fmt.Sprintf("已配置 %d 条启用规则 / %d 条启用路线", enabledRules, enabledRoutes), "provider.routes 没有启用规则或启用路线", "system_config"),
	}
	for _, kind := range []string{"image", "text", "video"} {
		if kinds[kind] || (kind == "text" && kinds["chat"]) || kinds["*"] {
			checks = append(checks, okCheck("provider_routes", "kind_"+kind, routeKindLabel(kind), "已覆盖该入口", "system_config", false))
		} else {
			checks = append(checks, warnCheck("provider_routes", "kind_"+kind, routeKindLabel(kind), "尚未配置该入口路由，可能退回默认账号池", "system_config", false))
		}
	}
	return checks
}

func (s *SystemReadinessService) storageChecks(ctx context.Context) []dto.AdminSystemReadinessCheck {
	ossEnabled := false
	if s.sys != nil {
		ossEnabled = s.sys.GetBool(ctx, "oss.enabled", false)
	}
	checks := []dto.AdminSystemReadinessCheck{
		boolCheck("storage", "oss_enabled", "OSS 存储", ossEnabled, "OSS 已启用", "OSS 未启用，生成结果会优先依赖本地缓存/运行环境存储", "system_config", false),
	}
	if !ossEnabled {
		return checks
	}
	required := []struct {
		key       string
		label     string
		configKey string
	}{
		{"endpoint", "OSS Endpoint", "oss.endpoint"},
		{"bucket", "OSS Bucket", "oss.bucket"},
		{"access_key_id", "OSS AccessKey ID", "oss.access_key_id"},
		{"access_key_secret", "OSS AccessKey Secret", "oss.access_key_secret"},
		{"public_base_url", "OSS 公开访问域名", "oss.public_base_url"},
	}
	for _, item := range required {
		value := ""
		if s.sys != nil {
			value = s.sys.GetString(ctx, item.configKey, "")
		}
		checks = append(checks, requiredCheck("storage", item.key, item.label, strings.TrimSpace(value) != "", "已配置", "OSS 已启用，但该配置缺失", "system_config"))
	}
	return checks
}

func (s *SystemReadinessService) effectiveString(ctx context.Context, envKey, configKey, fallback string) (string, string) {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return value, "env"
	}
	if s.sys != nil {
		if value := strings.TrimSpace(s.sys.GetString(ctx, configKey, "")); value != "" {
			return value, "system_config"
		}
	}
	return fallback, missingSource(fallback)
}

func (s *SystemReadinessService) effectiveBool(ctx context.Context, envKey, configKey string, fallback bool) (bool, string) {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return envBool(value), "env"
	}
	if s.sys != nil {
		return s.sys.GetBool(ctx, configKey, fallback), "system_config"
	}
	return fallback, "missing"
}

func requiredCheck(category, key, label string, ok bool, okMsg, errMsg, source string) dto.AdminSystemReadinessCheck {
	if ok {
		return okCheck(category, key, label, okMsg, source, true)
	}
	return errorCheck(category, key, label, errMsg, source, true)
}

func boolCheck(category, key, label string, enabled bool, okMsg, warnMsg, source string, required bool) dto.AdminSystemReadinessCheck {
	if enabled {
		return okCheck(category, key, label, okMsg, source, required)
	}
	return warnCheck(category, key, label, warnMsg, source, required)
}

func okCheck(category, key, label, message, source string, required bool) dto.AdminSystemReadinessCheck {
	return dto.AdminSystemReadinessCheck{Category: category, Key: key, Label: label, Status: readinessOK, Message: message, Source: source, Required: required}
}

func warnCheck(category, key, label, message, source string, required bool) dto.AdminSystemReadinessCheck {
	return dto.AdminSystemReadinessCheck{Category: category, Key: key, Label: label, Status: readinessWarn, Message: message, Source: source, Required: required}
}

func errorCheck(category, key, label, message, source string, required bool) dto.AdminSystemReadinessCheck {
	return dto.AdminSystemReadinessCheck{Category: category, Key: key, Label: label, Status: readinessError, Message: message, Source: source, Required: required}
}

func smtpSource(envKey string) string {
	if strings.TrimSpace(os.Getenv(envKey)) != "" {
		return "env"
	}
	return "config"
}

func missingSource(fallback string) string {
	if strings.TrimSpace(fallback) == "" {
		return "missing"
	}
	return "default"
}

func routeKindLabel(kind string) string {
	switch kind {
	case "image":
		return "图片入口路由"
	case "text":
		return "文字入口路由"
	case "video":
		return "视频入口路由"
	default:
		return kind
	}
}
