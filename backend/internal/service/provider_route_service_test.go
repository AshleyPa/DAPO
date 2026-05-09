package service

import (
	"context"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
)

func TestProviderRouteKindTextMatchesChat(t *testing.T) {
	if !routeKindMatches("text", "chat") {
		t.Fatalf("text route should match chat requests")
	}
	if !routeKindMatches("chat", "text") {
		t.Fatalf("chat route should match text requests")
	}
	if routeKindMatches("video", "chat") {
		t.Fatalf("video route should not match chat requests")
	}
}

func TestPickProviderRouteOptionPriorityBeforeWeight(t *testing.T) {
	no := false
	got, ok := pickProviderRouteOption([]ProviderRouteOption{
		{Provider: "gpt", Priority: 5, Weight: 100},
		{Provider: "grok", Priority: 1, Weight: 1},
		{Provider: "skip", Priority: 0, Weight: 999, Enabled: &no},
	})
	if !ok {
		t.Fatalf("expected route option")
	}
	if got.Provider != "grok" {
		t.Fatalf("expected grok to win lower priority, got %s", got.Provider)
	}
}

func TestPickProviderRouteOptionsKeepsFallbackOrder(t *testing.T) {
	no := false
	got := pickProviderRouteOptions([]ProviderRouteOption{
		{Provider: "gpt", Priority: 20, Weight: 100},
		{Provider: "grok", Priority: 10, Weight: 1},
		{Provider: "gpt", Priority: 10, Weight: 50, UpstreamModel: "gpt-image-2"},
		{Provider: "skip", Priority: 0, Weight: 999, Enabled: &no},
	})
	if len(got) != 3 {
		t.Fatalf("expected three enabled candidates, got %d", len(got))
	}
	if got[0].Provider != "gpt" || got[0].UpstreamModel != "gpt-image-2" {
		t.Fatalf("expected same-priority candidate with higher weight first, got %#v", got[0])
	}
	if got[1].Provider != "grok" {
		t.Fatalf("expected grok second, got %#v", got[1])
	}
	if got[2].Provider != "gpt" || got[2].Priority != 20 {
		t.Fatalf("expected lower priority candidate last, got %#v", got[2])
	}
}

func TestFindProviderRouteRulePrefersSpecificModel(t *testing.T) {
	rule, ok := findProviderRouteRule([]ProviderRouteRule{
		{Kind: "text", ModelCode: "*", Routes: []ProviderRouteOption{{Provider: "grok"}}},
		{Kind: "text", ModelCode: "gpt-4o-mini", Routes: []ProviderRouteOption{{Provider: "gpt"}}},
	}, "chat", "gpt-4o-mini")
	if !ok {
		t.Fatalf("expected route rule")
	}
	if rule.ModelCode != "gpt-4o-mini" {
		t.Fatalf("expected specific route rule, got %s", rule.ModelCode)
	}
}

func TestAccountAllowsRouteModel(t *testing.T) {
	raw := `["gpt-image-2","grok-4.20-fast"]`
	acc := &model.Account{ModelWhitelist: &raw}
	if !accountAllowsRouteModel(acc, "gpt-image-2", "gpt-image-2") {
		t.Fatalf("expected public model whitelist match")
	}
	if accountAllowsRouteModel(acc, "gpt-4o-mini", "gpt-4o-mini") {
		t.Fatalf("unexpected non-whitelisted model match")
	}
}

func TestNormalizeProviderRouteRulesConfigNormalizes(t *testing.T) {
	got, err := NormalizeProviderRouteRulesConfig([]any{
		map[string]any{
			"kind":       "TEXT",
			"model_code": "*",
			"enabled":    true,
			"strategy":   "weighted",
			"routes": []any{
				map[string]any{"provider": "GROK", "priority": 1, "weight": 100, "auth_type": ""},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one rule, got %d", len(got))
	}
	if got[0].Kind != "text" || got[0].Strategy != "weighted_rr" {
		t.Fatalf("unexpected normalized rule: %#v", got[0])
	}
	if got[0].Routes[0].Provider != "grok" || got[0].Routes[0].Weight != 100 {
		t.Fatalf("unexpected normalized option: %#v", got[0].Routes[0])
	}
}

func TestNormalizeProviderRouteRulesConfigRejectsString(t *testing.T) {
	if _, err := NormalizeProviderRouteRulesConfig(`[{"kind":"image"}]`); err == nil {
		t.Fatalf("expected string provider.routes to be rejected")
	}
}

func TestNormalizeProviderRouteRulesConfigRejectsInvalidProvider(t *testing.T) {
	_, err := NormalizeProviderRouteRulesConfig([]ProviderRouteRule{{
		Kind:      "image",
		ModelCode: "gpt-image-2",
		Routes:    []ProviderRouteOption{{Provider: "openai", Weight: 1}},
	}})
	if err == nil {
		t.Fatalf("expected invalid provider to be rejected")
	}
}

func TestNormalizeProviderRouteRulesConfigRejectsDuplicateEnabledRule(t *testing.T) {
	_, err := NormalizeProviderRouteRulesConfig([]ProviderRouteRule{
		{Kind: "text", ModelCode: "*", Routes: []ProviderRouteOption{{Provider: "grok", Weight: 1}}},
		{Kind: "text", ModelCode: "*", Routes: []ProviderRouteOption{{Provider: "gpt", Weight: 1}}},
	})
	if err == nil {
		t.Fatalf("expected duplicate enabled rules to be rejected")
	}
}

func TestProviderRouteResolveExplainReportsFallbackReason(t *testing.T) {
	svc := &ProviderRouteService{}
	route, trace := svc.ResolveExplain(context.Background(), "image", "gpt-image-2", "gpt")
	if route.Provider != "gpt" || route.UpstreamModel != "gpt-image-2" {
		t.Fatalf("unexpected fallback route: %#v", route)
	}
	if trace.MatchedConfig {
		t.Fatalf("unexpected matched config")
	}
	if trace.FallbackReason == "" {
		t.Fatalf("expected fallback reason")
	}
}

func TestProviderRouteResolveCandidatesFallsBackWhenUninitialized(t *testing.T) {
	svc := &ProviderRouteService{}
	routes, trace := svc.ResolveCandidates(context.Background(), "image", "gpt-image-2", "gpt")
	if len(routes) != 1 {
		t.Fatalf("expected one fallback route, got %d", len(routes))
	}
	if routes[0].Provider != "gpt" || routes[0].UpstreamModel != "gpt-image-2" || routes[0].Strategy != "round_robin" {
		t.Fatalf("unexpected fallback route: %#v", routes[0])
	}
	if trace.MatchedConfig || trace.FallbackReason == "" {
		t.Fatalf("expected fallback trace, got %#v", trace)
	}
}

func TestDecodeProviderRouteCandidates(t *testing.T) {
	raw := []any{
		map[string]any{"provider": "gpt", "upstream_model": "gpt-image-2", "strategy": "weighted_rr"},
		map[string]any{"provider": "gpt", "upstream_model": "gpt-image-2", "strategy": "weighted_rr"},
		map[string]any{"provider": "grok", "auth_type": "cookie"},
	}
	got := decodeProviderRouteCandidates(raw, "public-model")
	if len(got) != 2 {
		t.Fatalf("expected duplicate candidates to be removed, got %d", len(got))
	}
	if got[0].Provider != "gpt" || got[0].UpstreamModel != "gpt-image-2" || got[0].Strategy != "weighted_rr" {
		t.Fatalf("unexpected first candidate: %#v", got[0])
	}
	if got[1].Provider != "grok" || got[1].UpstreamModel != "public-model" || got[1].AuthType != "cookie" {
		t.Fatalf("unexpected second candidate: %#v", got[1])
	}
}

func TestDefaultProviderRouteFallback(t *testing.T) {
	if got := defaultProviderRouteFallback("image", "gpt-image-2"); got != "gpt" {
		t.Fatalf("expected image fallback gpt, got %s", got)
	}
	if got := defaultProviderRouteFallback("video", "grok-video"); got != "grok" {
		t.Fatalf("expected video fallback grok, got %s", got)
	}
	if got := defaultProviderRouteFallback("chat", "grok-4.20-fast"); got != "grok" {
		t.Fatalf("expected grok chat fallback, got %s", got)
	}
}

func TestBuildProviderHealthSummaryGroupsAuthAndErrors(t *testing.T) {
	now := time.Unix(1000, 0)
	lastErr := "rate limited"
	testErr := "token expired"
	testAt := now.Add(-time.Minute)
	cooldown := now.Add(time.Minute)
	expireAt := now.Add(-time.Minute)
	rows := []*repo.ProviderHealthRow{{
		Provider:        "gpt",
		Total:           3,
		Enabled:         2,
		Available:       1,
		CooldownActive:  1,
		TokenExpired:    1,
		LastTestOK:      1,
		LastTestFail:    1,
		LastTestUnknown: 1,
		QuotaZero:       1,
		SuccessCount:    7,
		ErrorCount:      2,
	}}
	authRows := []*repo.ProviderHealthAuthRow{{
		Provider:       "gpt",
		AuthType:       "oauth",
		Total:          2,
		Available:      1,
		CooldownActive: 1,
		LastTestOK:     1,
		LastTestFail:   1,
	}}
	errorRows := []*repo.ProviderHealthErrorRow{{
		ID:             42,
		Provider:       "gpt",
		Name:           "gpt-oauth",
		AuthType:       "oauth",
		Status:         model.AccountStatusBroken,
		ErrorCount:     2,
		LastError:      &lastErr,
		LastTestError:  &testErr,
		LastTestAt:     &testAt,
		CooldownUntil:  &cooldown,
		AccessTokenExp: &expireAt,
		UpdatedAt:      now,
	}}
	got := buildProviderHealthSummary(now, rows, authRows, errorRows)
	if got.RefreshedAt != now.Unix() || len(got.Providers) != 1 {
		t.Fatalf("unexpected summary: %#v", got)
	}
	p := got.Providers[0]
	if p.Provider != "gpt" || p.Available != 1 || p.TokenExpired != 1 || p.ErrorCount != 2 {
		t.Fatalf("unexpected provider row: %#v", p)
	}
	if len(p.AuthTypes) != 1 || p.AuthTypes[0].AuthType != "oauth" || p.AuthTypes[0].CooldownActive != 1 {
		t.Fatalf("unexpected auth breakdown: %#v", p.AuthTypes)
	}
	if len(p.RecentErrors) != 1 || p.RecentErrors[0].AccountID != 42 || p.RecentErrors[0].LastError != lastErr {
		t.Fatalf("unexpected error samples: %#v", p.RecentErrors)
	}
}
