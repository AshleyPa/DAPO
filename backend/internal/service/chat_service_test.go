package service

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kleinai/backend/internal/model"
)

func TestForwardChatStreamParsesUsageAndForwardsSSE(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":5,\"total_tokens\":8}}\n\ndata: [DONE]\n\n")
	rec := httptest.NewRecorder()

	usage, output, err := forwardChatStream(body, rec)
	if err != nil {
		t.Fatalf("forwardChatStream returned error: %v", err)
	}
	if usage == nil || usage.PromptTokens != 3 || usage.CompletionTokens != 5 || usage.TotalTokens != 8 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := rec.Body.String(); !strings.Contains(got, "data: [DONE]") {
		t.Fatalf("forwarded body missing done marker: %q", got)
	}
	if output["output_present"] != true || output["completion_tokens"] != 5 {
		t.Fatalf("unexpected output snapshot: %#v", output)
	}
}

func TestChatOutputSnapshotFromRawDoesNotStoreContent(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"role":"assistant","content":"hello world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	snapshot := chatOutputSnapshotFromRaw(false, raw, nil)
	if snapshot["output_present"] != true {
		t.Fatalf("expected output snapshot to prove output, got %#v", snapshot)
	}
	if snapshot["content_chars"] != 11 {
		t.Fatalf("content_chars = %#v, want 11", snapshot["content_chars"])
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "hello") {
		t.Fatalf("snapshot leaked content: %#v", snapshot)
	}
	if !chatOutputSnapshotProvesOutput(snapshot) {
		t.Fatalf("expected snapshot to prove output: %#v", snapshot)
	}
}

func TestChatOutputSnapshotRejectsEmptySuccessPayload(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`)
	snapshot := chatOutputSnapshotFromRaw(false, raw, nil)
	if snapshot["output_present"] != false {
		t.Fatalf("expected empty payload to have no output proof, got %#v", snapshot)
	}
	if chatOutputSnapshotProvesOutput(snapshot) {
		t.Fatalf("empty payload should not prove output: %#v", snapshot)
	}
}

func TestChatAcceptHeaderUsesSSEForStream(t *testing.T) {
	if got := chatAcceptHeader(map[string]any{"stream": true}); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("stream Accept = %q, want text/event-stream", got)
	}
	if got := chatAcceptHeader(map[string]any{"stream": false}); got != "application/json" {
		t.Fatalf("non-stream Accept = %q, want application/json", got)
	}
}

func TestChatAPIChannelMetaCarriesRouteAttempt(t *testing.T) {
	meta := chatAPIChannelMeta(chatRuntimeRoute{
		SourceType:    model.ModelSourceTypeAPIChannel,
		SourceCode:    "mimo-official",
		SourceName:    "MiMo API",
		Adapter:       model.APIChannelAdapterOpenAIChat,
		UpstreamModel: "mimo-v2.5-pro",
		Strategy:      "weighted_rr",
		AuthType:      model.AuthTypeAPIKey,
		RouteIndex:    2,
		Attempt:       2,
		APIChannel: &model.APIChannel{
			ID:           42,
			Code:         "mimo-official",
			Name:         "MiMo API",
			ProviderName: "mimo",
		},
	})

	if meta["model_gateway_source_code"] != "mimo-official" {
		t.Fatalf("source code = %#v", meta["model_gateway_source_code"])
	}
	if meta["upstream_model"] != "mimo-v2.5-pro" {
		t.Fatalf("upstream_model = %#v", meta["upstream_model"])
	}
	if meta["model_gateway_route_index"] != 2 || meta["model_gateway_attempt"] != 2 {
		t.Fatalf("attempt meta = %#v", meta)
	}
}

func TestChatRuntimeRouteSnapshotIncludesSkippedCandidates(t *testing.T) {
	payload := chatRuntimeRouteSnapshotPayload("mimo-v2.5-pro", []chatRuntimeRoute{
		{
			SourceType:    model.ModelSourceTypeAPIChannel,
			SourceCode:    "mimo-official",
			SourceName:    "MiMo API",
			Provider:      chatProviderAPIChannel,
			Adapter:       model.APIChannelAdapterOpenAIChat,
			UpstreamModel: "mimo-v2.5-pro",
			Strategy:      "round_robin",
			Priority:      20,
			Weight:        80,
		},
	}, 1, []chatRuntimeRoute{
		{
			SourceType:    model.ModelSourceTypeAccountPool,
			SourceCode:    model.ProviderGPT,
			Provider:      model.ProviderGPT,
			UpstreamModel: "mimo-v2.5-pro",
			SkipReason:    "MiMo 官方 API 模型不能挂到 GPT 账号池",
			Priority:      30,
			Weight:        10,
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
	if got := skipped[0]["skip_reason"]; got != "MiMo 官方 API 模型不能挂到 GPT 账号池" {
		t.Fatalf("skip_reason = %#v", got)
	}
	candidates, ok := payload["candidates"].([]map[string]any)
	if !ok || len(candidates) != 1 {
		t.Fatalf("candidates = %#v, want one candidate", payload["candidates"])
	}
	if candidates[0]["priority"] != 20 || candidates[0]["weight"] != 80 {
		t.Fatalf("candidate priority/weight missing: %#v", candidates[0])
	}
	if candidates[0]["index"] != 1 {
		t.Fatalf("candidate index = %#v, want 1", candidates[0]["index"])
	}
	if skipped[0]["priority"] != 30 || skipped[0]["weight"] != 10 {
		t.Fatalf("skipped priority/weight missing: %#v", skipped[0])
	}
	if skipped[0]["index"] != 1 {
		t.Fatalf("skipped index = %#v, want 1", skipped[0]["index"])
	}
}

func TestChatAPIChannelMetaIncludesRouteStrategyAndAuthType(t *testing.T) {
	meta := chatAPIChannelMeta(chatRuntimeRoute{
		SourceType:    model.ModelSourceTypeAPIChannel,
		SourceCode:    "mimo-official",
		SourceName:    "MiMo API",
		Provider:      chatProviderAPIChannel,
		Adapter:       model.APIChannelAdapterOpenAIChat,
		UpstreamModel: "mimo-v2.5-pro",
		Strategy:      "weighted_rr",
		AuthType:      model.AuthTypeAPIKey,
		APIChannel: &model.APIChannel{
			ID:           42,
			Code:         "mimo-official",
			Name:         "MiMo API",
			ProviderName: "mimo",
		},
	})

	if meta["model_gateway_source_type"] != model.ModelSourceTypeAPIChannel {
		t.Fatalf("source type = %#v", meta["model_gateway_source_type"])
	}
	if meta["model_gateway_source_code"] != "mimo-official" {
		t.Fatalf("source code = %#v", meta["model_gateway_source_code"])
	}
	if meta["strategy"] != "weighted_rr" {
		t.Fatalf("strategy = %#v", meta["strategy"])
	}
	if meta["auth_type"] != model.AuthTypeAPIKey {
		t.Fatalf("auth_type = %#v", meta["auth_type"])
	}
	if meta["api_channel_code"] != "mimo-official" {
		t.Fatalf("api_channel_code = %#v", meta["api_channel_code"])
	}
}

func TestOrderChatRuntimeRoutesForRuntimeWeightedWithinPriority(t *testing.T) {
	picker := newModelSourceRoutePicker()
	routes := []chatRuntimeRoute{
		{SourceType: model.ModelSourceTypeAPIChannel, SourceCode: "primary", Provider: chatProviderAPIChannel, UpstreamModel: "mimo-v2.5-pro", Strategy: "weighted_rr", Priority: 10, Weight: 2},
		{SourceType: model.ModelSourceTypeAPIChannel, SourceCode: "backup", Provider: chatProviderAPIChannel, UpstreamModel: "mimo-v2.5-pro", Strategy: "weighted_rr", Priority: 10, Weight: 1},
		{SourceType: model.ModelSourceTypeAccountPool, SourceCode: "gpt", Provider: model.ProviderGPT, UpstreamModel: "gpt-4o-mini", Strategy: "weighted_rr", Priority: 20, Weight: 100},
	}
	first := orderChatRuntimeRoutesForRuntimeWithPicker(picker, "mimo-v2.5-pro", "chat", routes)
	second := orderChatRuntimeRoutesForRuntimeWithPicker(picker, "mimo-v2.5-pro", "chat", routes)
	third := orderChatRuntimeRoutesForRuntimeWithPicker(picker, "mimo-v2.5-pro", "chat", routes)
	if first[0].SourceCode != "primary" || second[0].SourceCode != "backup" || third[0].SourceCode != "primary" {
		t.Fatalf("weighted chat order = %s, %s, %s", first[0].SourceCode, second[0].SourceCode, third[0].SourceCode)
	}
	if third[2].SourceCode != "gpt" {
		t.Fatalf("lower-priority fallback moved ahead: %#v", third)
	}
}
