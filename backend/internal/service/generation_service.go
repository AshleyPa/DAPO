// Package service 生成任务编排：创建 → 预扣 → 调度账号 → 调用 provider → 结算 / 退款。
//
// 当前实现为同步 inline 执行（开发期）。生产建议替换为 asynq 投递到 worker。
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/jwtpayload"
	"github.com/kleinai/backend/pkg/logger"
	"github.com/kleinai/backend/pkg/ratelimit"
)

const codexOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

const (
	routeParamProvider      = "_provider_route_provider"
	routeParamSourceType    = "_provider_route_source_type"
	routeParamSourceCode    = "_provider_route_source_code"
	routeParamAdapter       = "_provider_route_adapter"
	routeParamUpstreamModel = "_provider_route_upstream_model"
	routeParamStrategy      = "_provider_route_strategy"
	routeParamAuthType      = "_provider_route_auth_type"
	routeParamImageAPIMode  = "_provider_route_image_api_mode"
	routeParamCandidates    = "_provider_route_candidates"
	routeParamSnapshot      = "_model_gateway_route_snapshot"

	providerParamImageAPIMode = "image_api_mode"

	generationProviderAPIChannel = "api_channel"
)

// GenerationService 生成调度服务。
type GenerationService struct {
	db             *gorm.DB
	repo           *repo.GenerationRepo
	pool           *AccountPool
	billing        *BillingService
	providers      map[string]provider.Provider // key: "gpt" / "grok"
	priceFn        PriceFunc
	aes            *crypto.AESGCM // 用于解密 account.credential_enc
	proxySvc       *ProxyService
	cfg            *SystemConfigService
	routeSvc       *ProviderRouteService
	modelRepo      *repo.ModelCatalogRepo
	sourceRepo     *repo.ModelSourceRepo
	apiChannelRepo *repo.APIChannelRepo
	apiLimiter     apiChannelDistributedLimiter

	videoJobSnapshotHook func(context.Context, string, map[string]any)
}

// PriceFunc 模型计费：返回单次成本（点 *100）。
type PriceFunc func(modelCode string, kind provider.Kind, params map[string]any) int64

// NewGenerationService 构造。aes 必须非空（账号凭证加密强制）。
func NewGenerationService(db *gorm.DB, r *repo.GenerationRepo, pool *AccountPool, billing *BillingService, providers map[string]provider.Provider, priceFn PriceFunc, aes *crypto.AESGCM, proxySvc *ProxyService, cfg *SystemConfigService, routeSvc *ProviderRouteService, modelRepo *repo.ModelCatalogRepo, sourceRepo *repo.ModelSourceRepo, apiChannelRepo *repo.APIChannelRepo, apiLimiters ...*ratelimit.Limiter) *GenerationService {
	return &GenerationService{
		db:             db,
		repo:           r,
		pool:           pool,
		billing:        billing,
		providers:      providers,
		priceFn:        priceFn,
		aes:            aes,
		proxySvc:       proxySvc,
		cfg:            cfg,
		routeSvc:       routeSvc,
		modelRepo:      modelRepo,
		sourceRepo:     sourceRepo,
		apiChannelRepo: apiChannelRepo,
		apiLimiter:     optionalAPIChannelDistributedLimiter(apiLimiters),
	}
}

// CreateRequest 创建生成请求 DTO（被 handler 填充）。
type CreateRequest struct {
	UserID    uint64
	APIKeyID  *uint64
	Kind      provider.Kind
	Mode      provider.Mode
	ModelCode string
	Provider  string
	Prompt    string
	NegPrompt string
	Params    map[string]any
	RefAssets []string
	Count     int
	IdemKey   string
	ClientIP  string
}

// Create 同步创建 + 触发任务。返回最终 task。
func (s *GenerationService) Create(ctx context.Context, req CreateRequest) (*model.GenerationTask, error) {
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.IdemKey == "" {
		req.IdemKey = uuid.NewString()
	}
	var routeErr error
	req.Params, routeErr = s.applyProviderRoute(ctx, req.Kind, req.ModelCode, req.Provider, req.Params)
	if routeErr != nil {
		return nil, routeErr
	}
	if sourceType := routeParamString(req.Params, routeParamSourceType, ""); sourceType == model.ModelSourceTypeAPIChannel {
		req.Provider = generationProviderAPIChannel
	} else if p := routeParamString(req.Params, routeParamProvider, req.Provider); p != "" {
		req.Provider = p
	}

	if existing, err := s.repo.GetByIdem(ctx, req.UserID, req.IdemKey); err == nil && existing != nil {
		return existing, nil
	}

	cost := int64(0)
	if s.priceFn != nil {
		cost = s.priceFn(req.ModelCode, req.Kind, req.Params) * int64(req.Count)
	}
	if cost < 0 {
		return nil, errcode.InvalidParam.WithMsg("model price not configured")
	}
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	req.Params[PricingAuditSnapshotKey] = GenerationPricingAuditSnapshot(ctx, s.cfg, s.modelRepo, req.ModelCode, req.Kind, req.Params, req.Count, cost)

	taskID := newULID()
	req.RefAssets = s.normalizeInputRefs(ctx, &model.GenerationTask{TaskID: taskID}, req.RefAssets)
	req.Params = compactLargeInlineParams(req.Params)
	paramsJSON, _ := json.Marshal(req.Params)
	var refJSON *string
	if len(req.RefAssets) > 0 {
		b, _ := json.Marshal(req.RefAssets)
		s := string(b)
		refJSON = &s
	}
	t := &model.GenerationTask{
		TaskID:       taskID,
		UserID:       req.UserID,
		Kind:         string(req.Kind),
		Mode:         string(req.Mode),
		ModelCode:    req.ModelCode,
		Prompt:       req.Prompt,
		Params:       string(paramsJSON),
		RefAssets:    refJSON,
		Count:        req.Count,
		CostPoints:   cost,
		IdemKey:      req.IdemKey,
		Provider:     req.Provider,
		Status:       model.GenStatusPending,
		FromAPIKeyID: req.APIKeyID,
	}
	if req.NegPrompt != "" {
		ng := req.NegPrompt
		t.NegPrompt = &ng
	}
	if req.ClientIP != "" {
		ip := req.ClientIP
		t.ClientIP = &ip
	}

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}

	if cost > 0 {
		if err := s.billing.PreDeduct(ctx, PreDeductReq{
			UserID:     req.UserID,
			TaskID:     taskID,
			Kind:       string(req.Kind),
			ModelCode:  req.ModelCode,
			Count:      req.Count,
			UnitPoints: cost / int64(req.Count),
		}); err != nil {
			_ = s.repo.SetFailed(ctx, taskID, err.Error())
			return nil, err
		}
	}

	go s.runTask(context.Background(), t)
	return t, nil
}

// runTask 后台执行：取池中账号 → 调 provider → 结算 / 退款。
func (s *GenerationService) runTask(ctx context.Context, t *model.GenerationTask) {
	log := logger.L().With(zap.String("task", t.TaskID))

	var params map[string]any
	_ = json.Unmarshal([]byte(t.Params), &params)
	routes := providerRoutesFromParams(params, t.Provider, t.ModelCode)
	var refs []string
	if t.RefAssets != nil {
		_ = json.Unmarshal([]byte(*t.RefAssets), &refs)
	}
	refs = s.normalizeInputRefs(ctx, t, refs)

	timeout := 5 * time.Minute
	if t.Kind == "video" {
		timeout = 15 * time.Minute
	}
	if shouldUseExtendedGPTImageTimeout(t, params, routes) {
		timeout = 10 * time.Minute
	}
	maxAttempts := 3
	retryDelay := 800 * time.Millisecond
	if s.cfg != nil {
		timeout = s.cfg.RetryTimeout(ctx, timeout)
		maxAttempts = s.cfg.RetryMaxAttempts(ctx)
		retryDelay = s.cfg.RetryBaseDelay(ctx)
	}
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	var acc *model.Account
	var res *provider.Result
	var lastErr error
	attemptLimit := maxAttempts
	if len(routes) > attemptLimit {
		attemptLimit = len(routes)
	}
	releaseAcc := func(a *model.Account) {
		if a != nil {
			s.pool.Release(a.ID)
		}
	}
	for attempt := 1; attempt <= attemptLimit; attempt++ {
		routeIndex := (attempt - 1) % len(routes)
		route := routes[routeIndex]
		route.RouteIndex = routeIndex + 1
		route.Attempt = attempt
		params = selectedProviderRouteAttemptParams(params, route, routeIndex+1)
		if err := s.repo.MergeParams(ctx, t.TaskID, params); err != nil {
			log.Warn("record provider route attempt failed", zap.Int("attempt", attempt), zap.Int("route_index", routeIndex), zap.Error(err))
		} else if b, err := json.Marshal(params); err == nil {
			t.Params = string(b)
		}
		if generationRouteSourceType(route) == model.ModelSourceTypeAPIChannel {
			out, err := s.generateWithAPIChannelRoute(ctx, t, route, params, refs, timeout)
			if err == nil {
				res = out
				break
			}
			lastErr = err
			log.Warn("api channel route failed", zap.Int("attempt", attempt), zap.String("source_code", route.SourceCode), zap.String("adapter", route.Adapter), zap.String("upstream_model", route.UpstreamModel), zap.Error(err))
			canTryNext := canRetryAPIChannelRouteError(err)
			if attempt == attemptLimit || !canTryNext {
				s.failTask(ctx, t, fmt.Sprintf("provider call: %v", err))
				return
			}
			sleepBeforeRetry(ctx, retryDelay, attempt)
			continue
		}
		prov, ok := s.providers[route.Provider]
		if !ok {
			lastErr = fmt.Errorf("provider not registered: %s", route.Provider)
			log.Warn("provider route unavailable", zap.Int("attempt", attempt), zap.String("provider", route.Provider), zap.String("upstream_model", route.UpstreamModel), zap.Error(lastErr))
			continue
		}
		picked, err := s.pickAccountForRoute(ctx, t, route, params)
		if err != nil {
			lastErr = fmt.Errorf("pick account for %s/%s: %w", route.Provider, route.UpstreamModel, err)
			log.Warn("provider route pick account failed", zap.Int("attempt", attempt), zap.String("provider", route.Provider), zap.String("upstream_model", route.UpstreamModel), zap.Error(err))
			if attempt < attemptLimit {
				sleepBeforeRetry(ctx, retryDelay, attempt)
				continue
			}
			break
		}
		acc = picked
		if err := s.repo.SetRunning(ctx, t.TaskID, acc.ID); err != nil {
			log.Warn("set running failed", zap.Error(err))
		}
		provParams := paramsForProviderRoute(params, route)

		provReq := &provider.Request{
			TaskID:    t.TaskID,
			Kind:      provider.Kind(t.Kind),
			Mode:      provider.Mode(t.Mode),
			ModelCode: route.UpstreamModel,
			Prompt:    t.Prompt,
			Params:    provParams,
			RefAssets: refs,
			Count:     t.Count,
			Account:   acc,
		}
		provReq.UpstreamLog = s.makeUpstreamLoggerForProvider(t, acc, route.Provider, accountPoolRouteLogMeta(route))
		if t.NegPrompt != nil {
			provReq.NegPrompt = *t.NegPrompt
		}
		if acc.BaseURL != nil {
			provReq.BaseURL = *acc.BaseURL
		} else if accountRequiresCodexRouteForRoute(t, route, params) && isCodexOAuthAccount(acc) {
			provReq.BaseURL = "https://chatgpt.com/backend-api/codex"
		}
		if proxyURL, perr := s.resolveProxyURL(ctx, acc); perr == nil {
			provReq.ProxyURL = proxyURL
		} else {
			log.Warn("resolve proxy failed", zap.Error(perr))
		}
		if s.aes != nil {
			cred, derr := s.providerCredential(ctx, acc, provReq.ProxyURL)
			if derr != nil {
				lastErr = derr
				if isFatalOAuthRefreshError(derr) {
					s.disableProviderAccount(ctx, acc, derr.Error())
				} else {
					s.markProviderFailed(ctx, acc, derr.Error(), 30*time.Minute)
				}
				releaseAcc(acc)
				acc = nil
				if attempt == attemptLimit || !retryableProviderError(derr) {
					s.failTask(ctx, t, fmt.Sprintf("provider call: %v", derr))
					return
				}
				sleepBeforeRetry(ctx, retryDelay, attempt)
				continue
			}
			provReq.Credential = cred
		}

		rctx, cancel := context.WithTimeout(ctx, timeout)
		out, err := prov.Generate(rctx, provReq)
		cancel()
		if err == nil {
			res = out
			break
		}
		lastErr = err
		if isUsageLimitReachedError(err) {
			s.markProviderQuotaLimited(ctx, acc, err.Error(), usageLimitResetAt(err))
		} else if isTransientProviderPathError(route.Provider, err) {
			s.pool.MarkTransientFailed(ctx, acc.ID, err.Error())
		} else {
			cooldown := providerCooldown(err)
			s.markProviderFailed(ctx, acc, err.Error(), cooldown)
		}
		releaseAcc(acc)
		acc = nil
		if attempt == attemptLimit || !retryableProviderError(err) {
			s.failTask(ctx, t, fmt.Sprintf("provider call: %v", err))
			return
		}
		log.Warn("provider retrying with next route/account", zap.Int("attempt", attempt), zap.String("provider", route.Provider), zap.String("upstream_model", route.UpstreamModel), zap.Uint64("account_id", picked.ID), zap.Error(err))
		sleepBeforeRetry(ctx, retryDelay, attempt)
	}
	if res == nil {
		releaseAcc(acc)
		if lastErr != nil {
			s.failTask(ctx, t, fmt.Sprintf("provider call: %v", lastErr))
		} else {
			s.failTask(ctx, t, "provider call failed")
		}
		return
	}
	if err := validateProviderGenerationResult(t, res); err != nil {
		releaseAcc(acc)
		s.failTask(ctx, t, err.Error())
		return
	}
	releaseAcc(acc)
	if acc != nil {
		s.pool.MarkUsed(ctx, acc.ID)
	}

	results := make([]*model.GenerationResult, 0, len(res.Assets))
	for i, a := range res.Assets {
		gr := &model.GenerationResult{
			TaskID: t.TaskID,
			UserID: t.UserID,
			Kind:   t.Kind,
			Seq:    int8(i),
			URL:    a.URL,
			Width:  intPtr(a.Width),
			Height: intPtr(a.Height),
		}
		if a.ThumbURL != "" {
			s := a.ThumbURL
			gr.ThumbURL = &s
		}
		if a.DurationMs > 0 {
			d := a.DurationMs
			gr.DurationMs = &d
		}
		if a.SizeBytes > 0 {
			b := a.SizeBytes
			gr.SizeBytes = &b
		}
		if len(a.Meta) > 0 {
			b, _ := json.Marshal(a.Meta)
			s := string(b)
			gr.Meta = &s
		}
		results = append(results, gr)
	}
	s.cacheResultAssets(ctx, t, acc, results)

	if err := s.repo.SetSucceeded(ctx, t.TaskID, results); err != nil {
		log.Error("set succeeded failed", zap.Error(err))
	}
	s.updateAccountUsageMeta(ctx, acc, t, len(results))
	if t.CostPoints > 0 {
		var accountID *uint64
		if acc != nil {
			accountID = &acc.ID
		}
		if err := s.billing.Settle(ctx, t.TaskID, accountID); err != nil {
			log.Error("settle failed", zap.Error(err))
		}
	}
}

func validateProviderGenerationResult(t *model.GenerationTask, res *provider.Result) error {
	if res == nil {
		return fmt.Errorf("provider returned empty result")
	}
	kind := ""
	if t != nil {
		kind = strings.ToLower(strings.TrimSpace(t.Kind))
	}
	if kind != string(provider.KindImage) && kind != string(provider.KindVideo) {
		return nil
	}
	if len(res.Assets) == 0 {
		return fmt.Errorf("provider returned no output assets")
	}
	for i, asset := range res.Assets {
		if strings.TrimSpace(asset.URL) == "" {
			return fmt.Errorf("provider returned blank output asset url at index %d", i)
		}
	}
	return nil
}

func (s *GenerationService) generateWithAPIChannelRoute(ctx context.Context, t *model.GenerationTask, route ProviderRoute, params map[string]any, refs []string, timeout time.Duration) (*provider.Result, error) {
	if s.apiChannelRepo == nil {
		return nil, fmt.Errorf("api channel repository is not initialized")
	}
	ch, err := s.apiChannelRepo.GetByCode(ctx, route.SourceCode)
	if err != nil || ch == nil {
		return nil, fmt.Errorf("api channel not found: %s", route.SourceCode)
	}
	if ch.Status != model.APIChannelStatusEnabled {
		return nil, fmt.Errorf("api channel disabled: %s", route.SourceCode)
	}
	if t.Kind == string(provider.KindVideo) {
		return s.generateVideoWithAPIChannelRoute(ctx, t, route, ch, params, refs, timeout)
	}
	if t.Kind != string(provider.KindImage) {
		return nil, fmt.Errorf("api channel runtime currently supports image generation only")
	}
	imageMode := normalizeProviderRouteImageAPIMode(route.ImageAPIMode)
	if imageMode == "" {
		imageMode = imageAPIModeForAPIChannelAdapter(route.Adapter)
	}
	if imageMode == "" {
		return nil, fmt.Errorf("api channel adapter does not support image generation: %s", route.Adapter)
	}
	prov, ok := s.providers[model.ProviderGPT]
	if !ok {
		return nil, fmt.Errorf("provider not registered: %s", model.ProviderGPT)
	}
	credRef, err := selectAPIChannelCredentialWithLimiter(ctx, s.apiChannelRepo, s.aes, ch, 0, s.apiLimiter)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetRunningNoAccount(ctx, t.TaskID); err != nil {
		logger.FromCtx(ctx).Warn("set running for api channel failed", zap.Error(err))
	}
	provParams := paramsForProviderRoute(params, route)
	provParams[providerParamImageAPIMode] = imageMode
	provParams[routeParamImageAPIMode] = imageMode
	provReq := &provider.Request{
		TaskID:      t.TaskID,
		Kind:        provider.Kind(t.Kind),
		Mode:        provider.Mode(t.Mode),
		ModelCode:   route.UpstreamModel,
		Prompt:      t.Prompt,
		Params:      provParams,
		RefAssets:   refs,
		Count:       t.Count,
		Credential:  credRef.Token,
		BaseURL:     ch.BaseURL,
		UpstreamLog: s.makeUpstreamLoggerForAPIChannel(t, route, ch, credRef),
	}
	if t.NegPrompt != nil {
		provReq.NegPrompt = *t.NegPrompt
	}
	if proxyURL, perr := s.resolveProxyURLByID(ctx, ch.ProxyID); perr == nil {
		provReq.ProxyURL = proxyURL
	} else {
		logger.FromCtx(ctx).Warn("resolve api channel proxy failed", zap.String("channel", ch.Code), zap.Error(perr))
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := prov.Generate(rctx, provReq)
	if err != nil {
		recordAPIChannelCredentialError(ctx, s.apiChannelRepo, credRef, err)
		return nil, err
	}
	recordAPIChannelCredentialSuccess(ctx, s.apiChannelRepo, credRef)
	return result, nil
}

func (s *GenerationService) makeUpstreamLogger(t *model.GenerationTask, acc *model.Account) provider.UpstreamLogger {
	return s.makeUpstreamLoggerForProvider(t, acc, "")
}

func (s *GenerationService) makeUpstreamLoggerForProvider(t *model.GenerationTask, acc *model.Account, fallbackProvider string, defaultMeta ...map[string]any) provider.UpstreamLogger {
	return func(ctx context.Context, e provider.UpstreamLogEntry) {
		if t == nil {
			return
		}
		metaMap := mergeUpstreamLogMeta(defaultMeta, e.Meta)
		meta := ""
		if len(metaMap) > 0 {
			if b, err := json.Marshal(metaMap); err == nil {
				meta = string(b)
			}
		}
		row := &model.GenerationUpstreamLog{
			TaskID:     t.TaskID,
			Provider:   e.Provider,
			Stage:      e.Stage,
			Method:     e.Method,
			URL:        truncate(e.URL, 512),
			StatusCode: e.StatusCode,
			DurationMs: e.DurationMs,
		}
		if row.Provider == "" {
			if fallbackProvider != "" {
				row.Provider = fallbackProvider
			} else {
				row.Provider = t.Provider
			}
		}
		if acc != nil {
			row.AccountID = &acc.ID
		}
		if e.RequestExcerpt != "" {
			v := truncate(e.RequestExcerpt, 12000)
			row.RequestExcerpt = &v
		}
		if e.ResponseExcerpt != "" {
			v := truncate(e.ResponseExcerpt, 12000)
			row.ResponseExcerpt = &v
		}
		if e.Error != "" {
			v := truncate(e.Error, 4000)
			row.Error = &v
		}
		if meta != "" {
			row.Meta = &meta
		}
		if err := s.repo.CreateUpstreamLog(ctx, row); err != nil {
			logger.FromCtx(ctx).Warn("generation.upstream_log_failed", zap.String("task_id", t.TaskID), zap.String("stage", e.Stage), zap.Error(err))
		}
	}
}

func accountPoolRouteLogMeta(route ProviderRoute) map[string]any {
	meta := map[string]any{
		"model_gateway_source_type": model.ModelSourceTypeAccountPool,
		"model_gateway_source_code": route.Provider,
		"upstream_model":            route.UpstreamModel,
		"strategy":                  normalizeRouteStrategy(route.Strategy),
		"auth_type":                 route.AuthType,
	}
	addModelGatewayRouteAttemptMeta(meta, route.RouteIndex, route.Attempt)
	if strings.TrimSpace(route.ImageAPIMode) != "" {
		meta["image_api_mode"] = route.ImageAPIMode
	}
	return meta
}

func mergeUpstreamLogMeta(defaults []map[string]any, entry map[string]any) map[string]any {
	out := map[string]any{}
	for _, group := range defaults {
		for k, v := range group {
			if strings.TrimSpace(k) == "" || isEmptyUpstreamMetaValue(v) {
				continue
			}
			out[k] = v
		}
	}
	for k, v := range entry {
		if strings.TrimSpace(k) == "" || isEmptyUpstreamMetaValue(v) {
			continue
		}
		out[k] = v
	}
	return out
}

func isEmptyUpstreamMetaValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	default:
		return false
	}
}

func (s *GenerationService) makeUpstreamLoggerForAPIChannel(t *model.GenerationTask, route ProviderRoute, ch *model.APIChannel, credRef *APIChannelCredentialRef) provider.UpstreamLogger {
	base := s.makeUpstreamLoggerForProvider(t, nil, generationProviderAPIChannel)
	return func(ctx context.Context, e provider.UpstreamLogEntry) {
		e.Provider = generationProviderAPIChannel
		e.Meta = mergeUpstreamLogMeta([]map[string]any{apiChannelRouteLogMeta(route, ch, credRef)}, e.Meta)
		base(ctx, e)
	}
}

func apiChannelRouteLogMeta(route ProviderRoute, ch *model.APIChannel, credRef *APIChannelCredentialRef) map[string]any {
	meta := map[string]any{
		"model_gateway_source_type": model.ModelSourceTypeAPIChannel,
		"model_gateway_source_code": route.SourceCode,
		"model_gateway_adapter":     route.Adapter,
		"upstream_model":            route.UpstreamModel,
		"strategy":                  normalizeRouteStrategy(route.Strategy),
		"auth_type":                 route.AuthType,
	}
	addModelGatewayRouteAttemptMeta(meta, route.RouteIndex, route.Attempt)
	if strings.TrimSpace(route.ImageAPIMode) != "" {
		meta["image_api_mode"] = route.ImageAPIMode
	}
	if ch != nil {
		meta["api_channel_id"] = ch.ID
		meta["api_channel_code"] = ch.Code
		meta["api_channel_name"] = ch.Name
		meta["api_channel_provider_name"] = ch.ProviderName
	}
	addAPIChannelCredentialMeta(meta, credRef)
	return meta
}

func addModelGatewayRouteAttemptMeta(meta map[string]any, routeIndex, attempt int) {
	if meta == nil {
		return
	}
	if routeIndex > 0 {
		meta["model_gateway_route_index"] = routeIndex
	}
	if attempt > 0 {
		meta["model_gateway_attempt"] = attempt
	}
}

func (s *GenerationService) providerCredential(ctx context.Context, acc *model.Account, proxyURL string) (string, error) {
	if acc == nil {
		return "", fmt.Errorf("missing account")
	}
	if acc.AuthType == model.AuthTypeOAuth && acc.Provider == model.ProviderGPT {
		return s.gptOAuthAccessToken(ctx, acc, proxyURL)
	}
	if len(acc.CredentialEnc) == 0 {
		return "", fmt.Errorf("account credential is empty")
	}
	plain, err := s.aes.Decrypt(acc.CredentialEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt credential failed: %w", err)
	}
	cred := strings.TrimSpace(string(plain))
	if cred == "" {
		return "", fmt.Errorf("account credential is empty")
	}
	return cred, nil
}

func (s *GenerationService) apiChannelCredential(ch *model.APIChannel) (string, error) {
	ref, err := selectAPIChannelCredential(context.Background(), s.apiChannelRepo, s.aes, ch, 0)
	if err != nil {
		return "", err
	}
	return ref.Token, nil
}

func (s *GenerationService) pickAccountForTask(ctx context.Context, t *model.GenerationTask, params map[string]any) (*model.Account, error) {
	if t == nil {
		return nil, errcode.NoAvailableAcc
	}
	route := providerRouteFromParams(params, t.Provider, t.ModelCode)
	return s.pickAccountForRoute(ctx, t, route, params)
}

func (s *GenerationService) pickAccountForRoute(ctx context.Context, t *model.GenerationTask, route ProviderRoute, params map[string]any) (*model.Account, error) {
	if t == nil {
		return nil, errcode.NoAvailableAcc
	}
	predicate := func(acc *model.Account) bool {
		return matchesRouteAuthType(acc, route.AuthType) && accountAllowsRouteModel(acc, t.ModelCode, route.UpstreamModel)
	}
	if route.Provider != model.ProviderGPT || t.Kind != string(provider.KindImage) || !strings.EqualFold(t.ModelCode, "gpt-image-2") {
		return s.pool.ReserveWhere(ctx, route.Provider, route.Strategy, predicate)
	}
	if route.AuthType == "" && accountRequiresCodexRouteForRoute(t, route, params) {
		return s.pool.ReserveWhere(ctx, route.Provider, route.Strategy, func(acc *model.Account) bool {
			return predicate(acc) && isCodexOAuthAccount(acc)
		})
	}
	if route.AuthType == "" && routePrefersAPIKeyImageMode(route.ImageAPIMode) {
		return s.pool.ReserveWhere(ctx, route.Provider, route.Strategy, func(acc *model.Account) bool {
			return predicate(acc) && acc != nil && acc.AuthType == model.AuthTypeAPIKey
		})
	}
	if route.AuthType == "" {
		return s.pool.ReserveWhere(ctx, route.Provider, route.Strategy, func(acc *model.Account) bool {
			return predicate(acc) && acc != nil && acc.AuthType == model.AuthTypeOAuth
		})
	}
	return s.pool.ReserveWhere(ctx, route.Provider, route.Strategy, func(acc *model.Account) bool {
		if acc == nil {
			return false
		}
		return predicate(acc)
	})
}

func routePrefersAPIKeyImageMode(mode string) bool {
	switch normalizeProviderRouteImageAPIMode(mode) {
	case ProviderRouteImageAPIModeOpenAIImages, ProviderRouteImageAPIModePic2API, ProviderRouteImageAPIModeNovaAsync:
		return true
	default:
		return false
	}
}

func accountRequiresCodexRoute(t *model.GenerationTask, params map[string]any) bool {
	route := providerRouteFromParams(params, "", "")
	return accountRequiresCodexRouteForRoute(t, route, params)
}

func accountRequiresCodexRouteForRoute(t *model.GenerationTask, route ProviderRoute, params map[string]any) bool {
	if t == nil || t.Kind != string(provider.KindImage) || !strings.EqualFold(t.ModelCode, "gpt-image-2") {
		return false
	}
	if strings.TrimSpace(route.Provider) == "" {
		route.Provider = t.Provider
	}
	if route.Provider != model.ProviderGPT {
		return false
	}
	if mode := normalizeProviderRouteImageAPIMode(route.ImageAPIMode); mode != "" && mode != ProviderRouteImageAPIModeOpenAIResponses {
		return false
	}
	return !shouldUseGPTWebRoute(params)
}

func shouldUseGPTWebRoute(params map[string]any) bool {
	tier := strings.ToUpper(strings.TrimSpace(strParamAny(params, "resolution", strParamAny(params, "size_tier", ""))))
	if tier == "" {
		size := strParamAny(params, "size", "")
		w, h := parseWH(size)
		if size == "" || w*h <= 1500000 {
			return true
		}
		return false
	}
	return tier == "1K" || tier == "1"
}

func (s *GenerationService) generationRoutes(ctx context.Context, kind provider.Kind, modelCode, fallbackProvider string) []ProviderRoute {
	if routes, managed := s.modelGatewayGenerationRoutes(ctx, kind, modelCode); managed {
		return routes
	}
	routes := []ProviderRoute{accountPoolProviderRoute(fallbackProvider, modelCode, "round_robin")}
	if s.routeSvc != nil {
		if resolved, _ := s.routeSvc.ResolveCandidates(ctx, kind, modelCode, fallbackProvider); len(resolved) > 0 {
			routes = make([]ProviderRoute, 0, len(resolved))
			for _, route := range resolved {
				route.SourceType = model.ModelSourceTypeAccountPool
				route.SourceCode = route.Provider
				routes = appendProviderRouteCandidate(routes, route)
			}
		}
	}
	return routes
}

func (s *GenerationService) modelGatewayGenerationRoutes(ctx context.Context, kind provider.Kind, modelCode string) ([]ProviderRoute, bool) {
	if s.modelRepo == nil || s.sourceRepo == nil {
		return nil, false
	}
	item, err := s.modelRepo.GetByCode(ctx, modelCode)
	if err != nil || item == nil || item.Status != model.ModelCatalogStatusEnabled {
		return nil, false
	}
	entryKind := normalizeModelGatewayKindLoose(item.EntryKind)
	if !modelGatewayEntryKindMatchesGenerationKind(entryKind, kind) {
		return nil, false
	}
	status := int8(model.ModelSourceStatusEnabled)
	sources, _, err := s.sourceRepo.List(ctx, repo.ModelSourceListFilter{ModelCode: modelCode, Status: &status, Page: 1, PageSize: 500})
	if err != nil {
		logger.FromCtx(ctx).Warn("generation.model_gateway.list_sources", zap.String("model", modelCode), zap.Error(err))
		return nil, false
	}
	if len(sources) == 0 {
		return nil, false
	}
	routes := make([]ProviderRoute, 0, len(sources))
	for _, source := range sources {
		if source == nil {
			continue
		}
		switch source.SourceType {
		case model.ModelSourceTypeAPIChannel:
			if route, ok := s.apiChannelGenerationRoute(ctx, item, entryKind, kind, source); ok {
				routes = appendProviderRouteCandidate(routes, route)
			}
		case model.ModelSourceTypeAccountPool:
			if route, ok := accountPoolGenerationRoute(item, source); ok {
				routes = appendProviderRouteCandidate(routes, route)
			}
		}
	}
	return orderProviderRoutesForRuntime(modelCode, string(kind), routes), true
}

func (s *GenerationService) apiChannelGenerationRoute(ctx context.Context, item *model.ModelCatalog, entryKind string, kind provider.Kind, source *model.ModelSourceMapping) (ProviderRoute, bool) {
	if s.apiChannelRepo == nil || (kind != provider.KindImage && kind != provider.KindVideo) {
		return ProviderRoute{}, false
	}
	ch, err := s.apiChannelRepo.GetByCode(ctx, source.SourceCode)
	if err != nil || ch == nil || ch.Status != model.APIChannelStatusEnabled {
		return ProviderRoute{}, false
	}
	upstreamModel := effectiveModelGatewayUpstreamModel(item, source.UpstreamModel)
	adapter := strings.TrimSpace(source.Adapter)
	if adapter == "" {
		adapter = ch.Adapter
	}
	imageMode := strings.TrimSpace(source.ImageAPIMode)
	if imageMode == "" && kind == provider.KindImage {
		imageMode = imageAPIModeForAPIChannelAdapter(adapter)
	}
	if kind == provider.KindImage && imageMode == "" {
		return ProviderRoute{}, false
	}
	if kind == provider.KindVideo && adapter != model.APIChannelAdapterOpenAIVideo {
		return ProviderRoute{}, false
	}
	if !modelGatewayListAllows(parseStringListJSON(ch.Models), item.ModelCode, upstreamModel) {
		return ProviderRoute{}, false
	}
	if !modelGatewayCapabilityAllows(parseStringListJSON(ch.Capabilities), entryKind) {
		return ProviderRoute{}, false
	}
	if reason := apiChannelOperationalSkipReason(inspectAPIChannelOperational(ctx, s.apiChannelRepo, source.SourceCode)); reason != "" {
		return ProviderRoute{}, false
	}
	return ProviderRoute{
		SourceType:    model.ModelSourceTypeAPIChannel,
		SourceCode:    source.SourceCode,
		Adapter:       adapter,
		Provider:      model.ProviderGPT,
		UpstreamModel: upstreamModel,
		Strategy:      normalizeRouteStrategy(source.Strategy),
		AuthType:      source.AuthType,
		ImageAPIMode:  imageMode,
		Weight:        normalizeRuntimeRouteWeight(source.Weight),
		Priority:      normalizeRuntimeRoutePriority(source.Priority),
	}, true
}

func accountPoolGenerationRoute(item *model.ModelCatalog, source *model.ModelSourceMapping) (ProviderRoute, bool) {
	providerName := strings.TrimSpace(source.SourceCode)
	if providerName != model.ProviderGPT && providerName != model.ProviderGROK {
		return ProviderRoute{}, false
	}
	upstreamModel := effectiveModelGatewayUpstreamModel(item, source.UpstreamModel)
	if accountPoolSourceMismatchReason(item, providerName, upstreamModel) != "" {
		return ProviderRoute{}, false
	}
	return ProviderRoute{
		SourceType:    model.ModelSourceTypeAccountPool,
		SourceCode:    providerName,
		Provider:      providerName,
		UpstreamModel: upstreamModel,
		Strategy:      normalizeRouteStrategy(source.Strategy),
		AuthType:      source.AuthType,
		ImageAPIMode:  source.ImageAPIMode,
		Weight:        normalizeRuntimeRouteWeight(source.Weight),
		Priority:      normalizeRuntimeRoutePriority(source.Priority),
	}, true
}

func (s *GenerationService) generationSkippedCandidates(ctx context.Context, kind provider.Kind, modelCode string) []ProviderRoute {
	if s.modelRepo == nil || s.sourceRepo == nil {
		return nil
	}
	item, err := s.modelRepo.GetByCode(ctx, modelCode)
	if err != nil || item == nil || item.Status != model.ModelCatalogStatusEnabled {
		return nil
	}
	entryKind := normalizeModelGatewayKindLoose(item.EntryKind)
	if !modelGatewayEntryKindMatchesGenerationKind(entryKind, kind) {
		return nil
	}
	sources, _, err := s.sourceRepo.List(ctx, repo.ModelSourceListFilter{ModelCode: modelCode, Page: 1, PageSize: 500})
	if err != nil {
		return nil
	}
	out := make([]ProviderRoute, 0)
	for _, source := range sources {
		if source == nil {
			continue
		}
		if skipped := s.generationSkippedCandidate(ctx, item, entryKind, kind, source); skipped.SkipReason != "" {
			out = append(out, skipped)
		}
	}
	return out
}

func (s *GenerationService) generationSkippedCandidate(ctx context.Context, item *model.ModelCatalog, entryKind string, kind provider.Kind, source *model.ModelSourceMapping) ProviderRoute {
	route := ProviderRoute{
		SourceType:    source.SourceType,
		SourceCode:    source.SourceCode,
		Adapter:       strings.TrimSpace(source.Adapter),
		UpstreamModel: effectiveModelGatewayUpstreamModel(item, source.UpstreamModel),
		Strategy:      normalizeRouteStrategy(source.Strategy),
		AuthType:      source.AuthType,
		ImageAPIMode:  source.ImageAPIMode,
		Weight:        normalizeRuntimeRouteWeight(source.Weight),
		Priority:      normalizeRuntimeRoutePriority(source.Priority),
	}
	if source.Status != model.ModelSourceStatusEnabled {
		route.SkipReason = "来源映射已停用"
		return route
	}
	switch source.SourceType {
	case model.ModelSourceTypeAPIChannel:
		return s.apiChannelGenerationSkippedCandidate(ctx, item, entryKind, kind, source, route)
	case model.ModelSourceTypeAccountPool:
		return accountPoolGenerationSkippedCandidate(item, source, route)
	default:
		route.SkipReason = "来源类型不支持"
		return route
	}
}

func (s *GenerationService) apiChannelGenerationSkippedCandidate(ctx context.Context, item *model.ModelCatalog, entryKind string, kind provider.Kind, source *model.ModelSourceMapping, route ProviderRoute) ProviderRoute {
	if s.apiChannelRepo == nil {
		route.SkipReason = "API 渠道仓储未初始化"
		return route
	}
	if kind != provider.KindImage && kind != provider.KindVideo {
		route.SkipReason = "API 渠道暂不支持该入口运行时"
		return route
	}
	ch, err := s.apiChannelRepo.GetByCode(ctx, source.SourceCode)
	if err != nil || ch == nil {
		route.SkipReason = "API 渠道不存在或已删除"
		return route
	}
	if route.Adapter == "" {
		route.Adapter = ch.Adapter
	}
	if route.ImageAPIMode == "" && kind == provider.KindImage {
		route.ImageAPIMode = imageAPIModeForAPIChannelAdapter(route.Adapter)
	}
	if ch.Status != model.APIChannelStatusEnabled {
		route.SkipReason = "API 渠道已停用"
		return route
	}
	if kind == provider.KindImage && route.ImageAPIMode == "" {
		route.SkipReason = "API 渠道协议不支持图片运行时"
		return route
	}
	if kind == provider.KindVideo && route.Adapter != model.APIChannelAdapterOpenAIVideo {
		route.SkipReason = "API 渠道协议不支持视频运行时"
		return route
	}
	if !modelGatewayListAllows(parseStringListJSON(ch.Models), item.ModelCode, route.UpstreamModel) {
		route.SkipReason = "API 渠道模型白名单不包含该模型"
		return route
	}
	if !modelGatewayCapabilityAllows(parseStringListJSON(ch.Capabilities), entryKind) {
		route.SkipReason = "API 渠道能力不匹配"
		return route
	}
	if reason := apiChannelOperationalSkipReason(inspectAPIChannelOperational(ctx, s.apiChannelRepo, source.SourceCode)); reason != "" {
		route.SkipReason = reason
		return route
	}
	return ProviderRoute{}
}

func accountPoolGenerationSkippedCandidate(item *model.ModelCatalog, source *model.ModelSourceMapping, route ProviderRoute) ProviderRoute {
	providerName := strings.TrimSpace(source.SourceCode)
	if providerName != model.ProviderGPT && providerName != model.ProviderGROK {
		route.SkipReason = "账号池来源只支持 GPT 或 GROK"
		return route
	}
	route.Provider = providerName
	if reason := accountPoolSourceMismatchReason(item, providerName, route.UpstreamModel); reason != "" {
		route.SkipReason = reason
		return route
	}
	return ProviderRoute{}
}

func accountPoolProviderRoute(providerName, modelCode, strategy string) ProviderRoute {
	return ProviderRoute{
		SourceType:    model.ModelSourceTypeAccountPool,
		SourceCode:    strings.TrimSpace(providerName),
		Provider:      strings.TrimSpace(providerName),
		UpstreamModel: strings.TrimSpace(modelCode),
		Strategy:      normalizeRouteStrategy(strategy),
	}
}

func modelGatewayEntryKindMatchesGenerationKind(entryKind string, kind provider.Kind) bool {
	switch kind {
	case provider.KindImage:
		return entryKind == model.ModelCatalogKindImage
	case provider.KindVideo:
		return entryKind == model.ModelCatalogKindVideo
	default:
		return false
	}
}

func effectiveModelGatewayUpstreamModel(item *model.ModelCatalog, sourceUpstream string) string {
	upstreamModel := strings.TrimSpace(sourceUpstream)
	if upstreamModel == "" && item != nil {
		upstreamModel = strings.TrimSpace(item.UpstreamDefaultModel)
	}
	if upstreamModel == "" && item != nil {
		upstreamModel = item.ModelCode
	}
	return upstreamModel
}

func imageAPIModeForAPIChannelAdapter(adapter string) string {
	switch strings.TrimSpace(adapter) {
	case model.APIChannelAdapterOpenAIImages:
		return ProviderRouteImageAPIModeOpenAIImages
	case model.APIChannelAdapterOpenAIResponses:
		return ProviderRouteImageAPIModeOpenAIResponses
	case model.APIChannelAdapterNovaAsync:
		return ProviderRouteImageAPIModeNovaAsync
	case model.APIChannelAdapterPic2APIImages:
		return ProviderRouteImageAPIModePic2API
	default:
		return ""
	}
}

func isAPIChannelRouteUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "api channel repository is not initialized") ||
		strings.Contains(msg, "api channel is nil") ||
		strings.Contains(msg, "api channel not found") ||
		strings.Contains(msg, "api channel disabled") ||
		strings.Contains(msg, "api channel adapter does not support") ||
		strings.Contains(msg, "api channel runtime currently supports") ||
		strings.Contains(msg, "api channel missing credential") ||
		strings.Contains(msg, "decrypt api channel credential failed") ||
		strings.Contains(msg, "provider not registered")
}

func canRetryAPIChannelRouteError(err error) bool {
	if isAPIChannelVideoAcceptedError(err) {
		return false
	}
	return retryableProviderError(err) || isAPIChannelRouteUnavailableError(err)
}

func generationRouteSourceType(route ProviderRoute) string {
	sourceType := strings.TrimSpace(route.SourceType)
	if sourceType != "" {
		return sourceType
	}
	sourceCode := strings.TrimSpace(route.SourceCode)
	if sourceCode == model.ProviderGPT || sourceCode == model.ProviderGROK {
		return model.ModelSourceTypeAccountPool
	}
	if sourceCode != "" || strings.TrimSpace(route.Adapter) != "" {
		return model.ModelSourceTypeAPIChannel
	}
	return model.ModelSourceTypeAccountPool
}

func (s *GenerationService) applyProviderRoute(ctx context.Context, kind provider.Kind, modelCode, fallbackProvider string, params map[string]any) (map[string]any, error) {
	if params == nil {
		params = map[string]any{}
	}
	routes := s.generationRoutes(ctx, kind, modelCode, fallbackProvider)
	if len(routes) == 0 {
		return params, errcode.InvalidParam.WithMsg("模型库已启用，但当前没有可用生成来源")
	}
	skipped := s.generationSkippedCandidates(ctx, kind, modelCode)
	route := routes[0]
	if sourceType := generationRouteSourceType(route); sourceType != "" {
		params[routeParamSourceType] = sourceType
	}
	if sourceCode := strings.TrimSpace(route.SourceCode); sourceCode != "" {
		params[routeParamSourceCode] = sourceCode
	}
	if adapter := strings.TrimSpace(route.Adapter); adapter != "" {
		params[routeParamAdapter] = adapter
	}
	if route.Provider != "" {
		params[routeParamProvider] = route.Provider
	}
	if route.UpstreamModel != "" {
		params[routeParamUpstreamModel] = route.UpstreamModel
	}
	if route.Strategy != "" {
		params[routeParamStrategy] = route.Strategy
	}
	if route.AuthType != "" {
		params[routeParamAuthType] = route.AuthType
	}
	if route.ImageAPIMode != "" {
		params[routeParamImageAPIMode] = route.ImageAPIMode
		params[providerParamImageAPIMode] = route.ImageAPIMode
	}
	params[routeParamCandidates] = routes
	params[routeParamSnapshot] = providerRouteSnapshotPayload(modelCode, string(kind), routes, 1, skipped)
	return params, nil
}

func providerRouteFromParams(params map[string]any, fallbackProvider, modelCode string) ProviderRoute {
	return ProviderRoute{
		SourceType:    routeParamString(params, routeParamSourceType, ""),
		SourceCode:    routeParamString(params, routeParamSourceCode, ""),
		Adapter:       routeParamString(params, routeParamAdapter, ""),
		Provider:      routeParamString(params, routeParamProvider, fallbackProvider),
		UpstreamModel: routeParamString(params, routeParamUpstreamModel, modelCode),
		Strategy:      normalizeRouteStrategy(routeParamString(params, routeParamStrategy, "round_robin")),
		AuthType:      routeParamString(params, routeParamAuthType, ""),
		ImageAPIMode:  normalizeProviderRouteImageAPIMode(routeParamString(params, routeParamImageAPIMode, routeParamString(params, providerParamImageAPIMode, ""))),
	}
}

func providerRoutesFromParams(params map[string]any, fallbackProvider, modelCode string) []ProviderRoute {
	if params != nil {
		if raw, ok := params[routeParamCandidates]; ok {
			if routes := decodeProviderRouteCandidates(raw, modelCode); len(routes) > 0 {
				return routes
			}
		}
	}
	return []ProviderRoute{providerRouteFromParams(params, fallbackProvider, modelCode)}
}

func decodeProviderRouteCandidates(raw any, modelCode string) []ProviderRoute {
	var routes []ProviderRoute
	appendRoute := func(route ProviderRoute) {
		routes = appendProviderRouteCandidate(routes, route.withDefaults(modelCode))
	}
	switch v := raw.(type) {
	case []ProviderRoute:
		for _, route := range v {
			appendRoute(route)
		}
	case []any:
		for _, item := range v {
			switch x := item.(type) {
			case map[string]any:
				appendRoute(providerRouteFromCandidateMap(x))
			case map[string]string:
				appendRoute(ProviderRoute{
					SourceType:    x["source_type"],
					SourceCode:    x["source_code"],
					Adapter:       x["adapter"],
					Provider:      x["provider"],
					UpstreamModel: x["upstream_model"],
					AuthType:      x["auth_type"],
					ImageAPIMode:  x["image_api_mode"],
					Strategy:      x["strategy"],
				})
			}
		}
	case string:
		var decoded []ProviderRoute
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			for _, route := range decoded {
				appendRoute(route)
			}
		}
	}
	return routes
}

func providerRouteFromCandidateMap(m map[string]any) ProviderRoute {
	return ProviderRoute{
		SourceType:    strFromMap(m, "source_type"),
		SourceCode:    strFromMap(m, "source_code"),
		Adapter:       strFromMap(m, "adapter"),
		Provider:      strFromMap(m, "provider"),
		UpstreamModel: strFromMap(m, "upstream_model"),
		AuthType:      strFromMap(m, "auth_type"),
		ImageAPIMode:  strFromMap(m, "image_api_mode"),
		Strategy:      strFromMap(m, "strategy"),
		Weight:        intFromMap(m, "weight"),
		Priority:      intFromMap(m, "priority"),
	}
}

func providerRouteSnapshotPayload(modelCode, kind string, routes []ProviderRoute, selectedIndex int, skipped ...[]ProviderRoute) map[string]any {
	candidates := make([]map[string]any, 0, len(routes))
	for idx, route := range routes {
		candidates = append(candidates, map[string]any{
			"index":          idx + 1,
			"source_type":    generationRouteSourceType(route),
			"source_code":    route.SourceCode,
			"provider":       route.Provider,
			"adapter":        route.Adapter,
			"upstream_model": route.UpstreamModel,
			"strategy":       normalizeRouteStrategy(route.Strategy),
			"auth_type":      route.AuthType,
			"image_api_mode": route.ImageAPIMode,
			"priority":       normalizeRuntimeRoutePriority(route.Priority),
			"weight":         normalizeRuntimeRouteWeight(route.Weight),
		})
	}
	skippedCandidates := make([]map[string]any, 0)
	if len(skipped) > 0 {
		for idx, route := range skipped[0] {
			skippedCandidates = append(skippedCandidates, map[string]any{
				"index":          idx + 1,
				"source_type":    generationRouteSourceType(route),
				"source_code":    route.SourceCode,
				"provider":       route.Provider,
				"adapter":        route.Adapter,
				"upstream_model": route.UpstreamModel,
				"strategy":       normalizeRouteStrategy(route.Strategy),
				"auth_type":      route.AuthType,
				"image_api_mode": route.ImageAPIMode,
				"priority":       normalizeRuntimeRoutePriority(route.Priority),
				"weight":         normalizeRuntimeRouteWeight(route.Weight),
				"skip_reason":    route.SkipReason,
			})
		}
	}
	payload := map[string]any{
		"version":         1,
		"model_code":      modelCode,
		"kind":            kind,
		"selected_index":  selectedIndex,
		"candidate_count": len(candidates),
		"candidates":      candidates,
	}
	if len(skippedCandidates) > 0 {
		payload["skipped_count"] = len(skippedCandidates)
		payload["skipped_candidates"] = skippedCandidates
	}
	return payload
}

func selectedProviderRouteAttemptParams(params map[string]any, route ProviderRoute, selectedIndex int) map[string]any {
	out := paramsForProviderRoute(params, route)
	if snapshot, ok := routeSnapshotMap(out[routeParamSnapshot]); ok {
		snapshot["selected_index"] = selectedIndex
		out[routeParamSnapshot] = snapshot
	}
	return out
}

func routeSnapshotMap(raw any) (map[string]any, bool) {
	switch v := raw.(type) {
	case map[string]any:
		return v, true
	case string:
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err == nil && out != nil {
			return out, true
		}
	}
	return nil, false
}

func paramsForProviderRoute(params map[string]any, route ProviderRoute) map[string]any {
	out := make(map[string]any, len(params)+6)
	for k, v := range params {
		out[k] = v
	}
	if route.Provider != "" {
		out[routeParamProvider] = route.Provider
	}
	if sourceType := generationRouteSourceType(route); sourceType != "" {
		out[routeParamSourceType] = sourceType
	} else {
		delete(out, routeParamSourceType)
	}
	if route.SourceCode != "" {
		out[routeParamSourceCode] = route.SourceCode
	} else {
		delete(out, routeParamSourceCode)
	}
	if route.Adapter != "" {
		out[routeParamAdapter] = route.Adapter
	} else {
		delete(out, routeParamAdapter)
	}
	if route.UpstreamModel != "" {
		out[routeParamUpstreamModel] = route.UpstreamModel
	}
	if route.Strategy != "" {
		out[routeParamStrategy] = route.Strategy
	}
	if route.AuthType != "" {
		out[routeParamAuthType] = route.AuthType
	}
	if route.ImageAPIMode != "" {
		out[routeParamImageAPIMode] = route.ImageAPIMode
		out[providerParamImageAPIMode] = route.ImageAPIMode
	} else {
		delete(out, routeParamImageAPIMode)
		delete(out, providerParamImageAPIMode)
	}
	return out
}

func strFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func routeParamString(params map[string]any, key, fallback string) string {
	if params == nil {
		return strings.TrimSpace(fallback)
	}
	if v, ok := params[key]; ok {
		switch x := v.(type) {
		case string:
			if strings.TrimSpace(x) != "" {
				return strings.TrimSpace(x)
			}
		}
	}
	return strings.TrimSpace(fallback)
}

func shouldUseExtendedGPTImageTimeout(t *model.GenerationTask, params map[string]any, routes []ProviderRoute) bool {
	if t == nil || t.Kind != string(provider.KindImage) || !strings.EqualFold(t.ModelCode, "gpt-image-2") || !shouldUseGPTWebRoute(params) {
		return false
	}
	for _, route := range routes {
		if route.Provider == model.ProviderGPT {
			return true
		}
	}
	return false
}

func isCodexOAuthAccount(acc *model.Account) bool {
	return acc != nil && acc.Provider == model.ProviderGPT && acc.AuthType == model.AuthTypeOAuth && strings.EqualFold(accountOAuthClientID(acc), codexOAuthClientID)
}

func strParamAny(p map[string]any, key, def string) string {
	if p == nil {
		return def
	}
	if v, ok := p[key]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return def
}

func parseWH(size string) (int, int) {
	parts := strings.SplitN(strings.TrimSpace(size), "x", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	var w, h int
	fmt.Sscanf(parts[0], "%d", &w)
	fmt.Sscanf(parts[1], "%d", &h)
	return w, h
}

func (s *GenerationService) markProviderFailed(ctx context.Context, acc *model.Account, reason string, desiredCooldown time.Duration) {
	if acc == nil {
		return
	}
	threshold := int64(3)
	cooldown := desiredCooldown
	if s.cfg != nil {
		threshold = s.cfg.CircuitFailureThreshold(ctx)
		if desiredCooldown > 0 {
			if sec := s.cfg.CircuitCooldownSeconds(ctx); sec > 0 {
				cooldown = time.Duration(sec) * time.Second
			}
		}
	}
	acc.ErrorCount++
	if threshold > 1 && int64(acc.ErrorCount) < threshold {
		cooldown = 0
	}
	s.pool.MarkFailed(ctx, acc.ID, reason, cooldown)
}

func (s *GenerationService) disableProviderAccount(ctx context.Context, acc *model.Account, reason string) {
	if acc == nil || s.pool == nil || s.pool.repo == nil {
		return
	}
	now := time.Now().UTC()
	fields := map[string]any{
		"status":           model.AccountStatusDisabled,
		"last_error":       truncate(reason, 240),
		"last_test_status": model.AccountTestFail,
		"last_test_error":  truncate(reason, 240),
		"last_test_at":     now,
		"cooldown_until":   nil,
		"error_count":      gorm.Expr("error_count + 1"),
	}
	if err := s.pool.repo.Update(ctx, acc.ID, fields); err != nil {
		logger.FromCtx(ctx).Warn("account.disable_failed", zap.Uint64("account_id", acc.ID), zap.Error(err))
		return
	}
	acc.Status = model.AccountStatusDisabled
	s.pool.Reload(acc.Provider)
	logger.FromCtx(ctx).Warn("account.disabled_after_oauth_refresh_401", zap.Uint64("account_id", acc.ID), zap.String("provider", acc.Provider), zap.String("reason", truncate(reason, 240)))
}

func (s *GenerationService) markProviderQuotaLimited(ctx context.Context, acc *model.Account, reason string, until time.Time) {
	if acc == nil || s.pool == nil || s.pool.repo == nil {
		return
	}
	fields := map[string]any{
		"status":      model.AccountStatusBroken,
		"last_error":  truncate(reason, 240),
		"error_count": gorm.Expr("error_count + 1"),
	}
	if until.IsZero() {
		until = time.Now().UTC().Add(24 * time.Hour)
	}
	fields["cooldown_until"] = until.UTC()
	if err := s.pool.repo.Update(ctx, acc.ID, fields); err != nil {
		logger.FromCtx(ctx).Warn("account.quota_limit_failed", zap.Uint64("account_id", acc.ID), zap.Error(err))
		return
	}
	acc.Status = model.AccountStatusBroken
	s.pool.Reload(acc.Provider)
	logger.FromCtx(ctx).Warn("account.quota_limited", zap.Uint64("account_id", acc.ID), zap.String("provider", acc.Provider), zap.Time("cooldown_until", until), zap.String("reason", truncate(reason, 240)))
}

func sleepBeforeRetry(ctx context.Context, base time.Duration, attempt int) {
	if base <= 0 || attempt <= 0 {
		return
	}
	delay := base * time.Duration(attempt)
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (s *GenerationService) updateAccountUsageMeta(ctx context.Context, acc *model.Account, t *model.GenerationTask, units int) {
	if acc == nil || units <= 0 || t == nil || acc.OAuthMeta == nil || strings.TrimSpace(*acc.OAuthMeta) == "" {
		return
	}
	if t.Kind != string(provider.KindImage) && t.Kind != string(provider.KindVideo) {
		return
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(*acc.OAuthMeta), &meta); err != nil || meta == nil {
		return
	}
	remaining, ok := metaInt(meta, "image_quota_remaining")
	if !ok {
		return
	}
	remaining -= units
	if remaining < 0 {
		remaining = 0
	}
	meta["image_quota_remaining"] = remaining
	if total, ok := metaInt(meta, "image_quota_total"); ok && total >= remaining {
		meta["image_quota_used"] = total - remaining
	}
	meta["usage_updated_at"] = time.Now().UTC().Unix()
	raw, err := json.Marshal(meta)
	if err != nil {
		return
	}
	sv := string(raw)
	if err := s.db.WithContext(ctx).Model(&model.Account{}).Where("id = ?", acc.ID).Update("oauth_meta", sv).Error; err != nil {
		logger.FromCtx(ctx).Warn("account.usage_meta_update", zap.Uint64("id", acc.ID), zap.Error(err))
		return
	}
	acc.OAuthMeta = &sv
}

func metaInt(meta map[string]any, key string) (int, bool) {
	switch v := meta[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

func (s *GenerationService) gptOAuthAccessToken(ctx context.Context, acc *model.Account, proxyURL string) (string, error) {
	at, err := s.decryptOptional(acc.AccessTokenEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt access_token failed: %w", err)
	}
	rt, err := s.decryptOptional(acc.RefreshTokenEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh_token failed: %w", err)
	}
	if rt == "" {
		rt, err = s.decryptOptional(acc.CredentialEnc)
		if err != nil {
			return "", fmt.Errorf("decrypt refresh credential failed: %w", err)
		}
	}
	if at != "" && rt == "" && !s.accessTokenNeedsRefresh(ctx, acc, at) {
		return at, nil
	}
	if at != "" && rt != "" && !s.accessTokenNeedsRefresh(ctx, acc, at) && !s.accessTokenShouldRefreshForCodex(acc) {
		return at, nil
	}
	if rt == "" {
		return "", fmt.Errorf("OAuth account missing refresh_token")
	}
	clientID, err := oauthRefreshClientID(acc)
	if err != nil {
		return "", err
	}
	oauth := NewOpenAIOAuthService(s.cfg)
	tr, err := oauth.RefreshToken(ctx, rt, clientID, proxyURL)
	if err != nil {
		return "", fmt.Errorf("refresh OAuth access_token failed: %w", err)
	}
	now := time.Now().UTC()
	updates := map[string]any{"last_refresh_at": now}
	atEnc, err := s.aes.Encrypt([]byte(strings.TrimSpace(tr.AccessToken)))
	if err != nil {
		return "", fmt.Errorf("encrypt access_token failed: %w", err)
	}
	updates["access_token_enc"] = atEnc
	if exp, ok := jwtpayload.ExpUnixFromJWT(tr.AccessToken); ok {
		t := time.Unix(exp, 0).UTC()
		updates["access_token_expires_at"] = t
	} else if tr.ExpiresIn > 0 {
		t := now.Add(time.Duration(tr.ExpiresIn) * time.Second)
		updates["access_token_expires_at"] = t
	}
	if strings.TrimSpace(tr.RefreshToken) != "" {
		rtEnc, err := s.aes.Encrypt([]byte(strings.TrimSpace(tr.RefreshToken)))
		if err != nil {
			return "", fmt.Errorf("encrypt refresh_token failed: %w", err)
		}
		updates["refresh_token_enc"] = rtEnc
		updates["credential_enc"] = rtEnc
	}
	meta := accountOAuthMeta(acc)
	meta["scope"] = tr.Scope
	meta["updated"] = now.Unix()
	if tr.IDToken != "" {
		meta["id_token_present"] = true
	}
	if raw, err := json.Marshal(meta); err == nil {
		updates["oauth_meta"] = string(raw)
	}
	if s.pool != nil && s.pool.repo != nil {
		if err := s.pool.repo.Update(ctx, acc.ID, updates); err != nil {
			return "", errcode.DBError.Wrap(err)
		}
	}
	acc.AccessTokenEnc = atEnc
	if v, ok := updates["access_token_expires_at"].(time.Time); ok {
		acc.AccessTokenExpiresAt = &v
	}
	if raw, ok := updates["oauth_meta"].(string); ok {
		acc.OAuthMeta = &raw
	}
	return strings.TrimSpace(tr.AccessToken), nil
}

func (s *GenerationService) accessTokenShouldRefreshForCodex(acc *model.Account) bool {
	if !isCodexOAuthAccount(acc) {
		return false
	}
	if acc.BaseURL != nil && strings.TrimSpace(*acc.BaseURL) != "" && !strings.Contains(strings.ToLower(*acc.BaseURL), "/codex") {
		return false
	}
	if acc.LastRefreshAt == nil {
		return true
	}
	return acc.LastRefreshAt.Before(time.Now().UTC().Add(-30 * time.Minute))
}

func oauthRefreshClientID(acc *model.Account) (string, error) {
	cid := strings.TrimSpace(accountOAuthClientID(acc))
	if isCodexOAuthAccount(acc) {
		return codexOAuthClientID, nil
	}
	if cid == "" {
		return "", fmt.Errorf("OAuth account missing client_id; ordinary ChatGPT accounts cannot fall back to Codex client_id")
	}
	return cid, nil
}

func (s *GenerationService) decryptOptional(cipher []byte) (string, error) {
	if len(cipher) == 0 {
		return "", nil
	}
	plain, err := s.aes.Decrypt(cipher)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(plain)), nil
}

func (s *GenerationService) accessTokenNeedsRefresh(ctx context.Context, acc *model.Account, at string) bool {
	if strings.TrimSpace(at) == "" {
		return true
	}
	expAt := acc.AccessTokenExpiresAt
	if expAt == nil {
		if exp, ok := jwtpayload.ExpUnixFromJWT(at); ok {
			t := time.Unix(exp, 0).UTC()
			expAt = &t
		}
	}
	if expAt == nil {
		return false
	}
	hours := int64(24)
	if s.cfg != nil {
		hours = s.cfg.RefreshBeforeHours(ctx)
	}
	return expAt.Before(time.Now().UTC().Add(time.Duration(hours) * time.Hour))
}

func (s *GenerationService) resolveProxyURL(ctx context.Context, acc *model.Account) (string, error) {
	if acc != nil && acc.ProxyID != nil {
		return s.resolveProxyURLByID(ctx, acc.ProxyID)
	}
	return s.resolveProxyURLByID(ctx, nil)
}

func (s *GenerationService) resolveProxyURLByID(ctx context.Context, proxyID *uint64) (string, error) {
	if s.proxySvc == nil || s.cfg == nil {
		return "", nil
	}
	var (
		p   *model.Proxy
		err error
	)
	if proxyID != nil {
		p, err = s.proxySvc.GetByID(ctx, *proxyID)
	} else if s.cfg.GlobalProxyEnabled(ctx) {
		if s.cfg.GlobalProxySelectionMode(ctx) == "random" {
			p, err = s.proxySvc.PickEnabledRandom(ctx)
		} else {
			p, err = s.proxySvc.GetByID(ctx, s.cfg.GlobalProxyID(ctx))
		}
	}
	if err != nil || p == nil || p.Status != model.ProxyStatusEnabled {
		return "", err
	}
	u, err := s.proxySvc.BuildURL(p)
	if err != nil || u == nil {
		return "", err
	}
	return u.String(), nil
}

func (s *GenerationService) cacheResultAssets(ctx context.Context, t *model.GenerationTask, acc *model.Account, results []*model.GenerationResult) {
	if len(results) == 0 || s.cfg == nil || s.aes == nil || acc == nil {
		return
	}
	driver := strings.ToLower(strings.TrimSpace(s.cfg.GetString(ctx, "storage.result_cache_driver", "local")))
	if driver == "off" || driver == "none" {
		return
	}
	if driver == "oss" && !s.cfg.GetBool(ctx, "oss.enabled", false) {
		driver = "local"
	}
	if driver != "local" && driver != "oss" {
		driver = "local"
	}
	plain, err := s.aes.Decrypt(acc.CredentialEnc)
	if err != nil {
		logger.FromCtx(ctx).Warn("asset.cache.decrypt_failed", zap.Error(err))
		return
	}
	cookie := buildCookieForAssetDownload(string(plain))
	for i, gr := range results {
		if u, ok := s.cacheOneAsset(ctx, driver, cookie, gr.URL, t.TaskID, i, false); ok {
			gr.URL = u
		}
		if gr.ThumbURL != nil && *gr.ThumbURL != "" {
			if u, ok := s.cacheOneAsset(ctx, driver, cookie, *gr.ThumbURL, t.TaskID, i, true); ok {
				gr.ThumbURL = &u
			}
		}
	}
}

func (s *GenerationService) cacheOneAsset(ctx context.Context, driver, cookie, rawURL, taskID string, seq int, thumb bool) (string, bool) {
	if strings.HasPrefix(strings.TrimSpace(rawURL), "data:") {
		return s.cacheDataURLAsset(ctx, driver, rawURL, taskID, seq, thumb)
	}
	source := normalizeAssetSourceURL(rawURL)
	if source == "" || strings.HasPrefix(source, "/api/v1/gen/cached/") {
		return rawURL, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return rawURL, false
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Referer", "https://grok.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		logger.FromCtx(ctx).Warn("asset.cache.download_failed", zap.String("url", source), zap.Error(err))
		return rawURL, false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		logger.FromCtx(ctx).Warn("asset.cache.bad_status", zap.String("url", source), zap.Int("status", resp.StatusCode))
		return rawURL, false
	}
	ext := assetExt(source, resp.Header.Get("Content-Type"), thumb)
	now := time.Now()
	rel := path.Join("generated", now.Format("2006"), now.Format("01"), now.Format("02"), fmt.Sprintf("%s_%d%s%s", taskID, seq, map[bool]string{true: "_thumb", false: ""}[thumb], ext))
	root := strings.TrimSpace(os.Getenv("KLEIN_STORAGE_ROOT"))
	if root == "" {
		root = "/app/storage/public"
	}
	dst := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		logger.FromCtx(ctx).Warn("asset.cache.mkdir_failed", zap.Error(err))
		return rawURL, false
	}
	f, err := os.Create(dst)
	if err != nil {
		logger.FromCtx(ctx).Warn("asset.cache.create_failed", zap.Error(err))
		return rawURL, false
	}
	defer f.Close()
	written, err := io.Copy(f, resp.Body)
	if err != nil {
		logger.FromCtx(ctx).Warn("asset.cache.write_failed", zap.Error(err))
		return rawURL, false
	}
	if written <= 0 {
		_ = f.Close()
		_ = os.Remove(dst)
		logger.FromCtx(ctx).Warn("asset.cache.empty_file", zap.String("url", source), zap.String("file", dst))
		return rawURL, false
	}
	localURL := "/api/v1/gen/cached/" + rel
	if driver == "oss" {
		if ossURL, err := s.uploadCachedAssetToOSS(ctx, dst, rel, resp.Header.Get("Content-Type")); err == nil && ossURL != "" {
			return ossURL, true
		} else if err != nil {
			logger.FromCtx(ctx).Warn("asset.cache.oss_upload_failed", zap.String("file", dst), zap.Error(err))
		}
	}
	return localURL, true
}

func (s *GenerationService) cacheDataURLAsset(ctx context.Context, driver, rawURL, taskID string, seq int, thumb bool) (string, bool) {
	contentType, payload, ok := strings.Cut(strings.TrimSpace(rawURL), ",")
	if !ok || !strings.Contains(contentType, ";base64") {
		return rawURL, false
	}
	contentType = strings.TrimPrefix(contentType, "data:")
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = contentType[:idx]
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		logger.FromCtx(ctx).Warn("asset.cache.data_url_decode_failed", zap.Error(err))
		return rawURL, false
	}
	if len(data) == 0 {
		logger.FromCtx(ctx).Warn("asset.cache.data_url_empty")
		return rawURL, false
	}
	ext := assetExt("", contentType, thumb)
	now := time.Now()
	rel := path.Join("generated", now.Format("2006"), now.Format("01"), now.Format("02"), fmt.Sprintf("%s_%d%s%s", taskID, seq, map[bool]string{true: "_thumb", false: ""}[thumb], ext))
	root := strings.TrimSpace(os.Getenv("KLEIN_STORAGE_ROOT"))
	if root == "" {
		root = "/app/storage/public"
	}
	dst := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		logger.FromCtx(ctx).Warn("asset.cache.mkdir_failed", zap.Error(err))
		return rawURL, false
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		logger.FromCtx(ctx).Warn("asset.cache.write_failed", zap.Error(err))
		return rawURL, false
	}
	localURL := "/api/v1/gen/cached/" + rel
	if driver == "oss" {
		if ossURL, err := s.uploadCachedAssetToOSS(ctx, dst, rel, contentType); err == nil && ossURL != "" {
			return ossURL, true
		} else if err != nil {
			logger.FromCtx(ctx).Warn("asset.cache.oss_upload_failed", zap.String("file", dst), zap.Error(err))
		}
	}
	return localURL, true
}

func (s *GenerationService) normalizeInputRefs(ctx context.Context, t *model.GenerationTask, refs []string) []string {
	if len(refs) == 0 || s == nil || s.cfg == nil {
		return refs
	}
	driver := strings.ToLower(strings.TrimSpace(s.cfg.GetString(ctx, "storage.result_cache_driver", "local")))
	if driver == "off" || driver == "none" {
		driver = "local"
	}
	if driver != "local" && driver != "oss" {
		driver = "local"
	}
	out := make([]string, 0, len(refs))
	for i, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if strings.HasPrefix(ref, "data:") {
			if cached, ok := s.cacheDataURLAsset(ctx, driver, ref, t.TaskID, i, false); ok && cached != "" {
				out = append(out, cached)
				continue
			}
		}
		out = append(out, ref)
	}
	return out
}

func compactLargeInlineParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = compactLargeInlineValue(v)
	}
	return out
}

func compactLargeInlineValue(v any) any {
	switch x := v.(type) {
	case string:
		if len(x) > 2048 && strings.HasPrefix(strings.TrimSpace(x), "data:image/") {
			return "[inline image cached in ref_assets]"
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = compactLargeInlineValue(x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = compactLargeInlineValue(vv)
		}
		return out
	default:
		return v
	}
}

func (s *GenerationService) uploadCachedAssetToOSS(ctx context.Context, filePath, rel, contentType string) (string, error) {
	return UploadCachedAssetToOSS(ctx, s.cfg, filePath, rel, contentType)
}

func ossObjectURL(endpoint, bucket, key string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint + "/" + escapePathSegments(key)
	}
	if !strings.HasPrefix(u.Host, bucket+".") {
		u.Host = bucket + "." + u.Host
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + escapePathSegments(key)
	u.RawQuery = ""
	return u.String()
}

func escapePathSegments(v string) string {
	parts := strings.Split(v, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func normalizeAssetSourceURL(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "data:") {
		return ""
	}
	if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		return v
	}
	return "https://assets.grok.com/" + strings.TrimLeft(v, "/")
}

func assetExt(source, contentType string, thumb bool) string {
	lower := strings.ToLower(source)
	for _, ext := range []string{".mp4", ".webm", ".png", ".jpg", ".jpeg", ".webp"} {
		if strings.Contains(lower, ext) {
			if ext == ".jpeg" {
				return ".jpg"
			}
			return ext
		}
	}
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "video/webm"):
		return ".webm"
	case strings.Contains(ct, "video/"):
		return ".mp4"
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "webp"):
		return ".webp"
	case thumb:
		return ".jpg"
	default:
		return ".bin"
	}
}

func buildCookieForAssetDownload(cred string) string {
	cred = strings.TrimSpace(cred)
	if strings.Contains(cred, "=") {
		if !strings.Contains(cred, "sso-rw=") {
			if token := extractCookieValue(cred, "sso"); token != "" {
				cred = strings.TrimRight(cred, "; ") + "; sso-rw=" + token
			}
		}
		return cred
	}
	return "sso=" + cred + "; sso-rw=" + cred
}

func extractCookieValue(cookie, name string) string {
	prefix := name + "="
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, prefix) {
			return strings.TrimPrefix(part, prefix)
		}
	}
	return ""
}

func (s *GenerationService) failTask(ctx context.Context, t *model.GenerationTask, reason string) {
	displayReason := userFacingGenerationError(reason)
	if err := s.repo.SetFailed(ctx, t.TaskID, displayReason); err != nil {
		logger.FromCtx(ctx).Warn("gen.fail.update_status", zap.Error(err))
	}
	_ = s.repo.MergeParams(ctx, t.TaskID, map[string]any{
		PricingAuditSnapshotKey: PricingFailureRefundPatch(t.CostPoints, displayReason),
	})
	if t.CostPoints > 0 {
		if err := s.billing.FailRefund(ctx, t.TaskID, displayReason); err != nil {
			logger.FromCtx(ctx).Warn("gen.fail.refund", zap.Error(err))
		}
	}
}

// ReapStaleTasks closes tasks that were left pending/running after a restart or
// a killed provider request. Normal in-flight jobs have much shorter context
// deadlines than these cutoffs, so this only catches genuinely abandoned rows.
func (s *GenerationService) ReapStaleTasks(ctx context.Context, userID uint64) {
	if s == nil || s.db == nil {
		return
	}
	now := time.Now().UTC()
	cutoff := now.Add(-1 * time.Hour)
	var tasks []*model.GenerationTask
	q := s.db.WithContext(ctx).
		Where("deleted_at IS NULL AND status IN ?", []int8{model.GenStatusPending, model.GenStatusRunning}).
		Where("(started_at IS NOT NULL AND started_at < ?) OR (started_at IS NULL AND created_at < ?)", cutoff, cutoff).
		Order("id ASC").
		Limit(200)
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Find(&tasks).Error; err != nil {
		logger.FromCtx(ctx).Warn("gen.stale.query_failed", zap.Error(err))
		return
	}
	for _, t := range tasks {
		s.failTask(ctx, t, "任务执行超时，已自动结束")
	}
}

// === helpers ===

func intPtr(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// newULID 生成一个 26 字符 ULID（Crockford base32 简化版）。
//
// 用 UUID 转 hex 后截 26 位（在严格 ULID 库引入前的过渡方案）。
func newULID() string {
	id := uuid.NewString()
	clean := ""
	for i := 0; i < len(id); i++ {
		ch := id[i]
		if ch == '-' {
			continue
		}
		clean += string(ch)
		if len(clean) == 26 {
			break
		}
	}
	return clean
}

var _ = errors.New

var usageLimitResetAtRe = regexp.MustCompile(`"resets_at"\s*:\s*([0-9]+)`)

func isFatalOAuthRefreshError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "refresh oauth access_token failed") {
		return false
	}
	return strings.Contains(msg, " 401") ||
		strings.Contains(msg, "返回 401") ||
		strings.Contains(msg, "already been used") ||
		strings.Contains(msg, "please try signing in again") ||
		strings.Contains(msg, "invalid_request_error")
}

func retryableProviderError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return isFatalOAuthRefreshError(err) ||
		isUsageLimitReachedError(err) ||
		strings.Contains(msg, "http 429") ||
		strings.Contains(msg, "too many requests") ||
		isRetryableImageProviderError(msg) ||
		isGrokRetryableForbiddenError(msg)
}

func isTransientProviderPathError(provider string, err error) bool {
	if err == nil || provider != model.ProviderGROK {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 403") && isGrokRetryableForbiddenError(msg)
}

func isGrokRetryableForbiddenError(msg string) bool {
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "grok upload http 403") ||
		strings.Contains(msg, "grok video http 403") ||
		strings.Contains(msg, "grok media post http 403") ||
		strings.Contains(msg, "grok http 403") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "cloudflare") ||
		strings.Contains(msg, "just a moment") ||
		strings.Contains(msg, "request rejected by anti-bot rules")
}

func isUsageLimitReachedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "usage_limit_reached") ||
		strings.Contains(msg, "the usage limit has been reached") ||
		strings.Contains(msg, "insufficient_quota") ||
		strings.Contains(msg, "quota exceeded") ||
		strings.Contains(msg, "exceeded your current quota") ||
		strings.Contains(msg, "\"plan_type\":\"free\"") ||
		strings.Contains(msg, "\"plan_type\": \"free\"")
}

func isRetryableImageProviderError(msg string) bool {
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "reference image download") {
		return false
	}
	if strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "client timeout") ||
		strings.Contains(msg, "timeout awaiting response") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "temporary failure") ||
		strings.Contains(msg, "eof") {
		return true
	}
	if containsProviderStatus(msg, 500) ||
		containsProviderStatus(msg, 502) ||
		containsProviderStatus(msg, 503) ||
		containsProviderStatus(msg, 504) {
		return true
	}
	if strings.Contains(msg, "<!doctype html") ||
		strings.Contains(msg, "no healthy upstream") ||
		strings.Contains(msg, "bad gateway") ||
		strings.Contains(msg, "service unavailable") ||
		strings.Contains(msg, "gateway timeout") ||
		strings.Contains(msg, "returned 0 image") {
		return true
	}
	if strings.Contains(msg, "image generation is not enabled") ||
		strings.Contains(msg, "current api key does not allow image generation") ||
		strings.Contains(msg, "does not support image generation") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "invalid api key") ||
		strings.Contains(msg, "invalid_api_key") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "incorrect api key") {
		return true
	}
	if strings.Contains(msg, "gpt image2 nova 404") ||
		strings.Contains(msg, "gpt image2 images api 404") ||
		strings.Contains(msg, "gpt image2 images edits 404") ||
		strings.Contains(msg, "model_not_found") ||
		strings.Contains(msg, "model not found") ||
		strings.Contains(msg, "unsupported model") {
		return true
	}
	return strings.Contains(msg, "unsupported size") ||
		strings.Contains(msg, "invalid size") ||
		strings.Contains(msg, "invalid value for 'size'") ||
		strings.Contains(msg, "unsupported quality") ||
		strings.Contains(msg, "invalid quality")
}

func containsProviderStatus(msg string, status int) bool {
	code := strconv.Itoa(status)
	return strings.Contains(msg, "http "+code) ||
		strings.Contains(msg, " "+code+":") ||
		strings.Contains(msg, " "+code+" ") ||
		strings.Contains(msg, "status "+code)
}

func usageLimitResetAt(err error) time.Time {
	if err == nil {
		return time.Time{}
	}
	m := usageLimitResetAtRe.FindStringSubmatch(err.Error())
	if len(m) != 2 {
		return time.Time{}
	}
	sec, e := strconv.ParseInt(m[1], 10, 64)
	if e != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

func providerCooldown(err error) time.Duration {
	if err == nil {
		return 5 * time.Minute
	}
	msg := strings.ToLower(err.Error())
	if isGrokRetryableForbiddenError(msg) {
		return 0
	}
	switch {
	case strings.Contains(msg, "http 429"), strings.Contains(msg, "too many requests"):
		return 30 * time.Minute
	case strings.Contains(msg, "http 403"), strings.Contains(msg, "forbidden"),
		strings.Contains(msg, "cloudflare"), strings.Contains(msg, "just a moment"),
		strings.Contains(msg, "anti-bot"), strings.Contains(msg, "request rejected"):
		return 2 * time.Hour
	case strings.Contains(msg, "anti-bot"), strings.Contains(msg, "request rejected"):
		return 2 * time.Hour
	default:
		return 10 * time.Minute
	}
}

func userFacingGenerationError(reason string) string {
	msg := strings.ToLower(reason)
	switch {
	case strings.Contains(msg, "just a moment"), strings.Contains(msg, "cloudflare"):
		return "GROK 触发了 Cloudflare 验证，请配置可用的 CF Cookie/代理后再试"
	case strings.Contains(msg, "grok video http 429"), strings.Contains(msg, "too many requests"):
		return "GROK 视频生成频率受限，请稍后重试，或更换可用账号/代理后再试"
	case strings.Contains(msg, "anti-bot"), strings.Contains(msg, "request rejected"):
		return "GROK 风控拦截了本次请求，请更换代理或稍后重试"
	default:
		return reason
	}
}
