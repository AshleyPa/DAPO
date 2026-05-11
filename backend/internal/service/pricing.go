// Package service 模型计费表（开发期内置；后续从 model 表读取并缓存）。
package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kleinai/backend/internal/provider"
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

// ImagePriceRule describes per-image pricing by business-facing image options.
// UnitPoints is stored in points*100. Example: 400 = 4 points per image.
type ImagePriceRule struct {
	ModelCode  string   `json:"model_code"`
	Mode       string   `json:"mode"` // t2i / i2i
	RatioGroup string   `json:"ratio_group,omitempty"`
	Ratios     []string `json:"ratios,omitempty"`
	Resolution string   `json:"resolution"`
	UnitPoints int64    `json:"unit_points"`
	Enabled    *bool    `json:"enabled,omitempty"`
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

// ChatPrice is points*100 per 1K tokens.
type ChatPrice struct {
	InputPerK  int64
	OutputPerK int64
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
	modelCode = normalizePriceToken(modelCode)
	if modelCode == "" || len(rules) == 0 {
		return 0, false
	}
	mode := imageBillingMode(params)
	ratio := normalizeImageRatio(strParamAny(params, "ratio", strParamAny(params, "aspect_ratio", "1:1")))
	resolution := normalizeImageResolution(strParamAny(params, "resolution", strParamAny(params, "size_tier", "1K")))
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
		if imageRuleMatchesRatio(rule, ratio, ratioGroup) && rule.UnitPoints > 0 {
			return rule.UnitPoints, true
		}
	}
	return 0, false
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

func normalizeBillingVideoDuration(sec int) int {
	for _, v := range []int{6, 10} {
		if sec <= v {
			return v
		}
	}
	return 10
}
