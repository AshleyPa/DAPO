package gpt

import "testing"

func TestImagesAPIAssetsExtractsMarkdownImageChoice(t *testing.T) {
	resp := imageGenerationTaskResp{
		Model: "gpt-image-2",
		Choices: []struct {
			Text    string `json:"text"`
			Message *struct {
				Content string `json:"content"`
				Role    string `json:"role"`
			} `json:"message"`
		}{
			{
				Message: &struct {
					Content string `json:"content"`
					Role    string `json:"role"`
				}{
					Content: "![image](https://example-cdn.test/output.png)",
					Role:    "assistant",
				},
			},
		},
	}

	assets, done, err := imagesAPIAssets(resp, "1024x1024")
	if err != nil {
		t.Fatalf("imagesAPIAssets returned error: %v", err)
	}
	if !done {
		t.Fatal("imagesAPIAssets should treat markdown image choices as completed")
	}
	if len(assets) != 1 {
		t.Fatalf("assets len = %d, want 1", len(assets))
	}
	if assets[0].URL != "https://example-cdn.test/output.png" {
		t.Fatalf("asset URL = %q", assets[0].URL)
	}
	if assets[0].Width != 1024 || assets[0].Height != 1024 {
		t.Fatalf("asset size = %dx%d, want 1024x1024", assets[0].Width, assets[0].Height)
	}
}

func TestImageSizeUsesPic2APIStandardDimensions(t *testing.T) {
	cases := []struct {
		name string
		p    map[string]any
		want string
	}{
		{name: "1k wide", p: map[string]any{"ratio": "16:9", "resolution": "1K"}, want: "1280x720"},
		{name: "2k square", p: map[string]any{"ratio": "1:1", "resolution": "2K"}, want: "2048x2048"},
		{name: "4k portrait", p: map[string]any{"ratio": "9:16", "resolution": "4K"}, want: "1872x3328"},
		{name: "explicit size wins", p: map[string]any{"size": "2560x1440", "ratio": "1:1", "resolution": "1K"}, want: "2560x1440"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := imageSize(tc.p, "1024x1024"); got != tc.want {
				t.Fatalf("imageSize() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestImageGenerationEndpointAvoidsDuplicateV1(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{base: "https://example.test", want: "https://example.test/v1/images/generations"},
		{base: "https://example.test/v1", want: "https://example.test/v1/images/generations"},
		{base: "https://example.test/v1/", want: "https://example.test/v1/images/generations"},
		{base: "https://example.test/v1/images/generations", want: "https://example.test/v1/images/generations"},
		{base: "https://example.test/v1/chat/completions", want: "https://example.test/v1/images/generations"},
		{base: "https://example.test/v1/models", want: "https://example.test/v1/images/generations"},
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			if got := imageGenerationEndpoint(tc.base); got != tc.want {
				t.Fatalf("imageGenerationEndpoint() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResponseEndpointAvoidsDuplicateV1(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{base: "https://example.test", want: "https://example.test/v1/responses"},
		{base: "https://example.test/v1", want: "https://example.test/v1/responses"},
		{base: "https://example.test/v1/", want: "https://example.test/v1/responses"},
		{base: "https://example.test/v1/responses", want: "https://example.test/v1/responses"},
		{base: "https://example.test/v1/chat/completions", want: "https://example.test/v1/responses"},
		{base: "https://example.test/v1/images/generations", want: "https://example.test/v1/responses"},
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			if got := responseEndpoint(tc.base); got != tc.want {
				t.Fatalf("responseEndpoint() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestImagesAPIQualityPrefersResolutionTierForFrontendHigh(t *testing.T) {
	cases := []struct {
		name string
		p    map[string]any
		want string
	}{
		{name: "frontend high 1k", p: map[string]any{"quality": "high", "resolution": "1K"}, want: "standard"},
		{name: "frontend high 2k", p: map[string]any{"quality": "high", "resolution": "2K"}, want: "hd"},
		{name: "frontend high 4k", p: map[string]any{"quality": "high", "resolution": "4K"}, want: "4k"},
		{name: "explicit hd", p: map[string]any{"quality": "hd", "resolution": "1K"}, want: "hd"},
		{name: "explicit ultra", p: map[string]any{"quality": "ultra", "resolution": "1K"}, want: "4k"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := imagesAPIQuality(tc.p); got != tc.want {
				t.Fatalf("imagesAPIQuality() = %q, want %q", got, tc.want)
			}
		})
	}
}
