// Package service 模型计费表（开发期内置；后续从 model 表读取并缓存）。
package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
	"github.com/kleinai/backend/internal/repo"
)

// DefaultPriceTable 默认计费（与 migrations/seed 对齐）。
//
// 单位：点 *100。例：400 = 4 点 / 张图。
var DefaultPriceTable = map[string]int64{
	"gpt-4o-mini":        100,
	"gpt-image-2":        400,
	"vid-v1":             1500, // 4 秒视频
	"vid-i2v":            2000,
	"grok-imagine-video": 2000,
}

const SettingBillingImagePriceRules = "billing.image_price_rules"

const (
	PricingAuditSnapshotKey = "_model_gateway_pricing_snapshot"
	OutputAuditSnapshotKey  = "_model_gateway_output_snapshot"
)

// ImagePriceRule describes per-image pricing by business-facing image options.
// UnitPoints is stored in points*100. Example: 400 = 4 points per image.
type ImagePriceRule struct {
	ModelCode  string   `json:"model_code"`
	Mode       string   `json:"mode"` // t2i / i2i
	RatioGroup string   `json:"ratio_group,omitempty"`
	Ratios     []string `json:"ratios,omitempty"`
	Resolution string   `json:"resolution"`
	Quality    string   `json:"quality,omitempty"`
	UnitPoints int64    `json:"unit_points"`
	Enabled    *bool    `json:"enabled,omitempty"`
}

// VideoPriceRule is the catalog-side matrix shape for video pricing. It keeps
// the legacy "base price per normalized duration" behavior as the fallback,
// while allowing Model Catalog to express explicit duration tiers.
type VideoPriceRule struct {
	ModelCode   string `json:"model_code"`
	Mode        string `json:"mode,omitempty"`
	Quality     string `json:"quality,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	DurationSec int    `json:"duration_sec,omitempty"`
	UnitPoints  int64  `json:"unit_points"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

var defaultStandardImageRatios = []string{"1:1", "16:9", "9:16", "4:3", "3:4", "5:4", "4:5"}
var defaultExtendedImageRatios = []string{"3:2", "2:3", "21:9"}

var DefaultImagePriceRules = []ImagePriceRule{
	{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "standard", Ratios: defaultStandardImageRatios, Resolution: "1K", UnitPoints: 400},
	{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "standard", Ratios: defaultStandardImageRatios, Resolution: "2K", UnitPoints: 600},
	{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "standard", Ratios: defaultStandardImageRatios, Resolution: "4K", UnitPoints: 800},
	{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "extended", Ratios: defaultExtendedImageRatios, Resolution: "1K", UnitPoints: 500},
	{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "extended", Ratios: defaultExtendedImageRatios, Resolution: "2K", UnitPoints: 700},
	{ModelCode: "gpt-image-2", Mode: "t2i", RatioGroup: "extended", Ratios: defaultExtendedImageRatios, Resolution: "4K", UnitPoints: 900},
	{ModelCode: "gpt-image-2", Mode: "i2i", RatioGroup: "standard", Ratios: defaultStandardImageRatios, Resolution: "1K", UnitPoints: 600},
	{ModelCode: "gpt-image-2", Mode: "i2i", RatioGroup: "standard", Ratios: defaultStandardImageRatios, Resolution: "2K", UnitPoints: 800},
	{ModelCode: "gpt-image-2", Mode: "i2i", RatioGroup: "standard", Ratios: defaultStandardImageRatios, Resolution: "4K", UnitPoints: 1000},
	{ModelCode: "gpt-image-2", Mode: "i2i", RatioGroup: "extended", Ratios: defaultExtendedImageRatios, Resolution: "1K", UnitPoints: 700},
	{ModelCode: "gpt-image-2", Mode: "i2i", RatioGroup: "extended", Ratios: defaultExtendedImageRatios, Resolution: "2K", UnitPoints: 900},
	{ModelCode: "gpt-image-2", Mode: "i2i", RatioGroup: "extended", Ratios: defaultExtendedImageRatios, Resolution: "4K", UnitPoints: 1100},
}

const (
	ChatPriceBasisTokens = "per_1k_tokens"
	ChatPriceBasisChars  = "per_1k_chars"
)

// ChatPrice is points*100 per 1K billing units. UnitBasis defaults to tokens.
type ChatPrice struct {
	InputPerK  int64
	OutputPerK int64
	UnitBasis  string
}

// DefaultChatPriceFn returns default token prices in points*100 per 1K tokens.
func DefaultChatPriceFn(modelCode string) ChatPrice {
	switch modelCode {
	case "gpt-4o-mini":
		return ChatPrice{InputPerK: 100, OutputPerK: 300}
	case "grok-4.20-fast":
		return ChatPrice{InputPerK: 100, OutputPerK: 300}
	case "grok-4.20-auto":
		return ChatPrice{InputPerK: 150, OutputPerK: 450}
	case "grok-4.20-expert":
		return ChatPrice{InputPerK: 200, OutputPerK: 600}
	case "grok-4.20-heavy":
		return ChatPrice{InputPerK: 400, OutputPerK: 1200}
	case "grok-4.3-beta":
		return ChatPrice{InputPerK: 300, OutputPerK: 900}
	default:
		return ChatPrice{InputPerK: 100, OutputPerK: 300}
	}
}

// DefaultPriceFn 实现 PriceFunc。
func DefaultPriceFn(modelCode string, kind provider.Kind, params map[string]any) int64 {
	if kind == provider.KindImage {
		if v, ok := imagePriceFromRules(DefaultImagePriceRules, modelCode, params); ok {
			return v
		}
	}
	if v, ok := DefaultPriceTable[modelCode]; ok {
		// 视频：按秒倍率
		if kind == provider.KindVideo {
			if dur, ok2 := params["duration"].(float64); ok2 {
				dur = float64(normalizeBillingVideoDuration(int(dur)))
				if dur <= 6 {
					return v
				}
				factor := dur / 6
				return int64(float64(v) * factor)
			}
		}
		return v
	}
	switch kind {
	case provider.KindImage:
		return 400
	case provider.KindVideo:
		return 1500
	}
	return 0
}

func ConfigPriceFn(cfg *SystemConfigService) PriceFunc {
	return func(modelCode string, kind provider.Kind, params map[string]any) int64 {
		if cfg != nil {
			if kind == provider.KindImage {
				if v, ok := imagePriceFromRules(ConfiguredImagePriceRules(context.Background(), cfg), modelCode, params); ok {
					return v
				}
			}
			raw := cfg.GetString(context.Background(), "billing.model_prices", "")
			if raw != "" {
				var rows []struct {
					ModelCode  string `json:"model_code"`
					UnitPoints int64  `json:"unit_points"`
					Enabled    *bool  `json:"enabled"`
				}
				if err := json.Unmarshal([]byte(raw), &rows); err == nil {
					for _, row := range rows {
						if row.ModelCode != modelCode {
							continue
						}
						if row.Enabled != nil && !*row.Enabled {
							continue
						}
						if kind == provider.KindVideo {
							if dur, ok2 := params["duration"].(float64); ok2 {
								dur = float64(normalizeBillingVideoDuration(int(dur)))
								if dur <= 6 {
									return row.UnitPoints
								}
								return int64(float64(row.UnitPoints) * (dur / 6))
							}
						}
						return row.UnitPoints
					}
				}
				var prices map[string]int64
				if err := json.Unmarshal([]byte(raw), &prices); err == nil {
					if v, ok := prices[modelCode]; ok {
						if kind == provider.KindVideo {
							if dur, ok2 := params["duration"].(float64); ok2 {
								dur = float64(normalizeBillingVideoDuration(int(dur)))
								if dur <= 6 {
									return v
								}
								return int64(float64(v) * (dur / 6))
							}
						}
						return v
					}
				}
			}
		}
		return DefaultPriceFn(modelCode, kind, params)
	}
}

func ModelGatewayPriceFn(cfg *SystemConfigService, modelRepo *repo.ModelCatalogRepo) PriceFunc {
	legacy := ConfigPriceFn(cfg)
	return func(modelCode string, kind provider.Kind, params map[string]any) int64 {
		if modelRepo != nil {
			if item, err := modelRepo.GetByCode(context.Background(), modelCode); err == nil {
				if v, ok := catalogPrice(item, kind, params); ok {
					return v
				}
			}
		}
		return legacy(modelCode, kind, params)
	}
}

func ModelGatewayChatPriceFn(cfg *SystemConfigService, modelRepo *repo.ModelCatalogRepo) func(modelCode string) ChatPrice {
	legacy := ConfigChatPriceFn(cfg)
	return func(modelCode string) ChatPrice {
		if modelRepo != nil {
			if item, err := modelRepo.GetByCode(context.Background(), modelCode); err == nil {
				if price, ok := catalogChatPrice(item); ok {
					return price
				}
			}
		}
		return legacy(modelCode)
	}
}

func catalogPrice(item *model.ModelCatalog, kind provider.Kind, params map[string]any) (int64, bool) {
	if item == nil || item.Status != model.ModelCatalogStatusEnabled || !catalogKindMatches(item.EntryKind, kind) {
		return 0, false
	}
	useMatrixRules := normalizePriceToken(item.PricingMode) == model.ModelCatalogPricingMatrix
	if useMatrixRules && kind == provider.KindImage && item.PriceRules != nil {
		var rules []ImagePriceRule
		if err := json.Unmarshal([]byte(*item.PriceRules), &rules); err == nil && len(rules) > 0 {
			if v, ok := imagePriceFromRules(rules, item.ModelCode, params); ok {
				return v, true
			}
		}
	}
	if useMatrixRules && kind == provider.KindVideo && item.PriceRules != nil {
		var rules []VideoPriceRule
		if err := json.Unmarshal([]byte(*item.PriceRules), &rules); err == nil && len(rules) > 0 {
			if v, ok := videoPriceFromRules(rules, item.ModelCode, params); ok {
				return v, true
			}
		}
	}
	if item.UnitPoints <= 0 {
		return 0, false
	}
	if kind == provider.KindVideo {
		return scaleVideoUnitPrice(item.UnitPoints, params), true
	}
	return item.UnitPoints, true
}

func catalogChatPrice(item *model.ModelCatalog) (ChatPrice, bool) {
	if item == nil || item.Status != model.ModelCatalogStatusEnabled {
		return ChatPrice{}, false
	}
	entry := normalizePriceToken(item.EntryKind)
	if entry != model.ModelCatalogKindText && entry != model.ModelCatalogKindChat {
		return ChatPrice{}, false
	}
	basis := ChatPriceBasisTokens
	if normalizePriceToken(item.PricingMode) == model.ModelCatalogPricingChar {
		basis = ChatPriceBasisChars
	}
	if item.InputUnitPoints > 0 || item.OutputUnitPoints > 0 {
		return ChatPrice{InputPerK: item.InputUnitPoints, OutputPerK: item.OutputUnitPoints, UnitBasis: basis}, true
	}
	if item.UnitPoints > 0 {
		return ChatPrice{InputPerK: item.UnitPoints, OutputPerK: item.UnitPoints, UnitBasis: basis}, true
	}
	return ChatPrice{}, false
}

func catalogKindMatches(entryKind string, kind provider.Kind) bool {
	entry := normalizePriceToken(entryKind)
	switch kind {
	case provider.KindImage:
		return entry == model.ModelCatalogKindImage
	case provider.KindVideo:
		return entry == model.ModelCatalogKindVideo
	default:
		return entry == model.ModelCatalogKindText || entry == model.ModelCatalogKindChat
	}
}

// ConfiguredImagePriceRules returns admin-configured image price rules, or the
// default DAPO matrix when the setting is absent or invalid.
func ConfiguredImagePriceRules(ctx context.Context, cfg *SystemConfigService) []ImagePriceRule {
	if cfg != nil {
		raw := cfg.GetString(ctx, SettingBillingImagePriceRules, "")
		if raw != "" {
			var rules []ImagePriceRule
			if err := json.Unmarshal([]byte(raw), &rules); err == nil && len(rules) > 0 {
				return rules
			}
		}
	}
	return append([]ImagePriceRule(nil), DefaultImagePriceRules...)
}

func ImagePriceRulesForModel(ctx context.Context, cfg *SystemConfigService, modelCode string) []ImagePriceRule {
	rules := ConfiguredImagePriceRules(ctx, cfg)
	return imagePriceRulesForModel(rules, modelCode)
}

func DefaultImagePriceRulesForModel(modelCode string) []ImagePriceRule {
	return imagePriceRulesForModel(DefaultImagePriceRules, modelCode)
}

func imagePriceRulesForModel(rules []ImagePriceRule, modelCode string) []ImagePriceRule {
	out := make([]ImagePriceRule, 0, len(rules))
	for _, rule := range rules {
		if !imagePriceRuleEnabled(rule) || normalizePriceToken(rule.ModelCode) != normalizePriceToken(modelCode) {
			continue
		}
		out = append(out, rule)
	}
	return out
}

func imagePriceFromRules(rules []ImagePriceRule, modelCode string, params map[string]any) (int64, bool) {
	rule, ok := imagePriceRuleMatch(rules, modelCode, params)
	if !ok {
		return 0, false
	}
	return rule.UnitPoints, true
}

func imagePriceRuleMatch(rules []ImagePriceRule, modelCode string, params map[string]any) (ImagePriceRule, bool) {
	modelCode = normalizePriceToken(modelCode)
	if modelCode == "" || len(rules) == 0 {
		return ImagePriceRule{}, false
	}
	mode := imageBillingMode(params)
	ratio := normalizeImageRatio(strParamAny(params, "ratio", strParamAny(params, "aspect_ratio", "1:1")))
	resolution := normalizeImageResolution(strParamAny(params, "resolution", strParamAny(params, "size_tier", "1K")))
	quality := normalizePriceToken(strParamAny(params, "quality", ""))
	ratioGroup := imageRatioGroup(ratio)
	for _, rule := range rules {
		if !imagePriceRuleEnabled(rule) || normalizePriceToken(rule.ModelCode) != modelCode {
			continue
		}
		if normalizePriceToken(rule.Mode) != mode {
			continue
		}
		if normalizeImageResolution(rule.Resolution) != resolution {
			continue
		}
		if !blankOrMatches(rule.Quality, quality) {
			continue
		}
		if imageRuleMatchesRatio(rule, ratio, ratioGroup) && rule.UnitPoints > 0 {
			return rule, true
		}
	}
	return ImagePriceRule{}, false
}

func imageBillingMode(params map[string]any) string {
	mode := normalizePriceToken(strParamAny(params, "mode", "t2i"))
	switch mode {
	case "i2i", "edit", "image_to_image":
		return "i2i"
	default:
		if normalizePriceToken(strParamAny(params, "operation", "")) == "edit" {
			return "i2i"
		}
		return "t2i"
	}
}

func imageRuleMatchesRatio(rule ImagePriceRule, ratio, ratioGroup string) bool {
	for _, item := range rule.Ratios {
		if normalizeImageRatio(item) == ratio {
			return true
		}
	}
	group := normalizePriceToken(rule.RatioGroup)
	return group == "" || group == "all" || group == "*" || group == ratioGroup
}

func imagePriceRuleEnabled(rule ImagePriceRule) bool {
	return rule.Enabled == nil || *rule.Enabled
}

func imageRatioGroup(ratio string) string {
	for _, item := range defaultExtendedImageRatios {
		if normalizeImageRatio(item) == ratio {
			return "extended"
		}
	}
	return "standard"
}

func normalizeImageRatio(v string) string {
	v = strings.ReplaceAll(strings.TrimSpace(v), "：", ":")
	if v == "" {
		return "1:1"
	}
	return v
}

func normalizeImageResolution(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	switch v {
	case "", "1":
		return "1K"
	case "1K", "2K", "4K":
		return v
	default:
		return "1K"
	}
}

func normalizePriceToken(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func ConfigChatPriceFn(cfg *SystemConfigService) func(modelCode string) ChatPrice {
	return func(modelCode string) ChatPrice {
		def := DefaultChatPriceFn(modelCode)
		if cfg == nil {
			return def
		}
		raw := cfg.GetString(context.Background(), "billing.model_prices", "")
		if raw == "" {
			return def
		}
		var rows []struct {
			ModelCode        string `json:"model_code"`
			Kind             string `json:"kind"`
			UnitPoints       *int64 `json:"unit_points"`
			InputUnitPoints  *int64 `json:"input_unit_points"`
			OutputUnitPoints *int64 `json:"output_unit_points"`
			Enabled          *bool  `json:"enabled"`
		}
		if err := json.Unmarshal([]byte(raw), &rows); err == nil {
			for _, row := range rows {
				if row.ModelCode != modelCode {
					continue
				}
				if row.Enabled != nil && !*row.Enabled {
					continue
				}
				if row.InputUnitPoints != nil || row.OutputUnitPoints != nil {
					if row.InputUnitPoints != nil {
						def.InputPerK = *row.InputUnitPoints
					}
					if row.OutputUnitPoints != nil {
						def.OutputPerK = *row.OutputUnitPoints
					}
					return def
				}
				if row.Kind == "text" && row.UnitPoints != nil {
					return ChatPrice{InputPerK: *row.UnitPoints, OutputPerK: *row.UnitPoints}
				}
			}
		}
		return def
	}
}

func ChatCost(price ChatPrice, promptTokens, completionTokens int) int64 {
	if price.InputPerK <= 0 && price.OutputPerK <= 0 {
		return 0
	}
	in := (int64(promptTokens)*price.InputPerK + 999) / 1000
	out := (int64(completionTokens)*price.OutputPerK + 999) / 1000
	total := in + out
	if total <= 0 {
		return 1
	}
	return total
}

func ChatEstimatedCost(price ChatPrice, body map[string]any) int64 {
	if chatPriceUnitBasis(price) == ChatPriceBasisChars {
		promptChars := estimatePromptChars(body)
		maxTokens := intAny(body["max_tokens"], 1000)
		if maxTokens <= 0 {
			maxTokens = 1000
		}
		return ChatCost(price, promptChars, maxTokens*4)
	}
	promptTokens := estimatePromptTokens(body)
	maxTokens := intAny(body["max_tokens"], 1000)
	if maxTokens <= 0 {
		maxTokens = 1000
	}
	return ChatCost(price, promptTokens, maxTokens)
}

func ChatActualCost(price ChatPrice, body map[string]any, usage *ChatUsage, outputSnapshot map[string]any) (int64, bool) {
	if chatPriceUnitBasis(price) == ChatPriceBasisChars {
		promptChars := estimatePromptChars(body)
		completionChars := intAny(outputSnapshot["content_chars"], 0)
		if completionChars <= 0 && usage != nil && usage.CompletionTokens > 0 {
			completionChars = usage.CompletionTokens * 4
		}
		if promptChars <= 0 && completionChars <= 0 {
			return 0, false
		}
		return ChatCost(price, promptChars, completionChars), true
	}
	if usage == nil {
		return 0, false
	}
	return ChatCost(price, usage.PromptTokens, usage.CompletionTokens), true
}

func chatPriceUnitBasis(price ChatPrice) string {
	if strings.EqualFold(strings.TrimSpace(price.UnitBasis), ChatPriceBasisChars) {
		return ChatPriceBasisChars
	}
	return ChatPriceBasisTokens
}

func GenerationPricingAuditSnapshot(ctx context.Context, cfg *SystemConfigService, modelRepo *repo.ModelCatalogRepo, modelCode string, kind provider.Kind, params map[string]any, count int, estimatedTotal int64) map[string]any {
	if count <= 0 {
		count = 1
	}
	if estimatedTotal < 0 {
		estimatedTotal = 0
	}
	unit := int64(0)
	if count > 0 {
		unit = estimatedTotal / int64(count)
	}
	source, mode, item := generationPricingAuditSource(ctx, cfg, modelRepo, modelCode, kind, params)
	snapshot := map[string]any{
		"version":                1,
		"model_code":             modelCode,
		"kind":                   string(kind),
		"pricing_source":         source,
		"pricing_mode":           mode,
		"unit_basis":             "per_generation",
		"count":                  count,
		"estimated_unit_points":  unit,
		"estimated_total_points": estimatedTotal,
		"pre_deduct_points":      estimatedTotal,
		"actual_points":          estimatedTotal,
		"refund_points":          0,
		"extra_points":           0,
		"settlement":             "pre_deduct_fixed",
	}
	switch kind {
	case provider.KindImage:
		snapshot["request_mode"] = imageBillingMode(params)
		snapshot["ratio"] = normalizeImageRatio(strParamAny(params, "ratio", strParamAny(params, "aspect_ratio", "1:1")))
		snapshot["resolution"] = normalizeImageResolution(strParamAny(params, "resolution", strParamAny(params, "size_tier", "1K")))
		snapshot["quality"] = normalizePriceToken(strParamAny(params, "quality", ""))
		if item != nil && normalizePriceToken(item.PricingMode) == model.ModelCatalogPricingMatrix && item.PriceRules != nil {
			var rules []ImagePriceRule
			if err := json.Unmarshal([]byte(*item.PriceRules), &rules); err == nil {
				if rule, ok := imagePriceRuleMatch(rules, item.ModelCode, params); ok {
					snapshot["matched_rule"] = imagePriceRuleAudit(rule)
				}
			}
		}
	case provider.KindVideo:
		snapshot["request_mode"] = normalizePriceToken(strParamAny(params, "mode", "t2v"))
		snapshot["duration_sec"] = normalizeBillingVideoDuration(videoDurationParam(params))
		snapshot["quality"] = normalizePriceToken(strParamAny(params, "quality", ""))
		snapshot["resolution"] = normalizeImageResolution(strParamAny(params, "resolution", strParamAny(params, "size_tier", "")))
		if item != nil && normalizePriceToken(item.PricingMode) == model.ModelCatalogPricingMatrix && item.PriceRules != nil {
			var rules []VideoPriceRule
			if err := json.Unmarshal([]byte(*item.PriceRules), &rules); err == nil {
				if rule, ok := videoPriceRuleMatch(rules, item.ModelCode, params); ok {
					snapshot["matched_rule"] = videoPriceRuleAudit(rule)
				}
			}
		}
	}
	return snapshot
}

func ChatPricingAuditSnapshot(ctx context.Context, cfg *SystemConfigService, modelRepo *repo.ModelCatalogRepo, price ChatPrice, modelCode string, body map[string]any, estimatedPoints int64) map[string]any {
	if estimatedPoints < 0 {
		estimatedPoints = 0
	}
	source, mode := chatPricingAuditSource(ctx, cfg, modelRepo, modelCode)
	basis := chatPriceUnitBasis(price)
	snapshot := map[string]any{
		"version":            1,
		"model_code":         modelCode,
		"kind":               string(provider.KindChat),
		"pricing_source":     source,
		"pricing_mode":       mode,
		"unit_basis":         basis,
		"input_unit_points":  price.InputPerK,
		"output_unit_points": price.OutputPerK,
		"estimated_points":   estimatedPoints,
		"pre_deduct_points":  estimatedPoints,
		"actual_points":      estimatedPoints,
		"refund_points":      0,
		"extra_points":       0,
		"settlement":         "estimated_until_usage",
	}
	if basis == ChatPriceBasisChars {
		promptChars := estimatePromptChars(body)
		completionChars := intAny(body["max_tokens"], 1000)
		if completionChars <= 0 {
			completionChars = 1000
		}
		snapshot["estimated_prompt_chars"] = promptChars
		snapshot["estimated_completion_chars"] = completionChars * 4
	} else {
		promptTokens := estimatePromptTokens(body)
		completionTokens := intAny(body["max_tokens"], 1000)
		if completionTokens <= 0 {
			completionTokens = 1000
		}
		snapshot["estimated_prompt_tokens"] = promptTokens
		snapshot["estimated_completion_tokens"] = completionTokens
	}
	return snapshot
}

func ChatPricingResultPatch(usage *ChatUsage, estimatedPoints, actualPoints int64, outputSnapshots ...map[string]any) map[string]any {
	patch := pricingSettlementPatch(estimatedPoints, actualPoints)
	if usage != nil {
		patch["usage"] = map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		}
	} else {
		patch["usage_missing"] = true
	}
	if len(outputSnapshots) > 0 && outputSnapshots[0] != nil {
		if chars := intAny(outputSnapshots[0]["content_chars"], 0); chars > 0 {
			patch["completion_chars"] = chars
		}
	}
	return patch
}

func PricingFailureRefundPatch(estimatedPoints int64, reason string) map[string]any {
	if estimatedPoints < 0 {
		estimatedPoints = 0
	}
	return map[string]any{
		"actual_points":  0,
		"refund_points":  estimatedPoints,
		"extra_points":   0,
		"settlement":     "failed_refunded",
		"failure_reason": trimPricingAuditReason(reason),
	}
}

func pricingSettlementPatch(estimatedPoints, actualPoints int64) map[string]any {
	if estimatedPoints < 0 {
		estimatedPoints = 0
	}
	if actualPoints < 0 {
		actualPoints = 0
	}
	refund := int64(0)
	extra := int64(0)
	settlement := "settled"
	if actualPoints < estimatedPoints {
		refund = estimatedPoints - actualPoints
		settlement = "partial_refund"
	} else if actualPoints > estimatedPoints {
		extra = actualPoints - estimatedPoints
		settlement = "extra_charged"
	}
	if actualPoints == 0 && estimatedPoints > 0 {
		settlement = "refunded"
	}
	return map[string]any{
		"actual_points": actualPoints,
		"refund_points": refund,
		"extra_points":  extra,
		"settlement":    settlement,
	}
}

func generationPricingAuditSource(ctx context.Context, cfg *SystemConfigService, modelRepo *repo.ModelCatalogRepo, modelCode string, kind provider.Kind, params map[string]any) (string, string, *model.ModelCatalog) {
	if modelRepo != nil {
		if item, err := modelRepo.GetByCode(ctx, modelCode); err == nil && item != nil {
			if _, ok := catalogPrice(item, kind, params); ok {
				return "model_catalog", strings.TrimSpace(item.PricingMode), item
			}
		}
	}
	source, mode := legacyGenerationPricingAuditSource(ctx, cfg, modelCode, kind)
	return source, mode, nil
}

func chatPricingAuditSource(ctx context.Context, cfg *SystemConfigService, modelRepo *repo.ModelCatalogRepo, modelCode string) (string, string) {
	if modelRepo != nil {
		if item, err := modelRepo.GetByCode(ctx, modelCode); err == nil && item != nil {
			if _, ok := catalogChatPrice(item); ok {
				return "model_catalog", strings.TrimSpace(item.PricingMode)
			}
		}
	}
	if cfg != nil && billingModelPricesContains(ctx, cfg, modelCode) {
		return "system_config", "token"
	}
	return "default", "token"
}

func legacyGenerationPricingAuditSource(ctx context.Context, cfg *SystemConfigService, modelCode string, kind provider.Kind) (string, string) {
	if cfg != nil {
		if kind == provider.KindImage && strings.TrimSpace(cfg.GetString(ctx, SettingBillingImagePriceRules, "")) != "" {
			return "system_config", "matrix"
		}
		if billingModelPricesContains(ctx, cfg, modelCode) {
			if kind == provider.KindVideo {
				return "system_config", "duration_scaled"
			}
			return "system_config", "fixed"
		}
	}
	if kind == provider.KindImage {
		return "default", "matrix"
	}
	if kind == provider.KindVideo {
		return "default", "duration_scaled"
	}
	return "default", "fixed"
}

func billingModelPricesContains(ctx context.Context, cfg *SystemConfigService, modelCode string) bool {
	if cfg == nil {
		return false
	}
	raw := cfg.GetString(ctx, "billing.model_prices", "")
	if strings.TrimSpace(raw) == "" {
		return false
	}
	var rows []struct {
		ModelCode string `json:"model_code"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err == nil {
		for _, row := range rows {
			if row.ModelCode == modelCode {
				return true
			}
		}
	}
	var prices map[string]int64
	if err := json.Unmarshal([]byte(raw), &prices); err == nil {
		_, ok := prices[modelCode]
		return ok
	}
	return false
}

func imagePriceRuleAudit(rule ImagePriceRule) map[string]any {
	return map[string]any{
		"model_code":  rule.ModelCode,
		"mode":        rule.Mode,
		"ratio_group": rule.RatioGroup,
		"ratios":      rule.Ratios,
		"resolution":  rule.Resolution,
		"quality":     rule.Quality,
		"unit_points": rule.UnitPoints,
		"enabled":     imagePriceRuleEnabled(rule),
	}
}

func videoPriceRuleAudit(rule VideoPriceRule) map[string]any {
	return map[string]any{
		"model_code":   rule.ModelCode,
		"mode":         rule.Mode,
		"duration_sec": rule.DurationSec,
		"quality":      rule.Quality,
		"resolution":   rule.Resolution,
		"unit_points":  rule.UnitPoints,
		"enabled":      rule.Enabled == nil || *rule.Enabled,
	}
}

func trimPricingAuditReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) <= 180 {
		return reason
	}
	return reason[:180]
}

func videoPriceFromRules(rules []VideoPriceRule, modelCode string, params map[string]any) (int64, bool) {
	rule, ok := videoPriceRuleMatch(rules, modelCode, params)
	if !ok {
		return 0, false
	}
	return rule.UnitPoints, true
}

func videoPriceRuleMatch(rules []VideoPriceRule, modelCode string, params map[string]any) (VideoPriceRule, bool) {
	modelCode = normalizePriceToken(modelCode)
	if modelCode == "" || len(rules) == 0 {
		return VideoPriceRule{}, false
	}
	mode := normalizePriceToken(strParamAny(params, "mode", "t2v"))
	quality := normalizePriceToken(strParamAny(params, "quality", ""))
	resolution := normalizeImageResolution(strParamAny(params, "resolution", strParamAny(params, "size_tier", "")))
	duration := normalizeBillingVideoDuration(videoDurationParam(params))
	var fallback *VideoPriceRule
	for i := range rules {
		rule := &rules[i]
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		if normalizePriceToken(rule.ModelCode) != "" && normalizePriceToken(rule.ModelCode) != modelCode {
			continue
		}
		if !blankOrMatches(rule.Mode, mode) || !blankOrMatches(rule.Quality, quality) {
			continue
		}
		if strings.TrimSpace(rule.Resolution) != "" && normalizeImageResolution(rule.Resolution) != resolution {
			continue
		}
		if rule.UnitPoints <= 0 {
			continue
		}
		if rule.DurationSec <= 0 {
			if fallback == nil {
				fallback = rule
			}
			continue
		}
		if normalizeBillingVideoDuration(rule.DurationSec) == duration {
			return *rule, true
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return VideoPriceRule{}, false
}

func scaleVideoUnitPrice(unitPoints int64, params map[string]any) int64 {
	duration := videoDurationParam(params)
	if duration <= 0 {
		return unitPoints
	}
	duration = normalizeBillingVideoDuration(duration)
	if duration <= 6 {
		return unitPoints
	}
	return int64(float64(unitPoints) * (float64(duration) / 6))
}

func videoDurationParam(params map[string]any) int {
	if params == nil {
		return 0
	}
	for _, key := range []string{"duration", "duration_sec", "duration_seconds"} {
		switch v := params[key].(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case float32:
			return int(v)
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n
			}
		}
	}
	return 0
}

func blankOrMatches(ruleValue, actual string) bool {
	rule := normalizePriceToken(ruleValue)
	return rule == "" || rule == "all" || rule == "*" || rule == actual
}

func normalizeBillingVideoDuration(sec int) int {
	for _, v := range []int{6, 10} {
		if sec <= v {
			return v
		}
	}
	return 10
}
