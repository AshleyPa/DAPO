package service

import (
	"context"
	"testing"

	"github.com/kleinai/backend/internal/model"
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
