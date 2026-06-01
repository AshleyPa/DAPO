package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/config"
)

const (
	readinessOK    = "ok"
	readinessWarn  = "warn"
	readinessError = "error"
)

type SystemReadinessService struct {
	cfg        *config.Config
	sys        *SystemConfigService
	modelRepo  readinessModelCatalogLister
	sourceRepo readinessModelSourceLister
	apiRepo    apiChannelOperationalRepo
}

type readinessModelCatalogLister interface {
	List(ctx context.Context, f repo.ModelCatalogListFilter) ([]*model.ModelCatalog, int64, error)
}

type readinessModelSourceLister interface {
	List(ctx context.Context, f repo.ModelSourceListFilter) ([]*model.ModelSourceMapping, int64, error)
}

type SystemReadinessModelGatewayDeps struct {
	ModelRepo  readinessModelCatalogLister
	SourceRepo readinessModelSourceLister
	APIRepo    apiChannelOperationalRepo
}

func NewSystemReadinessService(cfg *config.Config, sys *SystemConfigService, deps ...SystemReadinessModelGatewayDeps) *SystemReadinessService {
	s := &SystemReadinessService{cfg: cfg, sys: sys}
	if len(deps) > 0 {
		s.modelRepo = deps[0].ModelRepo
		s.sourceRepo = deps[0].SourceRepo
		s.apiRepo = deps[0].APIRepo
	}
	return s
}

func (s *SystemReadinessService) Check(ctx context.Context) (*dto.AdminSystemReadinessResp, error) {
	checks := make([]dto.AdminSystemReadinessCheck, 0, 24)
	checks = append(checks, s.runtimeChecks()...)
	checks = append(checks, s.smtpChecks(ctx)...)
	checks = append(checks, s.humanVerificationChecks(ctx)...)
	checks = append(checks, s.paymentChecks(ctx)...)
	modelGatewayChecks, modelGatewayManaged := s.modelGatewayChecks(ctx)
	checks = append(checks, modelGatewayChecks...)
	checks = append(checks, s.providerRouteChecks(ctx, modelGatewayManaged)...)
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

func (s *SystemReadinessService) smtpChecks(ctx context.Context) []dto.AdminSystemReadinessCheck {
	cfg := config.SMTP{}
	if s.cfg != nil {
		cfg = s.cfg.SMTP
	}
	host, hostSource := s.effectiveSMTPString(ctx, "KLEIN_SMTP_HOST", SettingSMTPHost, cfg.Host)
	port, portSource := s.effectiveSMTPInt(ctx, "KLEIN_SMTP_PORT", SettingSMTPPort, int64(cfg.Port))
	username, usernameSource := s.effectiveSMTPString(ctx, "KLEIN_SMTP_USERNAME", SettingSMTPUsername, cfg.Username)
	password, passwordSource := s.effectiveSMTPString(ctx, "KLEIN_SMTP_PASSWORD", SettingSMTPPassword, cfg.Password)
	fromEmail, fromEmailSource := s.effectiveSMTPString(ctx, "KLEIN_SMTP_FROM_EMAIL", SettingSMTPFromEmail, cfg.FromEmail)
	fromName, fromNameSource := s.effectiveSMTPString(ctx, "KLEIN_SMTP_FROM_NAME", SettingSMTPFromName, cfg.FromName)
	checks := []dto.AdminSystemReadinessCheck{
		requiredCheck("smtp", "host", "SMTP Host", strings.TrimSpace(host) != "", "已配置 SMTP Host", "缺少 KLEIN_SMTP_HOST 或 smtp.host，无法发送注册/找回密码验证码", hostSource),
		requiredCheck("smtp", "port", "SMTP Port", port > 0, "已配置 SMTP Port", "缺少或错误的 KLEIN_SMTP_PORT / smtp.port", portSource),
		requiredCheck("smtp", "username", "发件账号", strings.TrimSpace(username) != "", "已配置发件账号", "缺少 KLEIN_SMTP_USERNAME 或 smtp.username", usernameSource),
		requiredCheck("smtp", "password", "邮箱三方密码", strings.TrimSpace(password) != "", "已配置邮箱三方密码", "缺少 KLEIN_SMTP_PASSWORD 或 smtp.password，验证码邮件会发送失败", passwordSource),
		requiredCheck("smtp", "from_email", "发件邮箱", strings.TrimSpace(fromEmail) != "", "已配置发件邮箱", "缺少 KLEIN_SMTP_FROM_EMAIL 或 smtp.from_email", fromEmailSource),
	}
	if strings.TrimSpace(fromName) == "" {
		checks = append(checks, warnCheck("smtp", "from_name", "发件名称", "未配置 KLEIN_SMTP_FROM_NAME 或 smtp.from_name，邮件发件展示不完整", fromNameSource, false))
	} else {
		checks = append(checks, okCheck("smtp", "from_name", "发件名称", "已配置发件名称", fromNameSource, false))
	}
	return checks
}

func (s *SystemReadinessService) humanVerificationChecks(ctx context.Context) []dto.AdminSystemReadinessCheck {
	cfg := config.Turnstile{}
	if s.cfg != nil {
		cfg = s.cfg.Turnstile
	}
	enabled, enabledSource := s.effectiveBool(ctx, "KLEIN_TURNSTILE_ENABLED", SettingTurnstileEnabled, cfg.Enabled)
	checks := []dto.AdminSystemReadinessCheck{
		boolCheck("human_verification", "turnstile_enabled", "Cloudflare Turnstile", enabled, "登录、注册和邮箱验证码已启用人机验证", "未启用人机验证，登录注册仍仅依赖限流保护", enabledSource, false),
	}
	if !enabled {
		return checks
	}
	siteKey, siteKeySource := s.effectiveString(ctx, "KLEIN_TURNSTILE_SITE_KEY", SettingTurnstileSiteKey, cfg.SiteKey)
	secretKey, secretKeySource := s.effectiveString(ctx, "KLEIN_TURNSTILE_SECRET_KEY", SettingTurnstileSecretKey, cfg.SecretKey)
	hostnames := s.effectiveTurnstileHostnames(ctx, cfg.AllowedHostnames)
	checks = append(checks,
		requiredCheck("human_verification", "turnstile_site_key", "Turnstile Site Key", strings.TrimSpace(siteKey) != "", "已配置前端站点 key", "人机验证已启用，但缺少 Turnstile Site Key", siteKeySource),
		requiredCheck("human_verification", "turnstile_secret_key", "Turnstile Secret Key", strings.TrimSpace(secretKey) != "", "已配置服务端校验密钥", "人机验证已启用，但缺少 Turnstile Secret Key", secretKeySource),
	)
	if len(hostnames) == 0 {
		checks = append(checks, warnCheck("human_verification", "turnstile_hostnames", "Turnstile 域名白名单", "未显式配置域名白名单，将使用请求 Host 做校验", "request_host", false))
	} else {
		checks = append(checks, okCheck("human_verification", "turnstile_hostnames", "Turnstile 域名白名单", fmt.Sprintf("已配置 %d 个允许域名", len(hostnames)), "system_config_or_env", false))
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

func (s *SystemReadinessService) modelGatewayChecks(ctx context.Context) ([]dto.AdminSystemReadinessCheck, bool) {
	if s.modelRepo == nil || s.sourceRepo == nil {
		return nil, false
	}
	statusEnabled := int8(model.ModelCatalogStatusEnabled)
	visibleEnabled := int8(1)
	models, modelTotal, err := s.listReadinessCatalogModels(ctx, repo.ModelCatalogListFilter{
		Status:  &statusEnabled,
		Visible: &visibleEnabled,
	})
	if err != nil {
		return []dto.AdminSystemReadinessCheck{
			errorCheck("model_gateway", "catalog_query", "模型库", fmt.Sprintf("读取模型库失败: %v", err), "model_catalog", false),
		}, false
	}
	sources, sourceTotal, err := s.listReadinessSources(ctx, repo.ModelSourceListFilter{Status: &statusEnabled})
	if err != nil {
		return []dto.AdminSystemReadinessCheck{
			errorCheck("model_gateway", "source_query", "模型来源映射", fmt.Sprintf("读取模型来源映射失败: %v", err), "model_source_mapping", false),
		}, false
	}
	allSources, _, allSourceErr := s.listReadinessSources(ctx, repo.ModelSourceListFilter{})
	if allSourceErr != nil {
		allSources = sources
	}

	sourceByModel := map[string]int{}
	apiChannelSourceCodes := map[string]struct{}{}
	apiChannelSources := 0
	for _, source := range sources {
		if source == nil {
			continue
		}
		sourceByModel[source.ModelCode]++
		if source.SourceType == model.ModelSourceTypeAPIChannel {
			apiChannelSources++
			if strings.TrimSpace(source.SourceCode) != "" {
				apiChannelSourceCodes[strings.TrimSpace(source.SourceCode)] = struct{}{}
			}
		}
	}
	modelsByKind := map[string]int{}
	coveredByKind := map[string]int{}
	modelsByCode := map[string]*model.ModelCatalog{}
	for _, item := range models {
		if item == nil {
			continue
		}
		modelsByCode[item.ModelCode] = item
		kind := readinessModelGatewayKind(item.EntryKind)
		if kind == "" {
			continue
		}
		modelsByKind[kind]++
		if sourceByModel[item.ModelCode] > 0 {
			coveredByKind[kind]++
		}
	}

	checks := []dto.AdminSystemReadinessCheck{
		boolCheck(
			"model_gateway",
			"catalog",
			"模型库",
			modelTotal > 0,
			fmt.Sprintf("已配置 %d 个启用可见模型", modelTotal),
			"模型库暂无启用可见模型；前台/兼容模型列表会回退旧配置或默认模型",
			"model_catalog",
			false,
		),
		boolCheck(
			"model_gateway",
			"sources",
			"模型来源映射",
			sourceTotal > 0,
			fmt.Sprintf("已配置 %d 条启用来源映射", sourceTotal),
			"模型来源映射为空；已接入模型无法按 Model Gateway 选择 API 渠道或账号池",
			"model_source_mapping",
			false,
		),
		boolCheck(
			"model_gateway",
			"api_channel_sources",
			"API 渠道来源",
			apiChannelSources > 0,
			fmt.Sprintf("已配置 %d 条 API Channel 来源映射", apiChannelSources),
			"尚未配置 API Channel 来源；MiMo / DeepSeek 等官方 API 模型不会走独立 API 渠道",
			"model_source_mapping",
			false,
		),
	}
	sourceConflicts := readinessAccountPoolSourceConflicts(modelsByCode, sources)
	checks = append(checks, boolCheck(
		"model_gateway",
		"source_conflicts",
		"模型来源错配",
		len(sourceConflicts) == 0,
		"未发现启用模型误挂账号池来源",
		fmt.Sprintf("发现 %d 条来源错配：%s", len(sourceConflicts), readinessSourceConflictSummary(sourceConflicts)),
		"model_source_mapping",
		false,
	))
	if allSourceErr != nil {
		checks = append(checks, errorCheck("model_gateway", "source_duplicate_query", "重复来源映射", fmt.Sprintf("读取全量来源映射失败: %v", allSourceErr), "model_source_mapping", false))
	} else {
		sourceDuplicates := readinessDuplicateModelSources(modelsByCode, allSources)
		checks = append(checks, boolCheck(
			"model_gateway",
			"source_duplicates",
			"重复来源映射",
			len(sourceDuplicates) == 0,
			"未发现重复来源映射",
			fmt.Sprintf("发现 %d 组重复来源映射：%s", len(sourceDuplicates), readinessSourceDuplicateSummary(sourceDuplicates)),
			"model_source_mapping",
			false,
		))
	}
	checks = append(checks, s.apiChannelOperationalChecks(ctx, apiChannelSources, apiChannelSourceCodes)...)
	for _, kind := range []string{"image", "text", "video"} {
		modelCount := modelsByKind[kind]
		coveredCount := coveredByKind[kind]
		label := routeKindLabel(kind)
		switch {
		case modelCount == 0:
			checks = append(checks, warnCheck("model_gateway", "kind_"+kind, label, "模型库尚无启用可见模型", "model_catalog", false))
		case coveredCount == modelCount:
			checks = append(checks, okCheck("model_gateway", "kind_"+kind, label, fmt.Sprintf("已覆盖 %d/%d 个启用模型", coveredCount, modelCount), "model_source_mapping", false))
		default:
			checks = append(checks, warnCheck("model_gateway", "kind_"+kind, label, fmt.Sprintf("仅覆盖 %d/%d 个启用模型，未覆盖模型会走兼容兜底或失败", coveredCount, modelCount), "model_source_mapping", false))
		}
	}
	return checks, modelTotal > 0 && sourceTotal > 0
}

type readinessSourceConflict struct {
	ModelCode  string
	SourceCode string
	Reason     string
}

type readinessSourceDuplicate struct {
	ModelCode     string
	SourceType    string
	SourceCode    string
	UpstreamModel string
	FirstID       uint64
	DuplicateID   uint64
}

func readinessAccountPoolSourceConflicts(modelsByCode map[string]*model.ModelCatalog, sources []*model.ModelSourceMapping) []readinessSourceConflict {
	out := []readinessSourceConflict{}
	for _, source := range sources {
		if source == nil || source.SourceType != model.ModelSourceTypeAccountPool {
			continue
		}
		item := modelsByCode[strings.TrimSpace(source.ModelCode)]
		if item == nil {
			continue
		}
		upstreamModel := strings.TrimSpace(source.UpstreamModel)
		if upstreamModel == "" {
			upstreamModel = strings.TrimSpace(item.UpstreamDefaultModel)
		}
		if upstreamModel == "" {
			upstreamModel = item.ModelCode
		}
		if reason := accountPoolSourceMismatchReason(item, strings.TrimSpace(source.SourceCode), upstreamModel); reason != "" {
			out = append(out, readinessSourceConflict{
				ModelCode:  item.ModelCode,
				SourceCode: strings.TrimSpace(source.SourceCode),
				Reason:     reason,
			})
		}
	}
	return out
}

func readinessSourceConflictSummary(conflicts []readinessSourceConflict) string {
	if len(conflicts) == 0 {
		return ""
	}
	parts := make([]string, 0, min(len(conflicts), 3))
	for idx, item := range conflicts {
		if idx >= 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s -> %s（%s）", item.ModelCode, item.SourceCode, item.Reason))
	}
	if len(conflicts) > len(parts) {
		parts = append(parts, fmt.Sprintf("另有 %d 条", len(conflicts)-len(parts)))
	}
	return strings.Join(parts, "；")
}

func readinessDuplicateModelSources(modelsByCode map[string]*model.ModelCatalog, sources []*model.ModelSourceMapping) []readinessSourceDuplicate {
	seen := map[modelSourceRouteSignature]*model.ModelSourceMapping{}
	out := []readinessSourceDuplicate{}
	for _, source := range sources {
		if source == nil {
			continue
		}
		modelCode := strings.TrimSpace(source.ModelCode)
		item := modelsByCode[modelCode]
		if item == nil {
			continue
		}
		sig := modelSourceSignature(item, modelCode, source.SourceType, source.SourceCode, source.UpstreamModel, source.Adapter, source.AuthType, source.ImageAPIMode)
		if previous := seen[sig]; previous != nil {
			out = append(out, readinessSourceDuplicate{
				ModelCode:     item.ModelCode,
				SourceType:    strings.TrimSpace(source.SourceType),
				SourceCode:    strings.TrimSpace(source.SourceCode),
				UpstreamModel: effectiveModelSourceUpstream(item, source.UpstreamModel),
				FirstID:       previous.ID,
				DuplicateID:   source.ID,
			})
			continue
		}
		seen[sig] = source
	}
	return out
}

func readinessSourceDuplicateSummary(duplicates []readinessSourceDuplicate) string {
	if len(duplicates) == 0 {
		return ""
	}
	parts := make([]string, 0, min(len(duplicates), 3))
	for idx, item := range duplicates {
		if idx >= 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s -> %s/%s/%s（ID %d 与 %d）", item.ModelCode, item.SourceType, item.SourceCode, item.UpstreamModel, item.FirstID, item.DuplicateID))
	}
	if len(duplicates) > len(parts) {
		parts = append(parts, fmt.Sprintf("另有 %d 组", len(duplicates)-len(parts)))
	}
	return strings.Join(parts, "；")
}

func (s *SystemReadinessService) apiChannelOperationalChecks(ctx context.Context, sourceRows int, sourceCodes map[string]struct{}) []dto.AdminSystemReadinessCheck {
	if sourceRows <= 0 {
		return nil
	}
	if s.apiRepo == nil {
		return []dto.AdminSystemReadinessCheck{
			warnCheck("model_gateway", "api_channel_health", "API 渠道健康", "体检未注入 API Channel 仓储，无法校验映射渠道健康状态", "api_channel", false),
			warnCheck("model_gateway", "api_channel_credentials", "API 渠道凭证", "体检未注入 API Channel 仓储，无法校验映射渠道 API 凭证", "api_channel", false),
			warnCheck("model_gateway", "api_channel_key_pool", "API Key Pool", "体检未注入 API Channel 仓储，无法校验映射渠道是否已迁移到 Key Pool", "api_channel", false),
		}
	}
	mapped := len(sourceCodes)
	if mapped == 0 {
		return []dto.AdminSystemReadinessCheck{
			warnCheck("model_gateway", "api_channel_health", "API 渠道健康", "API Channel 来源映射缺少来源编码", "model_source_mapping", false),
			warnCheck("model_gateway", "api_channel_credentials", "API 渠道凭证", "API Channel 来源映射缺少来源编码，无法校验 API 凭证", "model_source_mapping", false),
			warnCheck("model_gateway", "api_channel_key_pool", "API Key Pool", "API Channel 来源映射缺少来源编码，无法校验 Key Pool 迁移状态", "model_source_mapping", false),
		}
	}
	healthOK := 0
	credentialOK := 0
	keyPoolOK := 0
	missing := 0
	disabled := 0
	unhealthy := 0
	credentialless := 0
	keyPoolMissing := 0
	legacyCredentials := 0
	queryErrors := 0
	credentialQueryErrors := 0
	keyPoolQueryErrors := 0
	for code := range sourceCodes {
		state := inspectAPIChannelOperational(ctx, s.apiRepo, code)
		if state.QueryErr != nil {
			queryErrors++
			continue
		}
		if !state.Exists {
			missing++
			continue
		}
		if !state.Enabled {
			disabled++
		}
		if state.HealthOK {
			healthOK++
		} else {
			unhealthy++
		}
		if state.KeyQueryErr != nil && !state.LegacyKey {
			credentialQueryErrors++
		}
		if state.KeyQueryErr != nil {
			keyPoolQueryErrors++
		}
		if state.UsableCredentials() > 0 {
			credentialOK++
		} else {
			credentialless++
		}
		if state.LegacyKey {
			legacyCredentials++
		}
		if state.KeyQueryErr == nil && state.UsableKeys > 0 && !state.LegacyKey {
			keyPoolOK++
		} else if state.KeyQueryErr == nil && state.UsableKeys <= 0 {
			keyPoolMissing++
		}
	}
	return []dto.AdminSystemReadinessCheck{
		boolCheck(
			"model_gateway",
			"api_channel_health",
			"API 渠道健康",
			healthOK == mapped,
			fmt.Sprintf("API Channel 来源 %d/%d 最近健康检测 OK", healthOK, mapped),
			fmt.Sprintf("API Channel 来源健康未闭环：OK %d/%d，不存在 %d，停用 %d，未测/失败 %d，查询失败 %d", healthOK, mapped, missing, disabled, unhealthy, queryErrors),
			"api_channel",
			false,
		),
		boolCheck(
			"model_gateway",
			"api_channel_credentials",
			"API 渠道凭证",
			credentialOK == mapped,
			fmt.Sprintf("API Channel 来源 %d/%d 有可用 API 凭证", credentialOK, mapped),
			fmt.Sprintf("API Channel 来源凭证未闭环：OK %d/%d，无可用凭证 %d，凭证查询失败 %d，渠道不存在/查询失败 %d", credentialOK, mapped, credentialless, credentialQueryErrors, missing+queryErrors),
			"api_channel",
			false,
		),
		boolCheck(
			"model_gateway",
			"api_channel_key_pool",
			"API Key Pool",
			keyPoolOK == mapped,
			fmt.Sprintf("API Channel 来源 %d/%d 已使用 Key Pool 且无旧 channel 级 Key", keyPoolOK, mapped),
			fmt.Sprintf("API Channel Key Pool 未闭环：OK %d/%d，无可用 Key Pool %d，仍有旧 channel 级 Key %d，Key Pool 查询失败 %d，渠道不存在/查询失败 %d", keyPoolOK, mapped, keyPoolMissing, legacyCredentials, keyPoolQueryErrors, missing+queryErrors),
			"api_channel_key",
			false,
		),
	}
}

func (s *SystemReadinessService) listReadinessCatalogModels(ctx context.Context, filter repo.ModelCatalogListFilter) ([]*model.ModelCatalog, int64, error) {
	page := 1
	var all []*model.ModelCatalog
	var total int64
	for {
		filter.Page = page
		filter.PageSize = 200
		items, gotTotal, err := s.modelRepo.List(ctx, filter)
		if err != nil {
			return nil, 0, err
		}
		if page == 1 {
			total = gotTotal
		}
		all = append(all, items...)
		if len(items) == 0 || int64(len(all)) >= total {
			return all, total, nil
		}
		page++
	}
}

func (s *SystemReadinessService) listReadinessSources(ctx context.Context, filter repo.ModelSourceListFilter) ([]*model.ModelSourceMapping, int64, error) {
	page := 1
	var all []*model.ModelSourceMapping
	var total int64
	for {
		filter.Page = page
		filter.PageSize = 500
		items, gotTotal, err := s.sourceRepo.List(ctx, filter)
		if err != nil {
			return nil, 0, err
		}
		if page == 1 {
			total = gotTotal
		}
		all = append(all, items...)
		if len(items) == 0 || int64(len(all)) >= total {
			return all, total, nil
		}
		page++
	}
}

func readinessModelGatewayKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "chat", "text":
		return "text"
	case "image":
		return "image"
	case "video":
		return "video"
	default:
		return ""
	}
}

func (s *SystemReadinessService) providerRouteChecks(ctx context.Context, modelGatewayManaged bool) []dto.AdminSystemReadinessCheck {
	raw := ""
	if s.sys != nil {
		raw = s.sys.GetString(ctx, SettingProviderRoutes, "")
	}
	if strings.TrimSpace(raw) == "" {
		if modelGatewayManaged {
			return []dto.AdminSystemReadinessCheck{
				okCheck("provider_routes", "configured", "账号池兼容路由", "Model Gateway 已配置模型和来源映射；provider.routes 为空仅影响未接管模型的旧账号池兜底", "system_config", false),
			}
		}
		return []dto.AdminSystemReadinessCheck{
			warnCheck("provider_routes", "configured", "账号池兼容路由", "Model Gateway 尚未接管且 provider.routes 未配置，将退回代码默认账号池选择", "system_config", false),
		}
	}
	var anyRules []any
	if err := json.Unmarshal([]byte(raw), &anyRules); err != nil {
		return []dto.AdminSystemReadinessCheck{
			errorCheck("provider_routes", "valid", "账号池兼容路由", "provider.routes 不是合法 JSON 数组", "system_config", true),
		}
	}
	rules, err := NormalizeProviderRouteRulesConfig(anyRules)
	if err != nil {
		return []dto.AdminSystemReadinessCheck{
			errorCheck("provider_routes", "valid", "账号池兼容路由", err.Error(), "system_config", true),
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
		requiredCheck("provider_routes", "valid", "账号池兼容路由", enabledRules > 0 && enabledRoutes > 0, fmt.Sprintf("已配置 %d 条启用规则 / %d 条启用路线，仅作为旧账号池兜底", enabledRules, enabledRoutes), "provider.routes 没有启用规则或启用路线", "system_config"),
	}
	for _, kind := range []string{"image", "text", "video"} {
		if kinds[kind] || (kind == "text" && kinds["chat"]) || kinds["*"] {
			checks = append(checks, okCheck("provider_routes", "kind_"+kind, routeKindLabel(kind), "账号池兼容路由已覆盖该入口", "system_config", false))
		} else {
			checks = append(checks, warnCheck("provider_routes", "kind_"+kind, routeKindLabel(kind), "账号池兼容路由尚未覆盖该入口；Model Gateway 未接管的模型可能退回默认账号池", "system_config", false))
		}
	}
	return checks
}

func (s *SystemReadinessService) storageChecks(ctx context.Context) []dto.AdminSystemReadinessCheck {
	ossEnabled := false
	resultDriver := "local"
	if s.sys != nil {
		ossEnabled = s.sys.GetBool(ctx, "oss.enabled", false)
		resultDriver = strings.ToLower(strings.TrimSpace(s.sys.GetString(ctx, "storage.result_cache_driver", "local")))
	}
	checks := []dto.AdminSystemReadinessCheck{
		boolCheck("storage", "oss_enabled", "OSS 存储", ossEnabled, "OSS 已启用", "OSS 未启用，生成结果和快捷提示词封面会优先依赖本地缓存/运行环境存储", "system_config", false),
	}
	if !ossEnabled {
		return checks
	}
	checks = append(checks, boolCheck(
		"storage",
		"result_cache_driver",
		"生成结果与快捷封面缓存位置",
		resultDriver == "oss",
		"生成结果和快捷提示词封面使用 OSS",
		"OSS 已启用，但缓存位置不是 OSS，上传封面仍会依赖本地缓存",
		"system_config",
		false,
	))
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

func (s *SystemReadinessService) effectiveTurnstileHostnames(ctx context.Context, fallback []string) []string {
	if value := strings.TrimSpace(os.Getenv("KLEIN_TURNSTILE_ALLOWED_HOSTNAMES")); value != "" {
		return splitCSV(value)
	}
	if s.sys != nil {
		if value := strings.TrimSpace(s.sys.GetString(ctx, SettingTurnstileHostnames, "")); value != "" {
			return splitCSV(value)
		}
	}
	return fallback
}

func (s *SystemReadinessService) effectiveSMTPString(ctx context.Context, envKey, configKey, fallback string) (string, string) {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return value, "env"
	}
	if s.sys != nil {
		if value := strings.TrimSpace(s.sys.GetString(ctx, configKey, "")); value != "" {
			return value, "system_config"
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback, "config"
	}
	return "", "missing"
}

func (s *SystemReadinessService) effectiveSMTPInt(ctx context.Context, envKey, configKey string, fallback int64) (int64, string) {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n, "env"
		}
		return 0, "env"
	}
	if s.sys != nil {
		if value := s.sys.GetInt(ctx, configKey, 0); value > 0 {
			return value, "system_config"
		}
	}
	if fallback > 0 {
		return fallback, "config"
	}
	return 0, "missing"
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
