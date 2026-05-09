package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
	grokweb "github.com/kleinai/backend/internal/provider/grok"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/errcode"
)

// ProviderRouteTestService dry-runs provider.routes for admin diagnostics.
type ProviderRouteTestService struct {
	routeSvc    *ProviderRouteService
	accountRepo *repo.AccountRepo
}

func NewProviderRouteTestService(routeSvc *ProviderRouteService, accountRepo *repo.AccountRepo) *ProviderRouteTestService {
	return &ProviderRouteTestService{routeSvc: routeSvc, accountRepo: accountRepo}
}

func (s *ProviderRouteTestService) Health(ctx context.Context) (*dto.ProviderHealthSummaryResp, error) {
	if s.accountRepo == nil {
		return &dto.ProviderHealthSummaryResp{
			RefreshedAt: time.Now().Unix(),
			Providers:   []dto.ProviderHealthProviderResp{},
		}, nil
	}
	rows, err := s.accountRepo.ProviderHealthSummary(ctx)
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	authRows, err := s.accountRepo.ProviderHealthAuthBreakdown(ctx)
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	errorRows, err := s.accountRepo.ProviderHealthErrorSamples(ctx, 50)
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return buildProviderHealthSummary(time.Now(), rows, authRows, errorRows), nil
}

func (s *ProviderRouteTestService) Test(ctx context.Context, req dto.ProviderRouteTestReq) (*dto.ProviderRouteTestResp, error) {
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	modelCode := strings.TrimSpace(req.ModelCode)
	if kind == "" || modelCode == "" {
		return nil, errcode.InvalidParam.WithMsg("kind 和 model_code 不能为空")
	}
	fallback := strings.ToLower(strings.TrimSpace(req.FallbackProvider))
	if fallback == "" {
		fallback = defaultProviderRouteFallback(kind, modelCode)
	}
	if fallback != model.ProviderGPT && fallback != model.ProviderGROK {
		return nil, errcode.InvalidParam.WithMsg("fallback_provider 只能是 gpt 或 grok")
	}

	routes, trace := []ProviderRoute{{
		Provider:      fallback,
		UpstreamModel: modelCode,
		Strategy:      "round_robin",
	}}, ProviderRouteTrace{FallbackReason: "provider.routes 未初始化"}
	if s.routeSvc != nil {
		routes, trace = s.routeSvc.ResolveCandidates(ctx, provider.Kind(kind), modelCode, fallback)
	}
	if len(routes) == 0 {
		routes = []ProviderRoute{{Provider: fallback, UpstreamModel: modelCode, Strategy: "round_robin"}}
	}

	candidates := make([]dto.ProviderRouteCandidateResp, 0, len(routes))
	for i, route := range routes {
		if route.Provider == model.ProviderGPT && (kind == "text" || kind == string(provider.KindChat)) && strings.EqualFold(route.UpstreamModel, modelCode) {
			route.UpstreamModel = chatUpstreamModelFromConfig(ctx, s.routeSvc, modelCode)
			routes[i] = route
		}
		candidateAccounts, availableAccounts, warning, err := s.accountStats(ctx, route, modelCode)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, dto.ProviderRouteCandidateResp{
			Index:             i + 1,
			Provider:          route.Provider,
			UpstreamModel:     route.UpstreamModel,
			AuthType:          route.AuthType,
			Strategy:          route.Strategy,
			CandidateAccounts: candidateAccounts,
			AvailableAccounts: availableAccounts,
			Warning:           warning,
		})
	}
	route := routes[0]
	candidateAccounts := candidates[0].CandidateAccounts
	availableAccounts := candidates[0].AvailableAccounts
	warning := candidates[0].Warning
	if warning == "" && !trace.MatchedConfig && trace.FallbackReason != "" {
		warning = trace.FallbackReason
	}
	return &dto.ProviderRouteTestResp{
		Kind:              kind,
		ModelCode:         modelCode,
		FallbackProvider:  fallback,
		Provider:          route.Provider,
		UpstreamModel:     route.UpstreamModel,
		AuthType:          route.AuthType,
		Strategy:          route.Strategy,
		MatchedConfig:     trace.MatchedConfig,
		MatchedKind:       trace.MatchedKind,
		MatchedModelCode:  trace.MatchedModelCode,
		FallbackReason:    trace.FallbackReason,
		CandidateAccounts: candidateAccounts,
		AvailableAccounts: availableAccounts,
		Warning:           warning,
		Candidates:        candidates,
	}, nil
}

func (s *ProviderRouteTestService) accountStats(ctx context.Context, route ProviderRoute, modelCode string) (int, int, string, error) {
	if s.accountRepo == nil {
		return 0, 0, "账号池仓储未初始化", nil
	}
	if strings.TrimSpace(route.Provider) == "" {
		return 0, 0, "没有解析到 provider", nil
	}
	accounts, err := s.accountRepo.AvailableByProvider(ctx, route.Provider)
	if err != nil {
		return 0, 0, "", errcode.DBError.Wrap(err)
	}
	available := 0
	for _, acc := range accounts {
		if matchesRouteAuthType(acc, route.AuthType) && accountAllowsRouteModel(acc, modelCode, route.UpstreamModel) {
			available++
		}
	}
	warning := ""
	switch {
	case len(accounts) == 0:
		warning = "当前 provider 下没有可调度账号"
	case available == 0:
		warning = "账号池存在账号，但 auth_type 或 model_whitelist 过滤后无可用账号"
	}
	return len(accounts), available, warning, nil
}

func buildProviderHealthSummary(now time.Time, rows []*repo.ProviderHealthRow, authRows []*repo.ProviderHealthAuthRow, errorRows []*repo.ProviderHealthErrorRow) *dto.ProviderHealthSummaryResp {
	providers := make([]dto.ProviderHealthProviderResp, 0, len(rows))
	index := map[string]int{}
	for _, row := range rows {
		if row == nil {
			continue
		}
		item := dto.ProviderHealthProviderResp{
			Provider:        row.Provider,
			Total:           row.Total,
			Enabled:         row.Enabled,
			Disabled:        row.Disabled,
			Broken:          row.Broken,
			Banned:          row.Banned,
			Available:       row.Available,
			CooldownActive:  row.CooldownActive,
			TokenExpired:    row.TokenExpired,
			LastTestOK:      row.LastTestOK,
			LastTestFail:    row.LastTestFail,
			LastTestUnknown: row.LastTestUnknown,
			QuotaZero:       row.QuotaZero,
			SuccessCount:    row.SuccessCount,
			ErrorCount:      row.ErrorCount,
			AuthTypes:       []dto.ProviderHealthAuthResp{},
			RecentErrors:    []dto.ProviderHealthErrorResp{},
		}
		index[row.Provider] = len(providers)
		providers = append(providers, item)
	}
	for _, row := range authRows {
		if row == nil {
			continue
		}
		i, ok := index[row.Provider]
		if !ok {
			continue
		}
		providers[i].AuthTypes = append(providers[i].AuthTypes, dto.ProviderHealthAuthResp{
			AuthType:       row.AuthType,
			Total:          row.Total,
			Available:      row.Available,
			CooldownActive: row.CooldownActive,
			LastTestOK:     row.LastTestOK,
			LastTestFail:   row.LastTestFail,
		})
	}
	for _, row := range errorRows {
		if row == nil {
			continue
		}
		i, ok := index[row.Provider]
		if !ok || len(providers[i].RecentErrors) >= 5 {
			continue
		}
		item := dto.ProviderHealthErrorResp{
			AccountID:  row.ID,
			Name:       row.Name,
			AuthType:   row.AuthType,
			Status:     row.Status,
			ErrorCount: row.ErrorCount,
			UpdatedAt:  row.UpdatedAt.Unix(),
		}
		if row.LastError != nil {
			item.LastError = safeProviderDiagnosticText(*row.LastError)
		}
		if row.LastTestError != nil {
			item.LastTestError = safeProviderDiagnosticText(*row.LastTestError)
		}
		if row.LastTestAt != nil {
			item.LastTestAt = row.LastTestAt.Unix()
		}
		if row.CooldownUntil != nil {
			item.CooldownUntil = row.CooldownUntil.Unix()
		}
		if row.AccessTokenExp != nil {
			item.AccessTokenExpiresAt = row.AccessTokenExp.Unix()
		}
		providers[i].RecentErrors = append(providers[i].RecentErrors, item)
	}
	return &dto.ProviderHealthSummaryResp{
		RefreshedAt: now.Unix(),
		Providers:   providers,
	}
}

func safeProviderDiagnosticText(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	fields := strings.Fields(v)
	for i, field := range fields {
		lower := strings.ToLower(field)
		if strings.HasPrefix(lower, "sk-") || strings.HasPrefix(lower, "sess-") || strings.HasPrefix(lower, "bearer") {
			fields[i] = "[redacted]"
		}
	}
	v = strings.Join(fields, " ")
	const maxLen = 220
	if len(v) <= maxLen {
		return v
	}
	return v[:maxLen] + "..."
}

func defaultProviderRouteFallback(kind, modelCode string) string {
	switch kind {
	case string(provider.KindVideo):
		return model.ProviderGROK
	case "text", string(provider.KindChat):
		if grokweb.IsChatModel(modelCode) {
			return model.ProviderGROK
		}
		return model.ProviderGPT
	default:
		return model.ProviderGPT
	}
}

func chatUpstreamModelFromConfig(ctx context.Context, routeSvc *ProviderRouteService, modelCode string) string {
	if routeSvc == nil || routeSvc.cfg == nil {
		return modelCode
	}
	raw := routeSvc.cfg.GetString(ctx, "billing.model_prices", "")
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
		if row.ModelCode == modelCode && strings.TrimSpace(row.UpstreamModel) != "" {
			if row.Enabled != nil && !*row.Enabled {
				return modelCode
			}
			return strings.TrimSpace(row.UpstreamModel)
		}
	}
	return modelCode
}
