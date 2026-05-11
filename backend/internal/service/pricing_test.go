package service

import (
	"testing"

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
