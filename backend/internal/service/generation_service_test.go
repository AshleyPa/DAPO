package service

import (
	"errors"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
)

func TestProviderCooldownGrokForbiddenIsTransient(t *testing.T) {
	err := errors.New(`grok upload HTTP 403: <!DOCTYPE html><html><head><title>Just a moment...</title></head></html>`)
	if got := providerCooldown(err); got != 0 {
		t.Fatalf("expected transient cooldown 0, got %s", got)
	}
}

func TestProviderCooldownRetryable429StillCooldowns(t *testing.T) {
	err := errors.New(`provider call: grok video HTTP 429: {"error":{"code":8,"message":"Too many requests"}}`)
	got := providerCooldown(err)
	if got < 30*time.Minute {
		t.Fatalf("expected 429 cooldown >= 30m, got %s", got)
	}
}

func TestRetryableProviderErrorCoversImageChannelFailover(t *testing.T) {
	cases := []error{
		errors.New(`provider call: gpt image2 images api 403: {"error":{"message":"Image generation is not enabled for this account"}}`),
		errors.New(`provider call: gpt image2 images api 502: <!DOCTYPE html><html class="no-js">`),
		errors.New(`provider call: gpt image2 nova 404: 404 page not found`),
		errors.New(`provider call: gpt image2 images api 400: {"error":{"message":"Unsupported size"}}`),
	}
	for _, err := range cases {
		if !retryableProviderError(err) {
			t.Fatalf("expected %q to be retryable", err.Error())
		}
	}
}

func TestRetryableProviderErrorDoesNotRetryBrokenReferenceImage(t *testing.T) {
	err := errors.New(`provider call: gpt image2 images edits reference 1: reference image download 404: not found`)
	if retryableProviderError(err) {
		t.Fatalf("reference image download failure should not be retried across providers")
	}
}

func TestValidateProviderGenerationResultRejectsEmptyImageAssets(t *testing.T) {
	err := validateProviderGenerationResult(&model.GenerationTask{Kind: string(provider.KindImage)}, &provider.Result{})
	if err == nil {
		t.Fatalf("expected empty image assets to be rejected")
	}
}

func TestValidateProviderGenerationResultRejectsBlankAssetURL(t *testing.T) {
	err := validateProviderGenerationResult(&model.GenerationTask{Kind: string(provider.KindVideo)}, &provider.Result{
		Assets: []provider.Asset{{URL: " "}},
	})
	if err == nil {
		t.Fatalf("expected blank video asset URL to be rejected")
	}
}

func TestValidateProviderGenerationResultAllowsChatWithoutAssets(t *testing.T) {
	err := validateProviderGenerationResult(&model.GenerationTask{Kind: string(provider.KindChat)}, &provider.Result{})
	if err != nil {
		t.Fatalf("chat result should not require generation assets: %v", err)
	}
}

func TestAccountPoolRouteLogMetaCarriesRouteIdentity(t *testing.T) {
	meta := accountPoolRouteLogMeta(ProviderRoute{
		Provider:      model.ProviderGPT,
		UpstreamModel: "gpt-image-2",
		Strategy:      "weighted_rr",
		AuthType:      model.AuthTypeAPIKey,
		ImageAPIMode:  "nova_async",
		RouteIndex:    2,
		Attempt:       3,
	})

	if meta["model_gateway_source_type"] != model.ModelSourceTypeAccountPool {
		t.Fatalf("source type = %#v", meta["model_gateway_source_type"])
	}
	if meta["model_gateway_source_code"] != model.ProviderGPT {
		t.Fatalf("source code = %#v", meta["model_gateway_source_code"])
	}
	if meta["upstream_model"] != "gpt-image-2" {
		t.Fatalf("upstream_model = %#v", meta["upstream_model"])
	}
	if meta["image_api_mode"] != "nova_async" {
		t.Fatalf("image_api_mode = %#v", meta["image_api_mode"])
	}
	if meta["model_gateway_route_index"] != 2 || meta["model_gateway_attempt"] != 3 {
		t.Fatalf("attempt meta = %#v", meta)
	}
}

func TestAPIChannelRouteLogMetaCarriesRouteIdentity(t *testing.T) {
	meta := apiChannelRouteLogMeta(ProviderRoute{
		SourceCode:    "mimo-official",
		Adapter:       model.APIChannelAdapterOpenAIChat,
		UpstreamModel: "mimo-v2.5-pro",
		Strategy:      "weighted_rr",
		AuthType:      model.AuthTypeAPIKey,
		RouteIndex:    1,
		Attempt:       2,
	}, &model.APIChannel{
		ID:           42,
		Code:         "mimo-official",
		Name:         "MiMo API",
		ProviderName: "mimo",
	}, &APIChannelCredentialRef{
		Source:  apiChannelCredentialSourceKeyPool,
		KeyID:   7,
		KeyName: "primary",
	})

	if meta["model_gateway_source_type"] != model.ModelSourceTypeAPIChannel {
		t.Fatalf("source type = %#v", meta["model_gateway_source_type"])
	}
	if meta["model_gateway_source_code"] != "mimo-official" {
		t.Fatalf("source code = %#v", meta["model_gateway_source_code"])
	}
	if meta["upstream_model"] != "mimo-v2.5-pro" {
		t.Fatalf("upstream_model = %#v", meta["upstream_model"])
	}
	if meta["strategy"] != "weighted_rr" {
		t.Fatalf("strategy = %#v", meta["strategy"])
	}
	if meta["auth_type"] != model.AuthTypeAPIKey {
		t.Fatalf("auth_type = %#v", meta["auth_type"])
	}
	if meta["api_channel_credential_source"] != apiChannelCredentialSourceKeyPool || meta["api_channel_key_id"] != uint64(7) {
		t.Fatalf("credential meta = %#v", meta)
	}
	if meta["model_gateway_route_index"] != 1 || meta["model_gateway_attempt"] != 2 {
		t.Fatalf("attempt meta = %#v", meta)
	}
}

func TestSelectedProviderRouteAttemptParamsRefreshesRouteSnapshot(t *testing.T) {
	routes := []ProviderRoute{
		{
			SourceType:    model.ModelSourceTypeAPIChannel,
			SourceCode:    "primary-image",
			Provider:      generationProviderAPIChannel,
			Adapter:       model.APIChannelAdapterOpenAIImages,
			UpstreamModel: "gpt-image-2",
			Strategy:      "weighted_rr",
			Priority:      10,
			Weight:        100,
		},
		{
			SourceType:    model.ModelSourceTypeAccountPool,
			SourceCode:    model.ProviderGPT,
			Provider:      model.ProviderGPT,
			UpstreamModel: "gpt-image-2",
			Strategy:      "round_robin",
			Priority:      20,
			Weight:        1,
		},
	}
	params := map[string]any{
		routeParamCandidates: routes,
		routeParamSnapshot:   providerRouteSnapshotPayload("gpt-image-2", string(provider.KindImage), routes, 1),
	}

	got := selectedProviderRouteAttemptParams(params, routes[1], 2)
	if got[routeParamSourceType] != model.ModelSourceTypeAccountPool {
		t.Fatalf("source type = %#v", got[routeParamSourceType])
	}
	if got[routeParamSourceCode] != model.ProviderGPT {
		t.Fatalf("source code = %#v", got[routeParamSourceCode])
	}
	snapshot, ok := got[routeParamSnapshot].(map[string]any)
	if !ok {
		t.Fatalf("snapshot = %#v", got[routeParamSnapshot])
	}
	if snapshot["selected_index"] != 2 {
		t.Fatalf("selected_index = %#v, want 2", snapshot["selected_index"])
	}
	if snapshot["candidate_count"] != 2 {
		t.Fatalf("candidate_count = %#v, want 2", snapshot["candidate_count"])
	}
	candidates, ok := snapshot["candidates"].([]map[string]any)
	if !ok || len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want 2", snapshot["candidates"])
	}
	if candidates[1]["source_code"] != model.ProviderGPT {
		t.Fatalf("selected candidate source_code = %#v", candidates[1]["source_code"])
	}
	if candidates[1]["index"] != 2 {
		t.Fatalf("selected candidate index = %#v, want 2", candidates[1]["index"])
	}
}

func TestMergeUpstreamLogMetaKeepsProviderOverrideVisible(t *testing.T) {
	meta := mergeUpstreamLogMeta([]map[string]any{{
		"model_gateway_source_type": model.ModelSourceTypeAccountPool,
		"model_gateway_source_code": model.ProviderGPT,
		"upstream_model":            "gpt-image-2",
	}}, map[string]any{
		"upstream_model": "other-model",
		"poll":           2,
	})

	if meta["model_gateway_source_code"] != model.ProviderGPT {
		t.Fatalf("source code = %#v", meta["model_gateway_source_code"])
	}
	if meta["upstream_model"] != "other-model" {
		t.Fatalf("provider upstream_model override should remain visible, got %#v", meta["upstream_model"])
	}
	if meta["poll"] != 2 {
		t.Fatalf("poll = %#v", meta["poll"])
	}
}
