package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
	grokweb "github.com/kleinai/backend/internal/provider/grok"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/logger"
	"github.com/kleinai/backend/pkg/outbound"
	"github.com/kleinai/backend/pkg/ratelimit"
)

type ChatService struct {
	db              *gorm.DB
	repo            *repo.GenerationRepo
	pool            *AccountPool
	billing         *BillingService
	priceFn         func(modelCode string) ChatPrice
	cfg             *SystemConfigService
	routeSvc        *ProviderRouteService
	modelRepo       *repo.ModelCatalogRepo
	modelSourceRepo *repo.ModelSourceRepo
	apiChannelRepo  *repo.APIChannelRepo
	aes             *crypto.AESGCM
	proxySvc        *ProxyService
	apiLimiter      apiChannelDistributedLimiter
	client          *http.Client
	grok            *grokweb.WebClient
	mock            bool
	grokMock        bool
}

const (
	chatProviderAPIChannel = "api_channel"
)

type chatRuntimeRoute struct {
	SourceType    string
	SourceCode    string
	SourceName    string
	Provider      string
	UpstreamModel string
	Adapter       string
	Strategy      string
	AuthType      string
	Weight        int
	Priority      int
	SkipReason    string
	APIChannel    *model.APIChannel
	RouteIndex    int
	Attempt       int
}

type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatCallRequest struct {
	UserID   uint64
	APIKeyID *uint64
	ClientIP string
	IdemKey  string
	Body     map[string]any
	RawBody  []byte
}

func NewChatService(db *gorm.DB, r *repo.GenerationRepo, pool *AccountPool, billing *BillingService, cfg *SystemConfigService, routeSvc *ProviderRouteService, modelRepo *repo.ModelCatalogRepo, modelSourceRepo *repo.ModelSourceRepo, apiChannelRepo *repo.APIChannelRepo, aes *crypto.AESGCM, proxySvc *ProxyService, apiLimiters ...*ratelimit.Limiter) *ChatService {
	return &ChatService{
		db:              db,
		repo:            r,
		pool:            pool,
		billing:         billing,
		priceFn:         ModelGatewayChatPriceFn(cfg, modelRepo),
		cfg:             cfg,
		routeSvc:        routeSvc,
		modelRepo:       modelRepo,
		modelSourceRepo: modelSourceRepo,
		apiChannelRepo:  apiChannelRepo,
		aes:             aes,
		proxySvc:        proxySvc,
		apiLimiter:      optionalAPIChannelDistributedLimiter(apiLimiters),
		client:          &http.Client{Timeout: 10 * time.Minute},
		grok:            grokweb.NewWebClient(os.Getenv("KLEIN_GROK_BASE_URL")),
		mock:            !isLiveProvider(os.Getenv("KLEIN_PROVIDER_GPT")),
		grokMock:        !isLiveProvider(os.Getenv("KLEIN_PROVIDER_GROK")),
	}
}

func (s *ChatService) Complete(ctx context.Context, req ChatCallRequest) ([]byte, int, error) {
	modelCode := strAny(req.Body["model"], "gpt-4o-mini")
	routes := s.chatRuntimeRoutes(ctx, modelCode)
	if len(routes) == 0 {
		return nil, http.StatusBadGateway, fmt.Errorf("no available chat route for model %s", modelCode)
	}
	skipped := s.chatRuntimeSkippedCandidates(ctx, modelCode)
	var lastRaw []byte
	var lastStatus int
	var lastErr error
	for i, route := range routes {
		route.RouteIndex = i + 1
		route.Attempt = i + 1
		attemptReq := req
		attemptReq.Body = cloneChatBody(req.Body)
		if i > 0 && req.IdemKey != "" {
			attemptReq.IdemKey = fmt.Sprintf("%s-route-%d", req.IdemKey, i+1)
		}
		raw, status, err := s.completeRuntimeRoute(ctx, attemptReq, modelCode, route, routes, skipped, i+1)
		if err == nil {
			return raw, status, nil
		}
		lastRaw, lastStatus, lastErr = raw, status, err
		if !retryableChatFallback(status, err) {
			return raw, status, err
		}
	}
	if lastRaw != nil && lastStatus > 0 {
		return lastRaw, lastStatus, nil
	}
	if lastStatus == 0 {
		lastStatus = http.StatusBadGateway
	}
	return nil, lastStatus, lastErr
}

func (s *ChatService) completeRuntimeRoute(ctx context.Context, req ChatCallRequest, modelCode string, route chatRuntimeRoute, routes []chatRuntimeRoute, skipped []chatRuntimeRoute, selectedIndex int) ([]byte, int, error) {
	snapshot := chatRuntimeRouteSnapshotPayload(modelCode, routes, selectedIndex, skipped)
	if route.SourceType == model.ModelSourceTypeAPIChannel {
		return s.completeAPIChannelRoute(ctx, req, modelCode, route, snapshot)
	}
	return s.completeRoute(ctx, req, modelCode, ProviderRoute{
		SourceType:    route.SourceType,
		SourceCode:    route.SourceCode,
		Adapter:       route.Adapter,
		Provider:      route.Provider,
		UpstreamModel: route.UpstreamModel,
		Strategy:      route.Strategy,
		AuthType:      route.AuthType,
		RouteIndex:    route.RouteIndex,
		Attempt:       route.Attempt,
	}, snapshot)
}

func (s *ChatService) completeRoute(ctx context.Context, req ChatCallRequest, modelCode string, route ProviderRoute, routeSnapshot any) ([]byte, int, error) {
	if route.Provider == model.ProviderGROK {
		return s.completeGrok(ctx, req, modelCode, route)
	}
	req.Body["model"] = route.UpstreamModel
	req.Body["stream"] = false
	prompt := chatPrompt(req.Body)
	estimate := s.estimateCost(modelCode, req.Body)
	if s.mock {
		return s.completeMockWithProvider(ctx, req, modelCode, prompt, estimate, route.Provider)
	}
	t, acc, err := s.prepare(ctx, req, modelCode, prompt, estimate, route.Provider, route.Strategy, route.AuthType, route.UpstreamModel, routeSnapshot)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}

	raw, status, usage, err := s.callJSON(ctx, t, acc, route.Provider, "chat.completions", req.Body, map[string]any{
		"model_gateway_source_type": model.ModelSourceTypeAccountPool,
		"model_gateway_source_code": chatRouteSourceCode(route.SourceCode, route.Provider),
		"upstream_model":            route.UpstreamModel,
		"strategy":                  normalizeRouteStrategy(route.Strategy),
		"auth_type":                 route.AuthType,
		"model_gateway_route_index": route.RouteIndex,
		"model_gateway_attempt":     route.Attempt,
	})
	if err != nil {
		s.fail(ctx, t, err.Error())
		return nil, status, err
	}
	if status >= 400 {
		msg := fmt.Sprintf("upstream http %d: %s", status, snippet(raw, 240))
		s.fail(ctx, t, msg)
		if retryableChatHTTPStatus(status) {
			return raw, status, fmt.Errorf("chat upstream retryable failure: %s", msg)
		}
		return raw, status, nil
	}
	outputSnapshot := chatOutputSnapshotFromRaw(false, raw, usage)
	if !chatOutputSnapshotProvesOutput(outputSnapshot) {
		s.recordChatOutputSnapshot(ctx, t, outputSnapshot)
		msg := "chat upstream returned success without assistant output"
		s.fail(ctx, t, msg)
		return raw, http.StatusBadGateway, fmt.Errorf("%s", msg)
	}
	actual := estimate
	if v, ok := ChatActualCost(s.priceFn(modelCode), req.Body, usage, outputSnapshot); ok {
		actual = v
	}
	_ = s.repo.UpdateCost(ctx, t.TaskID, actual)
	if t.CostPoints > 0 || actual > 0 {
		if err := s.billing.FinalizeUsage(ctx, t.TaskID, actual, &acc.ID); err != nil {
			s.fail(ctx, t, err.Error())
			return nil, http.StatusBadRequest, err
		}
	}
	s.recordChatPricingResult(ctx, t, usage, actual, outputSnapshot)
	s.recordChatOutputSnapshot(ctx, t, outputSnapshot)
	_ = s.repo.SetSucceeded(ctx, t.TaskID, nil)
	return raw, status, nil
}

func (s *ChatService) completeAPIChannelRoute(ctx context.Context, req ChatCallRequest, modelCode string, route chatRuntimeRoute, routeSnapshot any) ([]byte, int, error) {
	if route.APIChannel == nil {
		return nil, http.StatusBadGateway, fmt.Errorf("api channel route missing channel")
	}
	req.Body["model"] = route.UpstreamModel
	req.Body["stream"] = false
	prompt := chatPrompt(req.Body)
	estimate := s.estimateCost(modelCode, req.Body)
	t, err := s.prepareAPIChannel(ctx, req, modelCode, prompt, estimate, route, routeSnapshot)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}

	raw, status, usage, err := s.callAPIChannelJSON(ctx, t, route, req.Body)
	if err != nil {
		s.fail(ctx, t, err.Error())
		return nil, status, err
	}
	if status >= 400 {
		msg := fmt.Sprintf("api channel http %d: %s", status, snippet(raw, 240))
		s.fail(ctx, t, msg)
		if retryableChatHTTPStatus(status) {
			return raw, status, fmt.Errorf("api channel retryable failure: %s", msg)
		}
		return raw, status, nil
	}
	outputSnapshot := chatOutputSnapshotFromRaw(false, raw, usage)
	if !chatOutputSnapshotProvesOutput(outputSnapshot) {
		s.recordChatOutputSnapshot(ctx, t, outputSnapshot)
		msg := "api channel returned success without assistant output"
		s.fail(ctx, t, msg)
		return raw, http.StatusBadGateway, fmt.Errorf("%s", msg)
	}
	actual := estimate
	if v, ok := ChatActualCost(s.priceFn(modelCode), req.Body, usage, outputSnapshot); ok {
		actual = v
	}
	_ = s.repo.UpdateCost(ctx, t.TaskID, actual)
	if t.CostPoints > 0 || actual > 0 {
		if err := s.billing.FinalizeUsage(ctx, t.TaskID, actual, nil); err != nil {
			s.fail(ctx, t, err.Error())
			return nil, http.StatusBadRequest, err
		}
	}
	s.recordChatPricingResult(ctx, t, usage, actual, outputSnapshot)
	s.recordChatOutputSnapshot(ctx, t, outputSnapshot)
	_ = s.repo.SetSucceeded(ctx, t.TaskID, nil)
	return raw, status, nil
}

func (s *ChatService) Stream(ctx context.Context, req ChatCallRequest, w http.ResponseWriter) error {
	modelCode := strAny(req.Body["model"], "gpt-4o-mini")
	routes := s.chatRuntimeRoutes(ctx, modelCode)
	if len(routes) == 0 {
		return fmt.Errorf("no available chat route for model %s", modelCode)
	}
	skipped := s.chatRuntimeSkippedCandidates(ctx, modelCode)
	var lastRaw []byte
	var lastStatus int
	var lastErr error
	for i, route := range routes {
		route.RouteIndex = i + 1
		route.Attempt = i + 1
		attemptReq := req
		attemptReq.Body = cloneChatBody(req.Body)
		if i > 0 && req.IdemKey != "" {
			attemptReq.IdemKey = fmt.Sprintf("%s-stream-route-%d", req.IdemKey, i+1)
		}
		started, status, raw, err := s.streamRuntimeRoute(ctx, attemptReq, modelCode, route, routes, skipped, i+1, w)
		if err == nil {
			return nil
		}
		lastRaw, lastStatus, lastErr = raw, status, err
		if started {
			return err
		}
		if !retryableChatFallback(status, err) {
			if len(raw) > 0 && status > 0 {
				writeChatJSONError(w, status, raw)
				return nil
			}
			return err
		}
	}
	if len(lastRaw) > 0 && lastStatus > 0 {
		writeChatJSONError(w, lastStatus, lastRaw)
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no available chat route for model %s", modelCode)
}

func (s *ChatService) streamRuntimeRoute(ctx context.Context, req ChatCallRequest, modelCode string, route chatRuntimeRoute, routes []chatRuntimeRoute, skipped []chatRuntimeRoute, selectedIndex int, w http.ResponseWriter) (bool, int, []byte, error) {
	snapshot := chatRuntimeRouteSnapshotPayload(modelCode, routes, selectedIndex, skipped)
	if route.SourceType == model.ModelSourceTypeAPIChannel {
		return s.streamAPIChannelRoute(ctx, req, modelCode, route, snapshot, w)
	}
	return s.streamAccountPoolRoute(ctx, req, modelCode, ProviderRoute{
		SourceType:    route.SourceType,
		SourceCode:    route.SourceCode,
		Adapter:       route.Adapter,
		Provider:      route.Provider,
		UpstreamModel: route.UpstreamModel,
		Strategy:      route.Strategy,
		AuthType:      route.AuthType,
		RouteIndex:    route.RouteIndex,
		Attempt:       route.Attempt,
	}, snapshot, w)
}

func (s *ChatService) streamAccountPoolRoute(ctx context.Context, req ChatCallRequest, modelCode string, route ProviderRoute, routeSnapshot any, w http.ResponseWriter) (bool, int, []byte, error) {
	if route.Provider == model.ProviderGROK {
		return s.streamGrokRoute(ctx, req, modelCode, route, routeSnapshot, w)
	}
	req.Body["model"] = route.UpstreamModel
	req.Body["stream"] = true
	req.Body["stream_options"] = map[string]any{"include_usage": true}
	prompt := chatPrompt(req.Body)
	estimate := s.estimateCost(modelCode, req.Body)
	if s.mock {
		return true, http.StatusOK, nil, s.streamMockWithProvider(ctx, req, modelCode, prompt, estimate, route.Provider, w)
	}
	t, acc, err := s.prepare(ctx, req, modelCode, prompt, estimate, route.Provider, route.Strategy, route.AuthType, route.UpstreamModel, routeSnapshot)
	if err != nil {
		return false, http.StatusBadRequest, nil, err
	}

	startedAt := time.Now()
	url := accountChatEndpoint(acc)
	meta := map[string]any{
		"model_gateway_source_type": model.ModelSourceTypeAccountPool,
		"model_gateway_source_code": chatRouteSourceCode(route.SourceCode, route.Provider),
		"upstream_model":            route.UpstreamModel,
		"strategy":                  normalizeRouteStrategy(route.Strategy),
		"auth_type":                 route.AuthType,
		"model_gateway_route_index": route.RouteIndex,
		"model_gateway_attempt":     route.Attempt,
	}
	resp, err := s.openUpstream(ctx, acc, req.Body)
	if err != nil {
		s.logChatUpstream(ctx, t, acc, route.Provider, "chat.completions.stream", http.MethodPost, url, 0, time.Since(startedAt), chatRequestExcerpt(req.Body), nil, err, meta)
		s.fail(ctx, t, err.Error())
		return false, http.StatusBadGateway, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		s.logChatUpstream(ctx, t, acc, route.Provider, "chat.completions.stream", http.MethodPost, url, resp.StatusCode, time.Since(startedAt), chatRequestExcerpt(req.Body), raw, nil, meta)
		msg := fmt.Sprintf("upstream http %d: %s", resp.StatusCode, snippet(raw, 240))
		s.fail(ctx, t, msg)
		return false, resp.StatusCode, raw, fmt.Errorf("chat stream upstream failure: %s", msg)
	}
	s.logChatUpstream(ctx, t, acc, route.Provider, "chat.completions.stream", http.MethodPost, url, resp.StatusCode, time.Since(startedAt), chatRequestExcerpt(req.Body), nil, nil, meta)

	usage, outputSnapshot, err := forwardChatStream(resp.Body, w)
	if err != nil {
		s.fail(ctx, t, err.Error())
		return true, http.StatusOK, nil, err
	}
	if !chatOutputSnapshotProvesOutput(outputSnapshot) {
		s.recordChatOutputSnapshot(context.Background(), t, outputSnapshot)
		msg := "chat stream completed without assistant output"
		s.fail(context.Background(), t, msg)
		return true, http.StatusOK, nil, fmt.Errorf("%s", msg)
	}
	actual := estimate
	if v, ok := ChatActualCost(s.priceFn(modelCode), req.Body, usage, outputSnapshot); ok {
		actual = v
	}
	_ = s.repo.UpdateCost(context.Background(), t.TaskID, actual)
	if t.CostPoints > 0 || actual > 0 {
		if err := s.billing.FinalizeUsage(context.Background(), t.TaskID, actual, &acc.ID); err != nil {
			s.fail(context.Background(), t, err.Error())
			return true, http.StatusOK, nil, err
		}
	}
	s.recordChatPricingResult(context.Background(), t, usage, actual, outputSnapshot)
	s.recordChatOutputSnapshot(context.Background(), t, outputSnapshot)
	_ = s.repo.SetSucceeded(context.Background(), t.TaskID, nil)
	return true, http.StatusOK, nil, nil
}

func (s *ChatService) streamAPIChannelRoute(ctx context.Context, req ChatCallRequest, modelCode string, route chatRuntimeRoute, routeSnapshot any, w http.ResponseWriter) (bool, int, []byte, error) {
	if route.APIChannel == nil {
		return false, http.StatusBadGateway, nil, fmt.Errorf("api channel route missing channel")
	}
	req.Body["model"] = route.UpstreamModel
	req.Body["stream"] = true
	req.Body["stream_options"] = map[string]any{"include_usage": true}
	prompt := chatPrompt(req.Body)
	estimate := s.estimateCost(modelCode, req.Body)
	t, err := s.prepareAPIChannel(ctx, req, modelCode, prompt, estimate, route, routeSnapshot)
	if err != nil {
		return false, http.StatusBadRequest, nil, err
	}

	startedAt := time.Now()
	ch := route.APIChannel
	url := apiChannelChatEndpoint(ch)
	meta := chatAPIChannelMeta(route)
	resp, credRef, err := s.openAPIChannelUpstream(ctx, ch, req.Body)
	addAPIChannelCredentialMeta(meta, credRef)
	if err != nil {
		recordAPIChannelCredentialError(ctx, s.apiChannelRepo, credRef, err)
		s.logChatUpstream(ctx, t, nil, chatProviderAPIChannel, "api_channel.chat.stream", http.MethodPost, url, 0, time.Since(startedAt), chatRequestExcerpt(req.Body), nil, err, meta)
		s.fail(ctx, t, err.Error())
		return false, http.StatusBadGateway, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		recordAPIChannelCredentialHTTPFailure(ctx, s.apiChannelRepo, credRef, resp.StatusCode, raw)
		s.logChatUpstream(ctx, t, nil, chatProviderAPIChannel, "api_channel.chat.stream", http.MethodPost, url, resp.StatusCode, time.Since(startedAt), chatRequestExcerpt(req.Body), raw, nil, meta)
		msg := fmt.Sprintf("api channel stream http %d: %s", resp.StatusCode, snippet(raw, 240))
		s.fail(ctx, t, msg)
		return false, resp.StatusCode, raw, fmt.Errorf("api channel stream failure: %s", msg)
	}
	s.logChatUpstream(ctx, t, nil, chatProviderAPIChannel, "api_channel.chat.stream", http.MethodPost, url, resp.StatusCode, time.Since(startedAt), chatRequestExcerpt(req.Body), nil, nil, meta)

	usage, outputSnapshot, err := forwardChatStream(resp.Body, w)
	if err != nil {
		recordAPIChannelCredentialError(ctx, s.apiChannelRepo, credRef, err)
		s.fail(ctx, t, err.Error())
		return true, http.StatusOK, nil, err
	}
	if !chatOutputSnapshotProvesOutput(outputSnapshot) {
		s.recordChatOutputSnapshot(context.Background(), t, outputSnapshot)
		msg := "api channel stream completed without assistant output"
		recordAPIChannelCredentialError(context.Background(), s.apiChannelRepo, credRef, fmt.Errorf("%s", msg))
		s.fail(context.Background(), t, msg)
		return true, http.StatusOK, nil, fmt.Errorf("%s", msg)
	}
	recordAPIChannelCredentialSuccess(ctx, s.apiChannelRepo, credRef)
	actual := estimate
	if v, ok := ChatActualCost(s.priceFn(modelCode), req.Body, usage, outputSnapshot); ok {
		actual = v
	}
	_ = s.repo.UpdateCost(context.Background(), t.TaskID, actual)
	if t.CostPoints > 0 || actual > 0 {
		if err := s.billing.FinalizeUsage(context.Background(), t.TaskID, actual, nil); err != nil {
			s.fail(context.Background(), t, err.Error())
			return true, http.StatusOK, nil, err
		}
	}
	s.recordChatPricingResult(context.Background(), t, usage, actual, outputSnapshot)
	s.recordChatOutputSnapshot(context.Background(), t, outputSnapshot)
	_ = s.repo.SetSucceeded(context.Background(), t.TaskID, nil)
	return true, http.StatusOK, nil, nil
}

func (s *ChatService) completeGrok(ctx context.Context, req ChatCallRequest, modelCode string, route ProviderRoute) ([]byte, int, error) {
	req.Body["model"] = route.UpstreamModel
	req.Body["stream"] = false
	prompt := chatPrompt(req.Body)
	estimate := s.estimateCost(modelCode, req.Body)
	if s.grokMock {
		return s.completeMockWithProvider(ctx, req, modelCode, prompt, estimate, route.Provider)
	}
	maxAttempts := 10
	retryDelay := 800 * time.Millisecond
	if s.cfg != nil {
		maxAttempts = s.cfg.RetryMaxAttempts(ctx)
		retryDelay = s.cfg.RetryBaseDelay(ctx)
	}
	var lastRaw []byte
	var lastStatus int
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptReq := req
		if attempt > 1 && req.IdemKey != "" {
			attemptReq.IdemKey = fmt.Sprintf("%s-retry-%d", req.IdemKey, attempt)
		}
		t, acc, err := s.prepare(ctx, attemptReq, modelCode, prompt, estimate, route.Provider, route.Strategy, route.AuthType, route.UpstreamModel, providerRouteSnapshotPayload(modelCode, string(provider.KindChat), []ProviderRoute{route}, 1))
		if err != nil {
			if lastErr != nil {
				return nil, http.StatusBadGateway, lastErr
			}
			return nil, http.StatusBadRequest, err
		}

		cred, err := s.credential(acc)
		if err != nil {
			lastErr = err
			s.fail(ctx, t, err.Error())
			s.pool.MarkFailed(ctx, acc.ID, err.Error(), 30*time.Minute)
			sleepBeforeRetry(ctx, retryDelay, attempt)
			continue
		}
		grok := s.grok
		proxyURL, perr := s.resolveProxyURL(ctx, acc)
		if perr != nil {
			logger.FromCtx(ctx).Warn("chat.grok.resolve_proxy", zap.Error(perr))
		}
		if proxyURL != "" || (acc.BaseURL != nil && *acc.BaseURL != "") {
			base := os.Getenv("KLEIN_GROK_BASE_URL")
			if acc.BaseURL != nil && *acc.BaseURL != "" {
				base = *acc.BaseURL
			}
			grok = grokweb.NewWebClientWithProxy(base, proxyURL)
		}
		res, err := grok.ChatComplete(ctx, cred, route.UpstreamModel, req.Body)
		if err != nil {
			lastErr = err
			s.fail(ctx, t, err.Error())
			if isTransientProviderPathError(acc.Provider, err) {
				s.pool.MarkTransientFailed(ctx, acc.ID, err.Error())
			} else {
				s.pool.MarkFailed(ctx, acc.ID, err.Error(), providerCooldown(err))
			}
			if attempt < maxAttempts && retryableProviderError(err) {
				logger.FromCtx(ctx).Warn("chat.grok.retrying_with_next_account", zap.Int("attempt", attempt), zap.Uint64("account_id", acc.ID), zap.Error(err))
				sleepBeforeRetry(ctx, retryDelay, attempt)
				continue
			}
			return nil, http.StatusBadGateway, err
		}
		if res.Status >= 400 {
			lastRaw = res.Raw
			lastStatus = res.Status
			lastErr = fmt.Errorf("grok chat http %d: %s", res.Status, snippet(res.Raw, 240))
			s.fail(ctx, t, lastErr.Error())
			if isTransientProviderPathError(acc.Provider, lastErr) {
				s.pool.MarkTransientFailed(ctx, acc.ID, lastErr.Error())
			} else {
				s.pool.MarkFailed(ctx, acc.ID, lastErr.Error(), providerCooldown(lastErr))
			}
			if attempt < maxAttempts && retryableProviderError(lastErr) {
				logger.FromCtx(ctx).Warn("chat.grok.retrying_with_next_account", zap.Int("attempt", attempt), zap.Uint64("account_id", acc.ID), zap.Error(lastErr))
				sleepBeforeRetry(ctx, retryDelay, attempt)
				continue
			}
			return res.Raw, res.Status, nil
		}
		usage := chatUsageFromGrokUsage(res.Usage)
		outputSnapshot := chatOutputSnapshotFromRaw(false, res.Raw, usage)
		actual := estimate
		if v, ok := ChatActualCost(s.priceFn(modelCode), req.Body, usage, outputSnapshot); ok {
			actual = v
		}
		_ = s.repo.UpdateCost(ctx, t.TaskID, actual)
		if t.CostPoints > 0 || actual > 0 {
			if err := s.billing.FinalizeUsage(ctx, t.TaskID, actual, &acc.ID); err != nil {
				s.fail(ctx, t, err.Error())
				return nil, http.StatusBadRequest, err
			}
		}
		s.recordChatPricingResult(ctx, t, usage, actual, outputSnapshot)
		s.recordChatOutputSnapshot(ctx, t, outputSnapshot)
		s.pool.MarkUsed(ctx, acc.ID)
		_ = s.repo.SetSucceeded(ctx, t.TaskID, nil)
		return res.Raw, res.Status, nil
	}
	if lastRaw != nil && lastStatus > 0 {
		return lastRaw, lastStatus, nil
	}
	return nil, http.StatusBadGateway, lastErr
}

func (s *ChatService) streamGrok(ctx context.Context, req ChatCallRequest, modelCode string, route ProviderRoute, w http.ResponseWriter) error {
	_, _, _, err := s.streamGrokRoute(ctx, req, modelCode, route, providerRouteSnapshotPayload(modelCode, string(provider.KindChat), []ProviderRoute{route}, 1), w)
	return err
}

func (s *ChatService) streamGrokRoute(ctx context.Context, req ChatCallRequest, modelCode string, route ProviderRoute, routeSnapshot any, w http.ResponseWriter) (bool, int, []byte, error) {
	req.Body["model"] = route.UpstreamModel
	req.Body["stream"] = true
	prompt := chatPrompt(req.Body)
	estimate := s.estimateCost(modelCode, req.Body)
	if s.grokMock {
		return true, http.StatusOK, nil, s.streamMockWithProvider(ctx, req, modelCode, prompt, estimate, route.Provider, w)
	}
	t, acc, err := s.prepare(ctx, req, modelCode, prompt, estimate, route.Provider, route.Strategy, route.AuthType, route.UpstreamModel, routeSnapshot)
	if err != nil {
		return false, http.StatusBadRequest, nil, err
	}
	cred, err := s.credential(acc)
	if err != nil {
		s.fail(ctx, t, err.Error())
		return false, http.StatusBadGateway, nil, err
	}
	grok := s.grok
	proxyURL, perr := s.resolveProxyURL(ctx, acc)
	if perr != nil {
		logger.FromCtx(ctx).Warn("chat.grok.resolve_proxy", zap.Error(perr))
	}
	if proxyURL != "" || (acc.BaseURL != nil && *acc.BaseURL != "") {
		base := os.Getenv("KLEIN_GROK_BASE_URL")
		if acc.BaseURL != nil && *acc.BaseURL != "" {
			base = *acc.BaseURL
		}
		grok = grokweb.NewWebClientWithProxy(base, proxyURL)
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	usage, err := grok.ChatStream(ctx, cred, route.UpstreamModel, req.Body, w, flusher)
	if err != nil {
		s.fail(ctx, t, err.Error())
		return true, http.StatusOK, nil, err
	}
	chatUsage := chatUsageFromGrokUsage(usage)
	outputSnapshot := chatOutputSnapshotFromUsage(true, chatUsage)
	actual := estimate
	if v, ok := ChatActualCost(s.priceFn(modelCode), req.Body, chatUsage, outputSnapshot); ok {
		actual = v
	}
	_ = s.repo.UpdateCost(context.Background(), t.TaskID, actual)
	if t.CostPoints > 0 || actual > 0 {
		if err := s.billing.FinalizeUsage(context.Background(), t.TaskID, actual, &acc.ID); err != nil {
			s.fail(context.Background(), t, err.Error())
			return true, http.StatusOK, nil, err
		}
	}
	s.recordChatPricingResult(context.Background(), t, chatUsage, actual, outputSnapshot)
	s.recordChatOutputSnapshot(context.Background(), t, outputSnapshot)
	s.pool.MarkUsed(context.Background(), acc.ID)
	_ = s.repo.SetSucceeded(context.Background(), t.TaskID, nil)
	return true, http.StatusOK, nil, nil
}

func (s *ChatService) completeMock(ctx context.Context, req ChatCallRequest, modelCode, prompt string, estimate int64) ([]byte, int, error) {
	return s.completeMockWithProvider(ctx, req, modelCode, prompt, estimate, model.ProviderGPT)
}

func (s *ChatService) completeMockWithProvider(ctx context.Context, req ChatCallRequest, modelCode, prompt string, estimate int64, providerName string) ([]byte, int, error) {
	t, err := s.prepareMock(ctx, req, modelCode, prompt, estimate, providerName)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	usage := &ChatUsage{PromptTokens: estimatePromptTokens(req.Body), CompletionTokens: 32}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	raw, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl_" + t.TaskID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelCode,
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "这是 mock 文字回复，用于本地和测试环境验证计费链路。"}, "finish_reason": "stop"}},
		"usage":   usage,
	})
	outputSnapshot := chatOutputSnapshotFromRaw(false, raw, usage)
	actual := estimate
	if v, ok := ChatActualCost(s.priceFn(modelCode), req.Body, usage, outputSnapshot); ok {
		actual = v
	}
	_ = s.repo.UpdateCost(ctx, t.TaskID, actual)
	if t.CostPoints > 0 || actual > 0 {
		if err := s.billing.FinalizeUsage(ctx, t.TaskID, actual, nil); err != nil {
			s.fail(ctx, t, err.Error())
			return nil, http.StatusBadRequest, err
		}
	}
	s.recordChatPricingResult(ctx, t, usage, actual, outputSnapshot)
	s.recordChatOutputSnapshot(ctx, t, outputSnapshot)
	_ = s.repo.SetSucceeded(ctx, t.TaskID, nil)
	return raw, http.StatusOK, nil
}

func (s *ChatService) streamMock(ctx context.Context, req ChatCallRequest, modelCode, prompt string, estimate int64, w http.ResponseWriter) error {
	return s.streamMockWithProvider(ctx, req, modelCode, prompt, estimate, model.ProviderGPT, w)
}

func (s *ChatService) streamMockWithProvider(ctx context.Context, req ChatCallRequest, modelCode, prompt string, estimate int64, providerName string, w http.ResponseWriter) error {
	t, err := s.prepareMock(ctx, req, modelCode, prompt, estimate, providerName)
	if err != nil {
		return err
	}
	usage := &ChatUsage{PromptTokens: estimatePromptTokens(req.Body), CompletionTokens: 32}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	chunks := []string{"这是 ", "mock ", "流式回复。"}
	for _, ch := range chunks {
		payload, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": ch}, "index": 0}}})
		_, _ = io.WriteString(w, "data: "+string(payload)+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
	payload, _ := json.Marshal(map[string]any{"choices": []map[string]any{}, "usage": usage})
	_, _ = io.WriteString(w, "data: "+string(payload)+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	outputSnapshot := chatOutputSnapshotFromStats(true, len(chunks), countRunes(strings.Join(chunks, "")), len(chunks), usage, nil)
	actual := estimate
	if v, ok := ChatActualCost(s.priceFn(modelCode), req.Body, usage, outputSnapshot); ok {
		actual = v
	}
	_ = s.repo.UpdateCost(context.Background(), t.TaskID, actual)
	if t.CostPoints > 0 || actual > 0 {
		if err := s.billing.FinalizeUsage(context.Background(), t.TaskID, actual, nil); err != nil {
			s.fail(context.Background(), t, err.Error())
			return err
		}
	}
	s.recordChatPricingResult(context.Background(), t, usage, actual, outputSnapshot)
	s.recordChatOutputSnapshot(context.Background(), t, outputSnapshot)
	_ = s.repo.SetSucceeded(context.Background(), t.TaskID, nil)
	return nil
}

func (s *ChatService) prepareMock(ctx context.Context, req ChatCallRequest, modelCode, prompt string, estimate int64, providerName string) (*model.GenerationTask, error) {
	if req.IdemKey == "" {
		req.IdemKey = uuid.NewString()
	}
	taskID := chatTaskID()
	params, _ := json.Marshal(map[string]any{
		"estimate_points":       estimate,
		"mock":                  true,
		PricingAuditSnapshotKey: s.chatPricingSnapshot(ctx, modelCode, req.Body, estimate),
	})
	t := &model.GenerationTask{
		TaskID:       taskID,
		UserID:       req.UserID,
		Kind:         string(provider.KindChat),
		Mode:         "chat",
		ModelCode:    modelCode,
		Prompt:       prompt,
		Params:       string(params),
		Count:        1,
		CostPoints:   estimate,
		IdemKey:      req.IdemKey,
		Provider:     providerName,
		Status:       model.GenStatusRunning,
		Progress:     5,
		FromAPIKeyID: req.APIKeyID,
	}
	if req.ClientIP != "" {
		ip := req.ClientIP
		t.ClientIP = &ip
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	if estimate > 0 {
		if err := s.billing.PreDeduct(ctx, PreDeductReq{UserID: req.UserID, TaskID: taskID, Kind: string(provider.KindChat), ModelCode: modelCode, Count: 1, UnitPoints: estimate}); err != nil {
			_ = s.repo.SetFailed(ctx, taskID, err.Error())
			return nil, err
		}
	}
	return t, nil
}

func (s *ChatService) prepare(ctx context.Context, req ChatCallRequest, modelCode, prompt string, estimate int64, providerName, strategy, authType, upstreamModel string, routeSnapshot any) (*model.GenerationTask, *model.Account, error) {
	if req.IdemKey == "" {
		req.IdemKey = uuid.NewString()
	}
	if existing, err := s.repo.GetByIdem(ctx, req.UserID, req.IdemKey); err == nil && existing != nil {
		return nil, nil, errcode.InvalidParam.WithMsg("idempotent chat replay is not supported for response body")
	}
	acc, err := s.pool.PickWhere(ctx, providerName, normalizeRouteStrategy(strategy), func(acc *model.Account) bool {
		return matchesRouteAuthType(acc, authType) && accountAllowsRouteModel(acc, modelCode, upstreamModel)
	})
	if err != nil {
		return nil, nil, errcode.ResourceMissing.WithMsg("no available chat account: " + err.Error())
	}
	taskID := chatTaskID()
	taskParams := map[string]any{
		"estimate_points":       estimate,
		routeParamProvider:      providerName,
		routeParamUpstreamModel: upstreamModel,
		routeParamStrategy:      normalizeRouteStrategy(strategy),
		routeParamAuthType:      authType,
		PricingAuditSnapshotKey: s.chatPricingSnapshot(ctx, modelCode, req.Body, estimate),
	}
	if routeSnapshot != nil {
		taskParams[routeParamSnapshot] = routeSnapshot
	}
	params, _ := json.Marshal(taskParams)
	t := &model.GenerationTask{
		TaskID:       taskID,
		UserID:       req.UserID,
		Kind:         string(provider.KindChat),
		Mode:         "chat",
		ModelCode:    modelCode,
		Prompt:       prompt,
		Params:       string(params),
		Count:        1,
		CostPoints:   estimate,
		IdemKey:      req.IdemKey,
		Provider:     providerName,
		Status:       model.GenStatusPending,
		FromAPIKeyID: req.APIKeyID,
	}
	if req.ClientIP != "" {
		ip := req.ClientIP
		t.ClientIP = &ip
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, nil, errcode.DBError.Wrap(err)
	}
	if estimate > 0 {
		if err := s.billing.PreDeduct(ctx, PreDeductReq{UserID: req.UserID, TaskID: taskID, Kind: string(provider.KindChat), ModelCode: modelCode, Count: 1, UnitPoints: estimate}); err != nil {
			_ = s.repo.SetFailed(ctx, taskID, err.Error())
			return nil, nil, err
		}
	}
	if err := s.repo.SetRunning(ctx, taskID, acc.ID); err != nil {
		logger.FromCtx(ctx).Warn("chat.set_running", zap.Error(err))
	}
	return t, acc, nil
}

func (s *ChatService) prepareAPIChannel(ctx context.Context, req ChatCallRequest, modelCode, prompt string, estimate int64, route chatRuntimeRoute, routeSnapshot any) (*model.GenerationTask, error) {
	if req.IdemKey == "" {
		req.IdemKey = uuid.NewString()
	}
	if existing, err := s.repo.GetByIdem(ctx, req.UserID, req.IdemKey); err == nil && existing != nil {
		return nil, errcode.InvalidParam.WithMsg("idempotent chat replay is not supported for response body")
	}
	taskID := chatTaskID()
	now := time.Now().UTC()
	taskParams := map[string]any{
		"estimate_points":              estimate,
		"model_gateway_source_type":    route.SourceType,
		"model_gateway_source_code":    route.SourceCode,
		"model_gateway_source_name":    route.SourceName,
		"model_gateway_adapter":        route.Adapter,
		"model_gateway_api_channel_id": routeAPIChannelID(route.APIChannel),
		routeParamProvider:             chatProviderAPIChannel,
		routeParamUpstreamModel:        route.UpstreamModel,
		routeParamStrategy:             normalizeRouteStrategy(route.Strategy),
		routeParamAuthType:             route.AuthType,
		PricingAuditSnapshotKey:        s.chatPricingSnapshot(ctx, modelCode, req.Body, estimate),
	}
	if routeSnapshot != nil {
		taskParams[routeParamSnapshot] = routeSnapshot
	}
	params, _ := json.Marshal(taskParams)
	t := &model.GenerationTask{
		TaskID:       taskID,
		UserID:       req.UserID,
		Kind:         string(provider.KindChat),
		Mode:         "chat",
		ModelCode:    modelCode,
		Prompt:       prompt,
		Params:       string(params),
		Count:        1,
		CostPoints:   estimate,
		IdemKey:      req.IdemKey,
		Provider:     chatProviderAPIChannel,
		Status:       model.GenStatusRunning,
		Progress:     5,
		StartedAt:    &now,
		FromAPIKeyID: req.APIKeyID,
	}
	if req.ClientIP != "" {
		ip := req.ClientIP
		t.ClientIP = &ip
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	if estimate > 0 {
		if err := s.billing.PreDeduct(ctx, PreDeductReq{UserID: req.UserID, TaskID: taskID, Kind: string(provider.KindChat), ModelCode: modelCode, Count: 1, UnitPoints: estimate}); err != nil {
			_ = s.repo.SetFailed(ctx, taskID, err.Error())
			return nil, err
		}
	}
	return t, nil
}

func (s *ChatService) chatRoute(ctx context.Context, modelCode string) ProviderRoute {
	routes := s.chatRoutes(ctx, modelCode)
	if len(routes) == 0 {
		return ProviderRoute{Provider: model.ProviderGPT, UpstreamModel: modelCode, Strategy: "round_robin"}
	}
	return routes[0]
}

func (s *ChatService) chatRuntimeRoutes(ctx context.Context, modelCode string) []chatRuntimeRoute {
	if routes, managed := s.modelGatewayChatRuntimeRoutes(ctx, modelCode); managed {
		return routes
	}
	out := make([]chatRuntimeRoute, 0)
	for _, route := range s.chatRoutes(ctx, modelCode) {
		out = appendChatRuntimeRouteCandidate(out, chatRuntimeRoute{
			SourceType:    model.ModelSourceTypeAccountPool,
			SourceCode:    route.Provider,
			Provider:      route.Provider,
			UpstreamModel: route.UpstreamModel,
			Strategy:      route.Strategy,
			AuthType:      route.AuthType,
			Weight:        route.Weight,
			Priority:      route.Priority,
		})
	}
	return out
}

func (s *ChatService) modelGatewayChatRuntimeRoutes(ctx context.Context, modelCode string) ([]chatRuntimeRoute, bool) {
	if s.modelRepo == nil || s.modelSourceRepo == nil || s.apiChannelRepo == nil {
		return nil, false
	}
	item, err := s.modelRepo.GetByCode(ctx, modelCode)
	if err != nil || item == nil || item.Status != model.ModelCatalogStatusEnabled {
		return nil, false
	}
	entryKind := normalizeModelGatewayKindLoose(item.EntryKind)
	if entryKind != model.ModelCatalogKindText && entryKind != model.ModelCatalogKindChat {
		return nil, false
	}
	status := int8(model.ModelSourceStatusEnabled)
	sources, _, err := s.modelSourceRepo.List(ctx, repo.ModelSourceListFilter{ModelCode: modelCode, Status: &status, Page: 1, PageSize: 500})
	if err != nil {
		logger.FromCtx(ctx).Warn("chat.model_gateway.list_sources", zap.String("model", modelCode), zap.Error(err))
		return nil, false
	}
	if len(sources) == 0 {
		return nil, false
	}
	out := make([]chatRuntimeRoute, 0, len(sources))
	for _, source := range sources {
		if source == nil {
			continue
		}
		switch source.SourceType {
		case model.ModelSourceTypeAPIChannel:
			route, ok := s.apiChannelChatRuntimeRoute(ctx, item, entryKind, source)
			if ok {
				out = appendChatRuntimeRouteCandidate(out, route)
			}
		case model.ModelSourceTypeAccountPool:
			route, ok := accountPoolChatRuntimeRoute(item, source)
			if ok {
				out = appendChatRuntimeRouteCandidate(out, route)
			}
		}
	}
	return orderChatRuntimeRoutesForRuntime(modelCode, string(provider.KindChat), out), true
}

func (s *ChatService) apiChannelChatRuntimeRoute(ctx context.Context, item *model.ModelCatalog, entryKind string, source *model.ModelSourceMapping) (chatRuntimeRoute, bool) {
	ch, err := s.apiChannelRepo.GetByCode(ctx, source.SourceCode)
	if err != nil || ch == nil {
		return chatRuntimeRoute{}, false
	}
	upstreamModel := strings.TrimSpace(source.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(item.UpstreamDefaultModel)
	}
	if upstreamModel == "" {
		upstreamModel = item.ModelCode
	}
	adapter := strings.TrimSpace(source.Adapter)
	if adapter == "" {
		adapter = ch.Adapter
	}
	if adapter != model.APIChannelAdapterOpenAIChat {
		return chatRuntimeRoute{}, false
	}
	if ch.Status != model.APIChannelStatusEnabled {
		return chatRuntimeRoute{}, false
	}
	if !modelGatewayListAllows(parseStringListJSON(ch.Models), item.ModelCode, upstreamModel) {
		return chatRuntimeRoute{}, false
	}
	if !modelGatewayCapabilityAllows(parseStringListJSON(ch.Capabilities), entryKind) {
		return chatRuntimeRoute{}, false
	}
	if reason := apiChannelOperationalSkipReason(inspectAPIChannelOperational(ctx, s.apiChannelRepo, source.SourceCode)); reason != "" {
		return chatRuntimeRoute{}, false
	}
	return chatRuntimeRoute{
		SourceType:    model.ModelSourceTypeAPIChannel,
		SourceCode:    source.SourceCode,
		SourceName:    ch.Name,
		Provider:      chatProviderAPIChannel,
		UpstreamModel: upstreamModel,
		Adapter:       adapter,
		Strategy:      normalizeRouteStrategy(source.Strategy),
		AuthType:      source.AuthType,
		Weight:        normalizeRuntimeRouteWeight(source.Weight),
		Priority:      normalizeRuntimeRoutePriority(source.Priority),
		APIChannel:    ch,
	}, true
}

func accountPoolChatRuntimeRoute(item *model.ModelCatalog, source *model.ModelSourceMapping) (chatRuntimeRoute, bool) {
	providerName := strings.TrimSpace(source.SourceCode)
	if providerName != model.ProviderGPT && providerName != model.ProviderGROK {
		return chatRuntimeRoute{}, false
	}
	upstreamModel := strings.TrimSpace(source.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(item.UpstreamDefaultModel)
	}
	if upstreamModel == "" {
		upstreamModel = item.ModelCode
	}
	if accountPoolSourceMismatchReason(item, providerName, upstreamModel) != "" {
		return chatRuntimeRoute{}, false
	}
	return chatRuntimeRoute{
		SourceType:    model.ModelSourceTypeAccountPool,
		SourceCode:    providerName,
		Provider:      providerName,
		UpstreamModel: upstreamModel,
		Strategy:      normalizeRouteStrategy(source.Strategy),
		AuthType:      source.AuthType,
		Weight:        normalizeRuntimeRouteWeight(source.Weight),
		Priority:      normalizeRuntimeRoutePriority(source.Priority),
	}, true
}

func (s *ChatService) chatRuntimeSkippedCandidates(ctx context.Context, modelCode string) []chatRuntimeRoute {
	if s.modelRepo == nil || s.modelSourceRepo == nil {
		return nil
	}
	item, err := s.modelRepo.GetByCode(ctx, modelCode)
	if err != nil || item == nil || item.Status != model.ModelCatalogStatusEnabled {
		return nil
	}
	entryKind := normalizeModelGatewayKindLoose(item.EntryKind)
	if entryKind != model.ModelCatalogKindText && entryKind != model.ModelCatalogKindChat {
		return nil
	}
	sources, _, err := s.modelSourceRepo.List(ctx, repo.ModelSourceListFilter{ModelCode: modelCode, Page: 1, PageSize: 500})
	if err != nil {
		return nil
	}
	out := make([]chatRuntimeRoute, 0)
	for _, source := range sources {
		if source == nil {
			continue
		}
		if skipped := s.chatRuntimeSkippedCandidate(ctx, item, entryKind, source); skipped.SkipReason != "" {
			out = append(out, skipped)
		}
	}
	return out
}

func (s *ChatService) chatRuntimeSkippedCandidate(ctx context.Context, item *model.ModelCatalog, entryKind string, source *model.ModelSourceMapping) chatRuntimeRoute {
	upstreamModel := effectiveModelGatewayUpstreamModel(item, source.UpstreamModel)
	route := chatRuntimeRoute{
		SourceType:    source.SourceType,
		SourceCode:    source.SourceCode,
		UpstreamModel: upstreamModel,
		Adapter:       strings.TrimSpace(source.Adapter),
		Strategy:      normalizeRouteStrategy(source.Strategy),
		AuthType:      source.AuthType,
		Weight:        normalizeRuntimeRouteWeight(source.Weight),
		Priority:      normalizeRuntimeRoutePriority(source.Priority),
	}
	if source.Status != model.ModelSourceStatusEnabled {
		route.SkipReason = "来源映射已停用"
		return route
	}
	switch source.SourceType {
	case model.ModelSourceTypeAPIChannel:
		return s.apiChannelChatSkippedCandidate(ctx, item, entryKind, source, route)
	case model.ModelSourceTypeAccountPool:
		return accountPoolChatSkippedCandidate(item, source, route)
	default:
		route.SkipReason = "来源类型不支持"
		return route
	}
}

func (s *ChatService) apiChannelChatSkippedCandidate(ctx context.Context, item *model.ModelCatalog, entryKind string, source *model.ModelSourceMapping, route chatRuntimeRoute) chatRuntimeRoute {
	if s.apiChannelRepo == nil {
		route.SkipReason = "API 渠道仓储未初始化"
		return route
	}
	ch, err := s.apiChannelRepo.GetByCode(ctx, source.SourceCode)
	if err != nil || ch == nil {
		route.SkipReason = "API 渠道不存在或已删除"
		return route
	}
	route.SourceName = ch.Name
	if route.Adapter == "" {
		route.Adapter = ch.Adapter
	}
	if route.Adapter != model.APIChannelAdapterOpenAIChat {
		route.SkipReason = "文字模型只能使用 openai_compatible_chat API 渠道"
		return route
	}
	if ch.Status != model.APIChannelStatusEnabled {
		route.SkipReason = "API 渠道已停用"
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
	return chatRuntimeRoute{}
}

func accountPoolChatSkippedCandidate(item *model.ModelCatalog, source *model.ModelSourceMapping, route chatRuntimeRoute) chatRuntimeRoute {
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
	return chatRuntimeRoute{}
}

func (s *ChatService) chatRoutes(ctx context.Context, modelCode string) []ProviderRoute {
	fallback := model.ProviderGPT
	if grokweb.IsChatModel(modelCode) {
		fallback = model.ProviderGROK
	}
	routes := []ProviderRoute{{Provider: fallback, UpstreamModel: modelCode, Strategy: "round_robin"}}
	if s.routeSvc != nil {
		if resolved, _ := s.routeSvc.ResolveCandidates(ctx, provider.KindChat, modelCode, fallback); len(resolved) > 0 {
			routes = resolved
		}
	}
	out := make([]ProviderRoute, 0, len(routes))
	for _, route := range routes {
		if strings.TrimSpace(route.Provider) == "" {
			route.Provider = fallback
		}
		if strings.TrimSpace(route.Strategy) == "" {
			route.Strategy = "round_robin"
		}
		if strings.TrimSpace(route.UpstreamModel) == "" {
			route.UpstreamModel = modelCode
		}
		if route.Provider == model.ProviderGPT && strings.EqualFold(route.UpstreamModel, modelCode) {
			route.UpstreamModel = s.upstreamModel(modelCode)
		}
		route.Strategy = normalizeRouteStrategy(route.Strategy)
		out = appendProviderRouteCandidate(out, route)
	}
	if len(out) == 0 {
		out = append(out, ProviderRoute{Provider: fallback, UpstreamModel: modelCode, Strategy: "round_robin"})
	}
	return out
}

func (s *ChatService) callJSON(ctx context.Context, t *model.GenerationTask, acc *model.Account, providerName, stage string, body map[string]any, meta map[string]any) ([]byte, int, *ChatUsage, error) {
	started := time.Now()
	url := accountChatEndpoint(acc)
	resp, err := s.openUpstream(ctx, acc, body)
	if err != nil {
		s.logChatUpstream(ctx, t, acc, providerName, stage, http.MethodPost, url, 0, time.Since(started), chatRequestExcerpt(body), nil, err, meta)
		return nil, http.StatusBadGateway, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	s.logChatUpstream(ctx, t, acc, providerName, stage, http.MethodPost, url, resp.StatusCode, time.Since(started), chatRequestExcerpt(body), raw, nil, meta)
	if resp.StatusCode >= 400 {
		return raw, resp.StatusCode, nil, nil
	}
	return raw, resp.StatusCode, parseUsage(raw), nil
}

func (s *ChatService) callAPIChannelJSON(ctx context.Context, t *model.GenerationTask, route chatRuntimeRoute, body map[string]any) ([]byte, int, *ChatUsage, error) {
	started := time.Now()
	ch := route.APIChannel
	url := apiChannelChatEndpoint(ch)
	meta := chatAPIChannelMeta(route)
	resp, credRef, err := s.openAPIChannelUpstream(ctx, ch, body)
	addAPIChannelCredentialMeta(meta, credRef)
	if err != nil {
		recordAPIChannelCredentialError(ctx, s.apiChannelRepo, credRef, err)
		s.logChatUpstream(ctx, t, nil, chatProviderAPIChannel, "api_channel.chat.completions", http.MethodPost, url, 0, time.Since(started), chatRequestExcerpt(body), nil, err, meta)
		return nil, http.StatusBadGateway, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	s.logChatUpstream(ctx, t, nil, chatProviderAPIChannel, "api_channel.chat.completions", http.MethodPost, url, resp.StatusCode, time.Since(started), chatRequestExcerpt(body), raw, nil, meta)
	if resp.StatusCode >= 400 {
		recordAPIChannelCredentialHTTPFailure(ctx, s.apiChannelRepo, credRef, resp.StatusCode, raw)
		return raw, resp.StatusCode, nil, nil
	}
	recordAPIChannelCredentialSuccess(ctx, s.apiChannelRepo, credRef)
	return raw, resp.StatusCode, parseUsage(raw), nil
}

func chatAPIChannelMeta(route chatRuntimeRoute) map[string]any {
	ch := route.APIChannel
	meta := map[string]any{
		"model_gateway_source_type":    route.SourceType,
		"model_gateway_source_code":    route.SourceCode,
		"model_gateway_source_name":    route.SourceName,
		"model_gateway_adapter":        route.Adapter,
		"model_gateway_api_channel_id": routeAPIChannelID(ch),
		"upstream_model":               route.UpstreamModel,
		"strategy":                     normalizeRouteStrategy(route.Strategy),
		"auth_type":                    route.AuthType,
		"model_gateway_route_index":    route.RouteIndex,
		"model_gateway_attempt":        route.Attempt,
	}
	if ch != nil {
		meta["api_channel_code"] = ch.Code
		meta["api_channel_name"] = ch.Name
		meta["api_channel_provider_name"] = ch.ProviderName
	}
	return meta
}

func chatRouteSourceCode(sourceCode, provider string) string {
	if strings.TrimSpace(sourceCode) != "" {
		return sourceCode
	}
	return provider
}

func (s *ChatService) openUpstream(ctx context.Context, acc *model.Account, body map[string]any) (*http.Response, error) {
	cred, err := s.credential(acc)
	if err != nil {
		return nil, err
	}
	base := "https://api.openai.com"
	if acc.BaseURL != nil && *acc.BaseURL != "" {
		base = strings.TrimRight(*acc.BaseURL, "/")
	}
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openAICompatibleChatEndpoint(base), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+cred)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("User-Agent", "kleinai/1.0")
	proxyURL, perr := s.resolveProxyURL(ctx, acc)
	if perr != nil {
		logger.FromCtx(ctx).Warn("chat.openai.resolve_proxy", zap.Error(perr))
	}
	if proxyURL == "" {
		return s.client.Do(httpReq)
	}
	client, err := outbound.NewClient(outbound.Options{Timeout: 10 * time.Minute, ProxyURL: proxyURL, Mode: outbound.ModeUTLS, Profile: outbound.ProfileChrome})
	if err != nil {
		return nil, err
	}
	return client.Do(httpReq)
}

func (s *ChatService) openAPIChannelUpstream(ctx context.Context, ch *model.APIChannel, body map[string]any) (*http.Response, *APIChannelCredentialRef, error) {
	credRef, err := selectAPIChannelCredentialWithLimiter(ctx, s.apiChannelRepo, s.aes, ch, estimatePromptTokens(body), s.apiLimiter)
	if err != nil {
		return nil, nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(ch.BaseURL), "/")
	if base == "" {
		return nil, credRef, fmt.Errorf("api channel base url is empty")
	}
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openAICompatibleChatEndpoint(base), bytes.NewReader(payload))
	if err != nil {
		return nil, credRef, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+credRef.Token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", chatAcceptHeader(body))
	httpReq.Header.Set("User-Agent", "kleinai/1.0")
	timeout := time.Duration(ch.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	proxyURL, perr := s.resolveProxyURLByID(ctx, ch.ProxyID)
	if perr != nil {
		logger.FromCtx(ctx).Warn("chat.api_channel.resolve_proxy", zap.String("channel", ch.Code), zap.Error(perr))
	}
	if proxyURL == "" {
		resp, err := (&http.Client{Timeout: timeout}).Do(httpReq)
		return resp, credRef, err
	}
	client, err := outbound.NewClient(outbound.Options{Timeout: timeout, ProxyURL: proxyURL, Mode: outbound.ModeUTLS, Profile: outbound.ProfileChrome})
	if err != nil {
		return nil, credRef, err
	}
	resp, err := client.Do(httpReq)
	return resp, credRef, err
}

func (s *ChatService) logChatUpstream(ctx context.Context, t *model.GenerationTask, acc *model.Account, providerName, stage, method, url string, statusCode int, duration time.Duration, requestExcerpt string, responseBody []byte, callErr error, meta map[string]any) {
	if t == nil || s.repo == nil {
		return
	}
	row := &model.GenerationUpstreamLog{
		TaskID:     t.TaskID,
		Provider:   providerName,
		Stage:      stage,
		Method:     method,
		URL:        truncate(url, 512),
		StatusCode: statusCode,
		DurationMs: duration.Milliseconds(),
	}
	if row.Provider == "" {
		row.Provider = t.Provider
	}
	if row.Stage == "" {
		row.Stage = "chat.completions"
	}
	if acc != nil {
		row.AccountID = &acc.ID
	}
	if requestExcerpt != "" {
		v := truncate(requestExcerpt, 12000)
		row.RequestExcerpt = &v
	}
	if len(responseBody) > 0 {
		v := truncate(string(responseBody), 12000)
		row.ResponseExcerpt = &v
	}
	if callErr != nil {
		v := truncate(callErr.Error(), 4000)
		row.Error = &v
	}
	if len(meta) > 0 {
		if b, err := json.Marshal(meta); err == nil {
			v := string(b)
			row.Meta = &v
		}
	}
	if err := s.repo.CreateUpstreamLog(ctx, row); err != nil {
		logger.FromCtx(ctx).Warn("chat.upstream_log_failed", zap.String("task_id", t.TaskID), zap.String("stage", row.Stage), zap.Error(err))
	}
}

func (s *ChatService) resolveProxyURL(ctx context.Context, acc *model.Account) (string, error) {
	if acc != nil && acc.ProxyID != nil {
		return s.resolveProxyURLByID(ctx, acc.ProxyID)
	}
	return s.resolveProxyURLByID(ctx, nil)
}

func (s *ChatService) resolveProxyURLByID(ctx context.Context, proxyID *uint64) (string, error) {
	if s.proxySvc == nil || s.cfg == nil {
		return "", nil
	}
	pid := uint64(0)
	if proxyID != nil {
		pid = *proxyID
	} else if s.cfg.GlobalProxyEnabled(ctx) {
		pid = s.cfg.GlobalProxyID(ctx)
	}
	if pid == 0 {
		return "", nil
	}
	p, err := s.proxySvc.GetByID(ctx, pid)
	if err != nil || p == nil || p.Status != model.ProxyStatusEnabled {
		return "", err
	}
	u, err := s.proxySvc.BuildURL(p)
	if err != nil || u == nil {
		return "", err
	}
	return u.String(), nil
}

func (s *ChatService) apiChannelCredential(ch *model.APIChannel) (string, error) {
	ref, err := selectAPIChannelCredential(context.Background(), s.apiChannelRepo, s.aes, ch, 0)
	if err != nil {
		return "", err
	}
	return ref.Token, nil
}

func accountChatEndpoint(acc *model.Account) string {
	base := "https://api.openai.com"
	if acc != nil && acc.BaseURL != nil && strings.TrimSpace(*acc.BaseURL) != "" {
		base = strings.TrimRight(strings.TrimSpace(*acc.BaseURL), "/")
	}
	return openAICompatibleChatEndpoint(base)
}

func apiChannelChatEndpoint(ch *model.APIChannel) string {
	if ch == nil {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(ch.BaseURL), "/")
	if base == "" {
		return ""
	}
	return openAICompatibleChatEndpoint(base)
}

func chatRequestExcerpt(body map[string]any) string {
	if len(body) == 0 {
		return ""
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ""
	}
	return string(raw)
}

func chatAcceptHeader(body map[string]any) string {
	if stream, _ := body["stream"].(bool); stream {
		return "text/event-stream, application/json"
	}
	return "application/json"
}

func (s *ChatService) credential(acc *model.Account) (string, error) {
	if s.aes == nil || len(acc.CredentialEnc) == 0 {
		return "", fmt.Errorf("chat account missing credential")
	}
	plain, err := s.aes.Decrypt(acc.CredentialEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt credential failed")
	}
	return string(plain), nil
}

func (s *ChatService) estimateCost(modelCode string, body map[string]any) int64 {
	price := s.priceFn(modelCode)
	return ChatEstimatedCost(price, body)
}

func (s *ChatService) chatPricingSnapshot(ctx context.Context, modelCode string, body map[string]any, estimate int64) map[string]any {
	price := ChatPrice{}
	if s.priceFn != nil {
		price = s.priceFn(modelCode)
	}
	return ChatPricingAuditSnapshot(ctx, s.cfg, s.modelRepo, price, modelCode, body, estimate)
}

func (s *ChatService) recordChatPricingResult(ctx context.Context, t *model.GenerationTask, usage *ChatUsage, actual int64, outputSnapshots ...map[string]any) {
	if s == nil || s.repo == nil || t == nil {
		return
	}
	_ = s.repo.MergeParams(ctx, t.TaskID, map[string]any{
		PricingAuditSnapshotKey: ChatPricingResultPatch(usage, t.CostPoints, actual, outputSnapshots...),
	})
}

func (s *ChatService) recordChatOutputResult(ctx context.Context, t *model.GenerationTask, stream bool, raw []byte, usage *ChatUsage) {
	s.recordChatOutputSnapshot(ctx, t, chatOutputSnapshotFromRaw(stream, raw, usage))
}

func (s *ChatService) recordChatOutputSnapshot(ctx context.Context, t *model.GenerationTask, snapshot map[string]any) {
	if s == nil || s.repo == nil || t == nil || snapshot == nil {
		return
	}
	_ = s.repo.MergeParams(ctx, t.TaskID, map[string]any{
		OutputAuditSnapshotKey: snapshot,
	})
}

func chatUsageFromGrokUsage(usage *grokweb.OpenAIUsage) *ChatUsage {
	if usage == nil {
		return nil
	}
	return &ChatUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}
}

func (s *ChatService) upstreamModel(modelCode string) string {
	if s.cfg == nil {
		return modelCode
	}
	raw := s.cfg.GetString(context.Background(), "billing.model_prices", "")
	if raw == "" {
		return modelCode
	}
	var rows []struct {
		ModelCode     string `json:"model_code"`
		UpstreamModel string `json:"upstream_model"`
		Enabled       *bool  `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return modelCode
	}
	for _, row := range rows {
		if row.ModelCode == modelCode && row.UpstreamModel != "" {
			if row.Enabled != nil && !*row.Enabled {
				return modelCode
			}
			return row.UpstreamModel
		}
	}
	return modelCode
}

func (s *ChatService) fail(ctx context.Context, t *model.GenerationTask, reason string) {
	_ = s.repo.SetFailed(ctx, t.TaskID, reason)
	_ = s.repo.MergeParams(ctx, t.TaskID, map[string]any{
		PricingAuditSnapshotKey: PricingFailureRefundPatch(t.CostPoints, reason),
	})
	_ = s.billing.FailRefund(ctx, t.TaskID, reason)
}

func parseUsage(raw []byte) *ChatUsage {
	var obj struct {
		Usage *ChatUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	return obj.Usage
}

func forwardChatStream(r io.Reader, w http.ResponseWriter) (*ChatUsage, map[string]any, error) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var usage *ChatUsage
	var choiceCount int
	var contentChars int
	var chunkCount int
	var finishReasons []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload != "" && payload != "[DONE]" {
				if u := parseStreamUsage([]byte(payload)); u != nil {
					usage = u
				}
				stats := parseStreamOutputStats([]byte(payload))
				choiceCount += stats.ChoiceCount
				contentChars += stats.ContentChars
				chunkCount += stats.ChunkCount
				finishReasons = append(finishReasons, stats.FinishReasons...)
			}
		}
		_, _ = io.WriteString(w, line+"\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
	if err := sc.Err(); err != nil {
		return usage, chatOutputSnapshotFromStats(true, choiceCount, contentChars, chunkCount, usage, finishReasons), err
	}
	return usage, chatOutputSnapshotFromStats(true, choiceCount, contentChars, chunkCount, usage, finishReasons), nil
}

type chatOutputStats struct {
	ChoiceCount   int
	ContentChars  int
	ChunkCount    int
	FinishReasons []string
}

func chatOutputSnapshotFromRaw(stream bool, raw []byte, usage *ChatUsage) map[string]any {
	stats := parseChatCompletionOutputStats(raw)
	return chatOutputSnapshotFromStats(stream, stats.ChoiceCount, stats.ContentChars, stats.ChunkCount, firstNonNilChatUsage(usage, parseUsage(raw)), stats.FinishReasons)
}

func chatOutputSnapshotFromUsage(stream bool, usage *ChatUsage) map[string]any {
	return chatOutputSnapshotFromStats(stream, 0, 0, 0, usage, nil)
}

func chatOutputSnapshotFromStats(stream bool, choiceCount, contentChars, chunkCount int, usage *ChatUsage, finishReasons []string) map[string]any {
	completionTokens := 0
	totalTokens := 0
	if usage != nil {
		completionTokens = usage.CompletionTokens
		totalTokens = usage.TotalTokens
	}
	snapshot := map[string]any{
		"kind":              string(provider.KindChat),
		"stream":            stream,
		"output_present":    contentChars > 0 || completionTokens > 0,
		"choice_count":      choiceCount,
		"content_chars":     contentChars,
		"chunk_count":       chunkCount,
		"completion_tokens": completionTokens,
		"total_tokens":      totalTokens,
	}
	if len(finishReasons) > 0 {
		snapshot["finish_reasons"] = compactStringList(finishReasons)
	}
	return snapshot
}

func chatOutputSnapshotProvesOutput(snapshot map[string]any) bool {
	if snapshot == nil {
		return false
	}
	if v, ok := snapshot["output_present"].(bool); ok && v {
		return true
	}
	return intAny(snapshot["content_chars"], 0) > 0 || intAny(snapshot["completion_tokens"], 0) > 0
}

func parseChatCompletionOutputStats(raw []byte) chatOutputStats {
	var obj struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
			Delta struct {
				Content any `json:"content"`
			} `json:"delta"`
			Text         any    `json:"text"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &obj) != nil {
		return chatOutputStats{}
	}
	stats := chatOutputStats{ChoiceCount: len(obj.Choices)}
	for _, choice := range obj.Choices {
		chars := contentRuneCount(choice.Message.Content) + contentRuneCount(choice.Delta.Content) + contentRuneCount(choice.Text)
		stats.ContentChars += chars
		if chars > 0 {
			stats.ChunkCount++
		}
		if strings.TrimSpace(choice.FinishReason) != "" {
			stats.FinishReasons = append(stats.FinishReasons, strings.TrimSpace(choice.FinishReason))
		}
	}
	return stats
}

func parseStreamOutputStats(raw []byte) chatOutputStats {
	return parseChatCompletionOutputStats(raw)
}

func contentRuneCount(v any) int {
	switch x := v.(type) {
	case string:
		return countRunes(x)
	case []any:
		var n int
		for _, item := range x {
			n += contentRuneCount(item)
		}
		return n
	case map[string]any:
		if text, ok := x["text"]; ok {
			return contentRuneCount(text)
		}
		if content, ok := x["content"]; ok {
			return contentRuneCount(content)
		}
		return 0
	default:
		return 0
	}
}

func countRunes(s string) int {
	return len([]rune(strings.TrimSpace(s)))
}

func firstNonNilChatUsage(values ...*ChatUsage) *ChatUsage {
	for _, usage := range values {
		if usage != nil {
			return usage
		}
	}
	return nil
}

func compactStringList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func writeChatJSONError(w http.ResponseWriter, status int, raw []byte) {
	if status <= 0 {
		status = http.StatusBadGateway
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func parseStreamUsage(raw []byte) *ChatUsage {
	var obj struct {
		Usage *ChatUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil || obj.Usage == nil || obj.Usage.TotalTokens == 0 {
		return nil
	}
	return obj.Usage
}

func chatPrompt(body map[string]any) string {
	s := chatPromptForBilling(body)
	if len(s) > 4000 {
		return s[:4000]
	}
	return s
}

func chatPromptForBilling(body map[string]any) string {
	if v, ok := body["messages"]; ok {
		b, _ := json.Marshal(v)
		return string(b)
	}
	return ""
}

func estimatePromptTokens(body map[string]any) int {
	p := chatPromptForBilling(body)
	if p == "" {
		return 1
	}
	return len([]rune(p))/4 + 1
}

func estimatePromptChars(body map[string]any) int {
	p := chatPromptForBilling(body)
	if p == "" {
		return 1
	}
	return countRunes(p)
}

func strAny(v any, def string) string {
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return def
}

func cloneChatBody(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func appendChatRuntimeRouteCandidate(routes []chatRuntimeRoute, route chatRuntimeRoute) []chatRuntimeRoute {
	if strings.TrimSpace(route.SourceType) == "" {
		route.SourceType = model.ModelSourceTypeAccountPool
	}
	if strings.TrimSpace(route.Strategy) == "" {
		route.Strategy = "round_robin"
	}
	route.Priority = normalizeRuntimeRoutePriority(route.Priority)
	route.Weight = normalizeRuntimeRouteWeight(route.Weight)
	if route.SourceType == model.ModelSourceTypeAccountPool && strings.TrimSpace(route.Provider) == "" {
		route.Provider = route.SourceCode
	}
	key := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(route.SourceType)),
		strings.ToLower(strings.TrimSpace(route.SourceCode)),
		strings.ToLower(strings.TrimSpace(route.Provider)),
		strings.ToLower(strings.TrimSpace(route.UpstreamModel)),
		strings.ToLower(strings.TrimSpace(route.Adapter)),
		strings.ToLower(strings.TrimSpace(route.AuthType)),
	}, "\x00")
	for _, existing := range routes {
		existingKey := strings.Join([]string{
			strings.ToLower(strings.TrimSpace(existing.SourceType)),
			strings.ToLower(strings.TrimSpace(existing.SourceCode)),
			strings.ToLower(strings.TrimSpace(existing.Provider)),
			strings.ToLower(strings.TrimSpace(existing.UpstreamModel)),
			strings.ToLower(strings.TrimSpace(existing.Adapter)),
			strings.ToLower(strings.TrimSpace(existing.AuthType)),
		}, "\x00")
		if existingKey == key {
			return routes
		}
	}
	return append(routes, route)
}

func chatRuntimeRouteSnapshotPayload(modelCode string, routes []chatRuntimeRoute, selectedIndex int, skipped ...[]chatRuntimeRoute) map[string]any {
	candidates := make([]map[string]any, 0, len(routes))
	for idx, route := range routes {
		sourceType := strings.TrimSpace(route.SourceType)
		if sourceType == "" {
			sourceType = model.ModelSourceTypeAccountPool
		}
		candidates = append(candidates, map[string]any{
			"index":          idx + 1,
			"source_type":    sourceType,
			"source_code":    route.SourceCode,
			"source_name":    route.SourceName,
			"provider":       route.Provider,
			"adapter":        route.Adapter,
			"upstream_model": route.UpstreamModel,
			"strategy":       normalizeRouteStrategy(route.Strategy),
			"auth_type":      route.AuthType,
			"priority":       normalizeRuntimeRoutePriority(route.Priority),
			"weight":         normalizeRuntimeRouteWeight(route.Weight),
		})
	}
	skippedCandidates := make([]map[string]any, 0)
	if len(skipped) > 0 {
		for idx, route := range skipped[0] {
			sourceType := strings.TrimSpace(route.SourceType)
			if sourceType == "" {
				sourceType = model.ModelSourceTypeAccountPool
			}
			skippedCandidates = append(skippedCandidates, map[string]any{
				"index":          idx + 1,
				"source_type":    sourceType,
				"source_code":    route.SourceCode,
				"source_name":    route.SourceName,
				"provider":       route.Provider,
				"adapter":        route.Adapter,
				"upstream_model": route.UpstreamModel,
				"strategy":       normalizeRouteStrategy(route.Strategy),
				"auth_type":      route.AuthType,
				"priority":       normalizeRuntimeRoutePriority(route.Priority),
				"weight":         normalizeRuntimeRouteWeight(route.Weight),
				"skip_reason":    route.SkipReason,
			})
		}
	}
	payload := map[string]any{
		"version":         1,
		"model_code":      modelCode,
		"kind":            string(provider.KindChat),
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

func routeAPIChannelID(ch *model.APIChannel) uint64 {
	if ch == nil {
		return 0
	}
	return ch.ID
}

func retryableChatFallback(status int, err error) bool {
	if retryableChatHTTPStatus(status) {
		return true
	}
	return retryableProviderError(err)
}

func retryableChatHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status >= 500
}

func intAny(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return def
	}
}

func isLiveProvider(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "real", "live", "prod":
		return true
	default:
		return false
	}
}

func chatTaskID() string {
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	if len(id) > 26 {
		return id[:26]
	}
	return id
}

func snippet(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	r := []rune(string(b))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "...(truncated)"
}
