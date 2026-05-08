package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
)

const SettingProviderRoutes = "provider.routes"

// ProviderRouteRule describes how a public model should be sent to upstream
// account pools. It is stored in system_config as JSON under provider.routes.
type ProviderRouteRule struct {
	Kind      string                `json:"kind"`
	ModelCode string                `json:"model_code"`
	Enabled   *bool                 `json:"enabled,omitempty"`
	Strategy  string                `json:"strategy,omitempty"`
	Routes    []ProviderRouteOption `json:"routes"`
}

type ProviderRouteOption struct {
	Provider      string `json:"provider"`
	UpstreamModel string `json:"upstream_model,omitempty"`
	AuthType      string `json:"auth_type,omitempty"`
	Strategy      string `json:"strategy,omitempty"`
	Weight        int    `json:"weight,omitempty"`
	Priority      int    `json:"priority,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`
}

type ProviderRoute struct {
	Provider      string
	UpstreamModel string
	AuthType      string
	Strategy      string
}

type ProviderRouteTrace struct {
	MatchedConfig    bool
	MatchedKind      string
	MatchedModelCode string
	FallbackReason   string
}

// ProviderRouteService resolves public model routes from system_config.
type ProviderRouteService struct {
	cfg *SystemConfigService
}

func NewProviderRouteService(cfg *SystemConfigService) *ProviderRouteService {
	return &ProviderRouteService{cfg: cfg}
}

func (s *ProviderRouteService) Resolve(ctx context.Context, kind provider.Kind, modelCode, fallbackProvider string) ProviderRoute {
	route, _ := s.ResolveExplain(ctx, kind, modelCode, fallbackProvider)
	return route
}

func (s *ProviderRouteService) ResolveExplain(ctx context.Context, kind provider.Kind, modelCode, fallbackProvider string) (ProviderRoute, ProviderRouteTrace) {
	route := ProviderRoute{
		Provider: strings.TrimSpace(fallbackProvider),
		Strategy: "round_robin",
	}
	trace := ProviderRouteTrace{}
	if s == nil || s.cfg == nil {
		trace.FallbackReason = "provider.routes 服务未初始化"
		return route.withDefaults(modelCode), trace
	}
	raw := s.cfg.GetString(ctx, SettingProviderRoutes, "")
	if strings.TrimSpace(raw) == "" {
		trace.FallbackReason = "provider.routes 未配置"
		return route.withDefaults(modelCode), trace
	}
	var rules []ProviderRouteRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		trace.FallbackReason = "provider.routes 解析失败"
		return route.withDefaults(modelCode), trace
	}
	rule, ok := findProviderRouteRule(rules, string(kind), modelCode)
	if !ok {
		trace.FallbackReason = "没有匹配的模型路由规则"
		return route.withDefaults(modelCode), trace
	}
	trace.MatchedKind = strings.TrimSpace(rule.Kind)
	trace.MatchedModelCode = strings.TrimSpace(rule.ModelCode)
	strategy := normalizeRouteStrategy(rule.Strategy)
	option, ok := pickProviderRouteOption(rule.Routes)
	if !ok {
		trace.FallbackReason = "匹配规则没有启用上游路线"
		return route.withDefaults(modelCode), trace
	}
	if option.Provider != "" {
		route.Provider = strings.TrimSpace(option.Provider)
	}
	if option.UpstreamModel != "" {
		route.UpstreamModel = strings.TrimSpace(option.UpstreamModel)
	}
	if option.AuthType != "" {
		route.AuthType = strings.TrimSpace(option.AuthType)
	}
	if option.Strategy != "" {
		strategy = normalizeRouteStrategy(option.Strategy)
	}
	route.Strategy = strategy
	trace.MatchedConfig = true
	return route.withDefaults(modelCode), trace
}

func (r ProviderRoute) withDefaults(modelCode string) ProviderRoute {
	if strings.TrimSpace(r.Strategy) == "" {
		r.Strategy = "round_robin"
	}
	r.Strategy = normalizeRouteStrategy(r.Strategy)
	if strings.TrimSpace(r.UpstreamModel) == "" {
		r.UpstreamModel = strings.TrimSpace(modelCode)
	}
	return r
}

func findProviderRouteRule(rules []ProviderRouteRule, kind, modelCode string) (ProviderRouteRule, bool) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	modelCode = strings.TrimSpace(modelCode)
	bestScore := -1
	var best ProviderRouteRule
	for _, rule := range rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		kindScore := routeKindScore(strings.ToLower(strings.TrimSpace(rule.Kind)), kind)
		if kindScore < 0 {
			continue
		}
		m := strings.TrimSpace(rule.ModelCode)
		if m != "" && m != "*" && !strings.EqualFold(m, modelCode) {
			continue
		}
		modelScore := 0
		if strings.EqualFold(m, modelCode) {
			modelScore = 10
		}
		score := kindScore + modelScore
		if score > bestScore {
			bestScore = score
			best = rule
		}
	}
	if bestScore < 0 {
		return ProviderRouteRule{}, false
	}
	return best, true
}

func routeKindMatches(ruleKind, requested string) bool {
	return routeKindScore(ruleKind, requested) >= 0
}

func routeKindScore(ruleKind, requested string) int {
	if ruleKind == "" || ruleKind == "*" {
		return 0
	}
	if ruleKind == requested {
		return 5
	}
	if (ruleKind == "text" && requested == string(provider.KindChat)) ||
		(ruleKind == string(provider.KindChat) && requested == "text") {
		return 4
	}
	return -1
}

func pickProviderRouteOption(options []ProviderRouteOption) (ProviderRouteOption, bool) {
	filtered := make([]ProviderRouteOption, 0, len(options))
	for _, option := range options {
		if option.Enabled != nil && !*option.Enabled {
			continue
		}
		if strings.TrimSpace(option.Provider) == "" {
			continue
		}
		filtered = append(filtered, option)
	}
	if len(filtered) == 0 {
		return ProviderRouteOption{}, false
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Priority == filtered[j].Priority {
			return filtered[i].Weight > filtered[j].Weight
		}
		return filtered[i].Priority < filtered[j].Priority
	})
	return filtered[0], true
}

func normalizeRouteStrategy(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "weighted", "weighted_rr", "weight", "weight_rr":
		return "weighted_rr"
	default:
		return "round_robin"
	}
}

// NormalizeProviderRouteRulesConfig validates admin-provided provider.routes
// before it is persisted, so bad routing config fails loudly instead of
// silently falling back at generation time.
func NormalizeProviderRouteRulesConfig(v any) ([]ProviderRouteRule, error) {
	var rules []ProviderRouteRule
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []ProviderRouteRule:
		rules = x
	case []any:
		raw, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("provider.routes 编码失败: %w", err)
		}
		if err := json.Unmarshal(raw, &rules); err != nil {
			return nil, fmt.Errorf("provider.routes 必须是规则数组: %w", err)
		}
	default:
		return nil, fmt.Errorf("provider.routes 必须是规则数组")
	}

	out := make([]ProviderRouteRule, 0, len(rules))
	seen := map[string]bool{}
	for i, rule := range rules {
		normalized, err := normalizeProviderRouteRuleForConfig(rule, i)
		if err != nil {
			return nil, err
		}
		if normalized.Enabled == nil || *normalized.Enabled {
			key := strings.ToLower(strings.TrimSpace(normalized.Kind)) + ":" + strings.ToLower(strings.TrimSpace(normalized.ModelCode))
			if seen[key] {
				return nil, fmt.Errorf("provider.routes 第 %d 条规则重复: %s / %s", i+1, normalized.Kind, normalized.ModelCode)
			}
			seen[key] = true
		}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeProviderRouteRuleForConfig(rule ProviderRouteRule, index int) (ProviderRouteRule, error) {
	kind := strings.ToLower(strings.TrimSpace(rule.Kind))
	if !validProviderRouteKind(kind) {
		return ProviderRouteRule{}, fmt.Errorf("provider.routes 第 %d 条规则入口只能是 image/text/video/chat/*", index+1)
	}
	modelCode := strings.TrimSpace(rule.ModelCode)
	if modelCode == "" {
		return ProviderRouteRule{}, fmt.Errorf("provider.routes 第 %d 条规则模型编码不能为空，可填写 * 表示通配", index+1)
	}
	strategy, err := normalizeRouteStrategyForConfig(rule.Strategy)
	if err != nil {
		return ProviderRouteRule{}, fmt.Errorf("provider.routes 第 %d 条规则%s", index+1, err.Error())
	}
	enabled := true
	if rule.Enabled != nil {
		enabled = *rule.Enabled
	}
	routes := make([]ProviderRouteOption, 0, len(rule.Routes))
	for i, option := range rule.Routes {
		normalized, err := normalizeProviderRouteOptionForConfig(option, index, i, strategy)
		if err != nil {
			return ProviderRouteRule{}, err
		}
		routes = append(routes, normalized)
	}
	if enabled && !hasEnabledProviderRoute(routes) {
		return ProviderRouteRule{}, fmt.Errorf("provider.routes 第 %d 条启用规则必须至少配置 1 条启用上游路线", index+1)
	}
	return ProviderRouteRule{
		Kind:      kind,
		ModelCode: modelCode,
		Enabled:   providerRouteBoolPtr(enabled),
		Strategy:  strategy,
		Routes:    routes,
	}, nil
}

func normalizeProviderRouteOptionForConfig(option ProviderRouteOption, ruleIndex, optionIndex int, ruleStrategy string) (ProviderRouteOption, error) {
	providerName := strings.ToLower(strings.TrimSpace(option.Provider))
	if providerName != model.ProviderGPT && providerName != model.ProviderGROK {
		return ProviderRouteOption{}, fmt.Errorf("provider.routes 第 %d 条规则第 %d 条上游账号池只能是 gpt 或 grok", ruleIndex+1, optionIndex+1)
	}
	authType := strings.ToLower(strings.TrimSpace(option.AuthType))
	if authType != "" && authType != model.AuthTypeAPIKey && authType != model.AuthTypeCookie && authType != model.AuthTypeOAuth {
		return ProviderRouteOption{}, fmt.Errorf("provider.routes 第 %d 条规则第 %d 条认证类型只能是 api_key/cookie/oauth 或留空", ruleIndex+1, optionIndex+1)
	}
	strategy := ""
	if strings.TrimSpace(option.Strategy) != "" {
		normalized, err := normalizeRouteStrategyForConfig(option.Strategy)
		if err != nil {
			return ProviderRouteOption{}, fmt.Errorf("provider.routes 第 %d 条规则第 %d 条上游%s", ruleIndex+1, optionIndex+1, err.Error())
		}
		strategy = normalized
	}
	if option.Priority < 0 || option.Priority > 10000 {
		return ProviderRouteOption{}, fmt.Errorf("provider.routes 第 %d 条规则第 %d 条优先级必须是 0-10000", ruleIndex+1, optionIndex+1)
	}
	weight := option.Weight
	if weight == 0 {
		weight = 1
	}
	if weight < 0 || weight > 10000 {
		return ProviderRouteOption{}, fmt.Errorf("provider.routes 第 %d 条规则第 %d 条权重必须是 1-10000", ruleIndex+1, optionIndex+1)
	}
	if ruleStrategy == "weighted_rr" && weight <= 0 {
		return ProviderRouteOption{}, fmt.Errorf("provider.routes 第 %d 条规则第 %d 条权重必须大于 0", ruleIndex+1, optionIndex+1)
	}
	enabled := true
	if option.Enabled != nil {
		enabled = *option.Enabled
	}
	return ProviderRouteOption{
		Provider:      providerName,
		UpstreamModel: strings.TrimSpace(option.UpstreamModel),
		AuthType:      authType,
		Strategy:      strategy,
		Weight:        weight,
		Priority:      option.Priority,
		Enabled:       providerRouteBoolPtr(enabled),
	}, nil
}

func validProviderRouteKind(kind string) bool {
	switch kind {
	case "image", "text", "video", string(provider.KindChat), "*":
		return true
	default:
		return false
	}
}

func normalizeRouteStrategyForConfig(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return "round_robin", nil
	case "round_robin":
		return "round_robin", nil
	case "weighted", "weighted_rr", "weight", "weight_rr":
		return "weighted_rr", nil
	default:
		return "", fmt.Errorf("策略只能是 round_robin 或 weighted_rr")
	}
}

func hasEnabledProviderRoute(routes []ProviderRouteOption) bool {
	for _, route := range routes {
		if route.Enabled == nil || *route.Enabled {
			return true
		}
	}
	return false
}

func providerRouteBoolPtr(v bool) *bool {
	return &v
}

func matchesRouteAuthType(acc *model.Account, authType string) bool {
	authType = strings.TrimSpace(authType)
	if authType == "" {
		return true
	}
	return acc != nil && strings.EqualFold(acc.AuthType, authType)
}

func accountAllowsRouteModel(acc *model.Account, publicModel, upstreamModel string) bool {
	if acc == nil || acc.ModelWhitelist == nil || strings.TrimSpace(*acc.ModelWhitelist) == "" {
		return true
	}
	raw := strings.TrimSpace(*acc.ModelWhitelist)
	allowed := map[string]bool{}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		for _, item := range arr {
			if s := strings.TrimSpace(item); s != "" {
				allowed[strings.ToLower(s)] = true
			}
		}
	} else {
		for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			if s := strings.TrimSpace(item); s != "" {
				allowed[strings.ToLower(s)] = true
			}
		}
	}
	if len(allowed) == 0 {
		return true
	}
	if allowed["*"] {
		return true
	}
	if publicModel != "" && allowed[strings.ToLower(strings.TrimSpace(publicModel))] {
		return true
	}
	return upstreamModel != "" && allowed[strings.ToLower(strings.TrimSpace(upstreamModel))]
}
