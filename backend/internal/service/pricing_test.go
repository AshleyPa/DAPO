package service

import (
	"testing"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
)

func TestDefaultImagePriceMatrix(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   int64
	}{
		{name: "text 1k standard", params: map[string]any{"mode": "t2i", "ratio": "9:16", "resolution": "1K"}, want: 400},
		{name: "text 1k extended", params: map[string]any{"mode": "t2i", "ratio": "21:9", "resolution": "1K"}, want: 500},
		{name: "text 4k standard", params: map[string]any{"mode": "t2i", "ratio": "3:4", "resolution": "4K"}, want: 800},
		{name: "image to image 2k standard", params: map[string]any{"mode": "i2i", "ratio": "4:5", "resolution": "2K"}, want: 800},
		{name: "image to image 4k extended", params: map[string]any{"mode": "i2i", "ratio": "2:3", "resolution": "4K"}, want: 1100},
		{name: "defaults to text 1k square", params: map[string]any{}, want: 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultPriceFn("gpt-image-2", provider.KindImage, tc.params)
			if got != tc.want {
				t.Fatalf("DefaultPriceFn() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDefaultImagePriceMatrixFallsBackForOtherImageModels(t *testing.T) {
	got := DefaultPriceFn("img-v3", provider.KindImage, map[string]any{"mode": "t2i", "ratio": "21:9", "resolution": "4K"})
	if got != 400 {
		t.Fatalf("DefaultPriceFn(img-v3) = %d, want model fallback 400", got)
	}
}

func TestImagePriceMatrixUsesCustomEnabledRule(t *testing.T) {
	enabled := true
	disabled := false
	rules := []ImagePriceRule{
		{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "extended", Ratios: []string{"21:9"}, Resolution: "4K", UnitPoints: 1900, Enabled: &disabled},
		{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "extended", Ratios: []string{"21:9"}, Resolution: "4K", UnitPoints: 1200, Enabled: &enabled},
	}
	got, ok := imagePriceFromRules(rules, "gpt-image-2", map[string]any{"mode": "t2i", "ratio": "21:9", "resolution": "4K"})
	if !ok {
		t.Fatal("imagePriceFromRules() did not match custom rule")
	}
	if got != 1200 {
		t.Fatalf("imagePriceFromRules() = %d, want 1200", got)
	}
}

func TestImagePriceMatrixMatchesOptionalQuality(t *testing.T) {
	rules := []ImagePriceRule{
		{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "standard", Resolution: "1K", Quality: "draft", UnitPoints: 300},
		{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "standard", Resolution: "1K", Quality: "high", UnitPoints: 500},
	}
	got, ok := imagePriceFromRules(rules, "gpt-image-2", map[string]any{"mode": "t2i", "ratio": "1:1", "resolution": "1K", "quality": "high"})
	if !ok {
		t.Fatal("imagePriceFromRules() did not match quality-specific rule")
	}
	if got != 500 {
		t.Fatalf("imagePriceFromRules() = %d, want high-quality price 500", got)
	}
}

func TestCatalogChatPriceUsesModelCatalogTokenFields(t *testing.T) {
	item := &model.ModelCatalog{
		ModelCode:        "mimo-v2.5-pro",
		EntryKind:        model.ModelCatalogKindText,
		Status:           model.ModelCatalogStatusEnabled,
		InputUnitPoints:  120,
		OutputUnitPoints: 360,
	}
	got, ok := catalogChatPrice(item)
	if !ok {
		t.Fatal("catalogChatPrice() did not match enabled text model")
	}
	if got.InputPerK != 120 || got.OutputPerK != 360 {
		t.Fatalf("catalogChatPrice() = %+v, want input 120 output 360", got)
	}
}

func TestCatalogChatPriceSupportsCharacterPricing(t *testing.T) {
	item := &model.ModelCatalog{
		ModelCode:        "deepseek-chat",
		EntryKind:        model.ModelCatalogKindText,
		Status:           model.ModelCatalogStatusEnabled,
		PricingMode:      model.ModelCatalogPricingChar,
		InputUnitPoints:  50,
		OutputUnitPoints: 200,
	}
	price, ok := catalogChatPrice(item)
	if !ok {
		t.Fatal("catalogChatPrice() did not match enabled char-priced model")
	}
	if price.UnitBasis != ChatPriceBasisChars {
		t.Fatalf("UnitBasis = %q, want %q", price.UnitBasis, ChatPriceBasisChars)
	}
	body := map[string]any{
		"messages":   []map[string]any{{"role": "user", "content": "你好"}},
		"max_tokens": 10,
	}
	estimated := ChatEstimatedCost(price, body)
	if estimated <= 0 {
		t.Fatalf("ChatEstimatedCost() = %d, want positive", estimated)
	}
	actual, ok := ChatActualCost(price, body, nil, map[string]any{"content_chars": 6})
	if !ok {
		t.Fatal("ChatActualCost() did not calculate char usage")
	}
	if actual <= 0 || actual >= estimated {
		t.Fatalf("ChatActualCost() = %d, estimated = %d, want positive refundable actual", actual, estimated)
	}
}

func TestCatalogImagePriceUsesCatalogMatrixRules(t *testing.T) {
	rules := `[{"model_code":"gpt-image-2","mode":"t2i","ratio_group":"extended","ratios":["21:9"],"resolution":"4K","unit_points":1500}]`
	item := &model.ModelCatalog{
		ModelCode:   "gpt-image-2",
		EntryKind:   model.ModelCatalogKindImage,
		Status:      model.ModelCatalogStatusEnabled,
		PricingMode: model.ModelCatalogPricingMatrix,
		PriceRules:  &rules,
		UnitPoints:  400,
	}
	got, ok := catalogPrice(item, provider.KindImage, map[string]any{"mode": "t2i", "ratio": "21:9", "resolution": "4K"})
	if !ok {
		t.Fatal("catalogPrice() did not match catalog image matrix")
	}
	if got != 1500 {
		t.Fatalf("catalogPrice(image) = %d, want 1500", got)
	}
}

func TestCatalogImagePriceIgnoresRulesOutsideMatrixMode(t *testing.T) {
	rules := `[{"model_code":"gpt-image-2","mode":"t2i","ratio_group":"extended","ratios":["21:9"],"resolution":"4K","unit_points":1500}]`
	item := &model.ModelCatalog{
		ModelCode:   "gpt-image-2",
		EntryKind:   model.ModelCatalogKindImage,
		Status:      model.ModelCatalogStatusEnabled,
		PricingMode: model.ModelCatalogPricingFixed,
		PriceRules:  &rules,
		UnitPoints:  400,
	}
	got, ok := catalogPrice(item, provider.KindImage, map[string]any{"mode": "t2i", "ratio": "21:9", "resolution": "4K"})
	if !ok {
		t.Fatal("catalogPrice() did not fall back to fixed unit price")
	}
	if got != 400 {
		t.Fatalf("catalogPrice(image) = %d, want fixed unit price 400", got)
	}
}

func TestCatalogVideoPriceUsesDurationTier(t *testing.T) {
	rules := `[
		{"model_code":"sora2","mode":"t2v","duration_sec":6,"unit_points":1800},
		{"model_code":"sora2","mode":"t2v","duration_sec":10,"unit_points":2600}
	]`
	item := &model.ModelCatalog{
		ModelCode:   "sora2",
		EntryKind:   model.ModelCatalogKindVideo,
		Status:      model.ModelCatalogStatusEnabled,
		PricingMode: model.ModelCatalogPricingMatrix,
		PriceRules:  &rules,
		UnitPoints:  1800,
	}
	got, ok := catalogPrice(item, provider.KindVideo, map[string]any{"mode": "t2v", "duration": float64(9)})
	if !ok {
		t.Fatal("catalogPrice() did not match catalog video matrix")
	}
	if got != 2600 {
		t.Fatalf("catalogPrice(video) = %d, want 2600", got)
	}
}

func TestGenerationPricingAuditSnapshotIncludesMatchedImageRule(t *testing.T) {
	rules := []ImagePriceRule{
		{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "extended", Ratios: []string{"21:9"}, Resolution: "4K", Quality: "high", UnitPoints: 1500},
	}
	rule, ok := imagePriceRuleMatch(rules, "gpt-image-2", map[string]any{"mode": "t2i", "ratio": "21:9", "resolution": "4K", "quality": "high"})
	if !ok {
		t.Fatal("imagePriceRuleMatch() did not match rule")
	}
	audit := imagePriceRuleAudit(rule)
	if audit["unit_points"] != int64(1500) {
		t.Fatalf("unit_points = %#v, want 1500", audit["unit_points"])
	}
	if audit["ratio_group"] != "extended" {
		t.Fatalf("ratio_group = %#v", audit["ratio_group"])
	}
	if audit["quality"] != "high" {
		t.Fatalf("quality = %#v", audit["quality"])
	}
}

func TestChatPricingResultPatchShowsRefundAndUsage(t *testing.T) {
	patch := ChatPricingResultPatch(&ChatUsage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}, 500, 320)
	if patch["settlement"] != "partial_refund" {
		t.Fatalf("settlement = %#v", patch["settlement"])
	}
	if patch["refund_points"] != int64(180) {
		t.Fatalf("refund_points = %#v", patch["refund_points"])
	}
	usage, ok := patch["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage = %#v, want object", patch["usage"])
	}
	if usage["total_tokens"] != 30 {
		t.Fatalf("total_tokens = %#v, want 30", usage["total_tokens"])
	}
}

func TestProviderRouteSnapshotIncludesSkippedCandidates(t *testing.T) {
	payload := providerRouteSnapshotPayload("gpt-image-2", string(provider.KindImage), []ProviderRoute{
		{
			SourceType:    model.ModelSourceTypeAPIChannel,
			SourceCode:    "newapi-image",
			Provider:      generationProviderAPIChannel,
			Adapter:       model.APIChannelAdapterOpenAIImages,
			UpstreamModel: "gpt-image-2",
			Strategy:      "round_robin",
			ImageAPIMode:  ProviderRouteImageAPIModeOpenAIImages,
			Priority:      10,
			Weight:        60,
		},
	}, 0, []ProviderRoute{
		{
			SourceType:    model.ModelSourceTypeAccountPool,
			SourceCode:    model.ProviderGPT,
			Provider:      model.ProviderGPT,
			UpstreamModel: "mimo-v2.5-pro",
			SkipReason:    "账号池存在账号，但认证类型或模型白名单过滤后无可用账号",
			Priority:      20,
			Weight:        1,
		},
	})
	if payload["candidate_count"] != 1 {
		t.Fatalf("candidate_count = %#v, want 1", payload["candidate_count"])
	}
	if payload["skipped_count"] != 1 {
		t.Fatalf("skipped_count = %#v, want 1", payload["skipped_count"])
	}
	skipped, ok := payload["skipped_candidates"].([]map[string]any)
	if !ok || len(skipped) != 1 {
		t.Fatalf("skipped_candidates = %#v, want one candidate", payload["skipped_candidates"])
	}
	if got := skipped[0]["skip_reason"]; got != "账号池存在账号，但认证类型或模型白名单过滤后无可用账号" {
		t.Fatalf("skip_reason = %#v", got)
	}
	candidates, ok := payload["candidates"].([]map[string]any)
	if !ok || len(candidates) != 1 {
		t.Fatalf("candidates = %#v, want one candidate", payload["candidates"])
	}
	if candidates[0]["priority"] != 10 || candidates[0]["weight"] != 60 {
		t.Fatalf("candidate priority/weight missing: %#v", candidates[0])
	}
	if candidates[0]["index"] != 1 {
		t.Fatalf("candidate index = %#v, want 1", candidates[0]["index"])
	}
	if skipped[0]["priority"] != 20 || skipped[0]["weight"] != 1 {
		t.Fatalf("skipped priority/weight missing: %#v", skipped[0])
	}
	if skipped[0]["index"] != 1 {
		t.Fatalf("skipped index = %#v, want 1", skipped[0]["index"])
	}
}
