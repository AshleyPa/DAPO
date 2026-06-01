package service

import (
	"strings"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/pkg/crypto"
)

func TestAPIChannelProbeEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		adapter     string
		baseURL     string
		wantSuffix  string
		wantPayload string
	}{
		{name: "chat", adapter: model.APIChannelAdapterOpenAIChat, baseURL: "https://api.example.test/v1", wantSuffix: "/v1/chat/completions", wantPayload: "{}"},
		{name: "images", adapter: model.APIChannelAdapterOpenAIImages, baseURL: "https://api.example.test/v1", wantSuffix: "/v1/images/generations", wantPayload: "{}"},
		{name: "video", adapter: model.APIChannelAdapterOpenAIVideo, baseURL: "https://api.example.test/v1", wantSuffix: "/v1/video/generations", wantPayload: "{}"},
		{name: "responses", adapter: model.APIChannelAdapterOpenAIResponses, baseURL: "https://api.example.test/v1", wantSuffix: "/v1/responses", wantPayload: "{}"},
		{name: "full endpoint base", adapter: model.APIChannelAdapterOpenAIChat, baseURL: "https://api.example.test/v1/chat/completions", wantSuffix: "/v1/chat/completions", wantPayload: "{}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, payload := apiChannelProbeEndpoint(&model.APIChannel{Adapter: tc.adapter, BaseURL: tc.baseURL})
			if !strings.HasSuffix(got, tc.wantSuffix) {
				t.Fatalf("endpoint = %q, want suffix %q", got, tc.wantSuffix)
			}
			if payload != tc.wantPayload {
				t.Fatalf("payload = %q, want %q", payload, tc.wantPayload)
			}
		})
	}
}

func TestAPIChannelProbeTimeoutBounds(t *testing.T) {
	if got := apiChannelProbeTimeout(&model.APIChannel{TimeoutSeconds: 2}); got != 5*time.Second {
		t.Fatalf("low timeout = %s, want 5s", got)
	}
	if got := apiChannelProbeTimeout(&model.APIChannel{TimeoutSeconds: 120}); got != 60*time.Second {
		t.Fatalf("high timeout = %s, want 60s", got)
	}
	if got := apiChannelProbeTimeout(&model.APIChannel{TimeoutSeconds: 30}); got != 30*time.Second {
		t.Fatalf("normal timeout = %s, want 30s", got)
	}
}

func TestAPIChannelProbeModelsOKRequiresConfiguredModelWhenListed(t *testing.T) {
	models := `["mimo-v2.5-pro"]`
	ok, msg := apiChannelProbeModelsOK(&model.APIChannel{Models: &models}, []byte(`{"data":[{"id":"deepseek-chat"}]}`))
	if ok {
		t.Fatalf("expected configured model mismatch to fail, msg=%q", msg)
	}
	if !strings.Contains(msg, "mimo-v2.5-pro") || !strings.Contains(msg, "deepseek-chat") {
		t.Fatalf("expected mismatch summary to mention expected and listed models, got %q", msg)
	}
}

func TestAPIChannelProbeModelsOKAcceptsConfiguredModelWhenListed(t *testing.T) {
	models := `["mimo-v2.5-pro"]`
	ok, msg := apiChannelProbeModelsOK(&model.APIChannel{Models: &models}, []byte(`{"data":[{"id":"mimo-v2.5-pro"}]}`))
	if !ok || msg != "" {
		t.Fatalf("expected configured model to pass, ok=%v msg=%q", ok, msg)
	}
}

func TestAPIChannelProbeModelsOKDoesNotBlockUnparseableModelsResponse(t *testing.T) {
	models := `["mimo-v2.5-pro"]`
	ok, msg := apiChannelProbeModelsOK(&model.APIChannel{Models: &models}, []byte(`{"object":"list","data":[{"name":"opaque"}]}`))
	if !ok || msg != "" {
		t.Fatalf("expected unparseable model list to stay protocol-compatible, ok=%v msg=%q", ok, msg)
	}
}

func TestCredentialRefResponseFields(t *testing.T) {
	ref := &APIChannelCredentialRef{
		Source:  apiChannelCredentialSourceKeyPool,
		KeyID:   42,
		KeyName: "mimo-primary",
	}
	if got := credentialRefSource(ref); got != apiChannelCredentialSourceKeyPool {
		t.Fatalf("source = %q", got)
	}
	if got := credentialRefKeyID(ref); got != 42 {
		t.Fatalf("key id = %d", got)
	}
	if got := credentialRefKeyName(ref); got != "mimo-primary" {
		t.Fatalf("key name = %q", got)
	}
	if got := credentialRefSource(nil); got != "" {
		t.Fatalf("nil source = %q", got)
	}
}

func TestApplyAPIChannelCredentialUpdateClearLegacy(t *testing.T) {
	aes, err := crypto.NewAESGCM([]byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAPIChannelAdminService(nil, aes)
	clear := true
	fields := map[string]any{}
	dirty, err := svc.applyAPIChannelCredentialUpdate(&dto.APIChannelUpdateReq{ClearAPIKey: &clear}, fields)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Fatal("expected clear legacy key to mark credential dirty")
	}
	if value, ok := fields["credential_enc"]; !ok || value != nil {
		t.Fatalf("expected credential_enc to be explicitly cleared, got ok=%v value=%#v", ok, value)
	}
	resetAPIChannelHealthFields(fields)
	if fields["last_test_at"] != nil || fields["last_test_error"] != nil || fields["last_test_status"] != int8(0) {
		t.Fatalf("expected health fields to be reset, got %#v", fields)
	}
}

func TestApplyAPIChannelCredentialUpdateRejectsClearAndNewKey(t *testing.T) {
	aes, err := crypto.NewAESGCM([]byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAPIChannelAdminService(nil, aes)
	clear := true
	apiKey := "new-secret"
	if _, err := svc.applyAPIChannelCredentialUpdate(&dto.APIChannelUpdateReq{APIKey: &apiKey, ClearAPIKey: &clear}, map[string]any{}); err == nil {
		t.Fatal("expected clear legacy key and new key in the same request to fail")
	}
}

func TestApplyAPIChannelCredentialUpdateEncryptsNewLegacyKey(t *testing.T) {
	aes, err := crypto.NewAESGCM([]byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAPIChannelAdminService(nil, aes)
	apiKey := "new-secret"
	fields := map[string]any{}
	dirty, err := svc.applyAPIChannelCredentialUpdate(&dto.APIChannelUpdateReq{APIKey: &apiKey}, fields)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Fatal("expected new legacy key to mark credential dirty")
	}
	raw, ok := fields["credential_enc"].([]byte)
	if !ok || len(raw) == 0 {
		t.Fatalf("expected encrypted credential bytes, got %#v", fields["credential_enc"])
	}
	plain, err := aes.Decrypt(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != apiKey {
		t.Fatalf("decrypted key = %q, want %q", string(plain), apiKey)
	}
}

func TestNormalizeAPIChannelBaseURLRejectsCredentialBearingURLParts(t *testing.T) {
	for _, value := range []string{
		"https://user:pass@example.test/v1",
		"https://example.test/v1?api_key=secret",
		"https://example.test/v1#token",
	} {
		if got, err := normalizeAPIChannelBaseURL(value); err == nil {
			t.Fatalf("expected %q to be rejected, got %q", value, got)
		}
	}
}

func TestNormalizeAPIChannelBaseURLTrimsTrailingSlash(t *testing.T) {
	got, err := normalizeAPIChannelBaseURL("https://token-plan-cn.xiaomimimo.com/v1/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://token-plan-cn.xiaomimimo.com/v1" {
		t.Fatalf("normalized base URL = %q", got)
	}
}

func TestNormalizeAPIChannelModelListRejectsCredentialShapedEntries(t *testing.T) {
	for _, values := range [][]string{
		{"api_key=secret"},
		{"https://example.test/v1/models"},
		{"mimo-v2.5-pro?token=secret"},
		{"Bearer secret"},
	} {
		if got, err := normalizeAPIChannelModelList(values); err == nil {
			t.Fatalf("expected %v to be rejected, got %#v", values, got)
		}
	}
}

func TestNormalizeAPIChannelModelListAllowsCommonProviderModelCodes(t *testing.T) {
	got, err := normalizeAPIChannelModelList([]string{
		"mimo-v2.5-pro",
		"deepseek-chat",
		"openai/gpt-4o-mini",
		"ft:gpt-4.1-mini:org:custom:123",
		"mimo-v2.5-pro",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"deepseek-chat", "ft:gpt-4.1-mini:org:custom:123", "mimo-v2.5-pro", "openai/gpt-4o-mini"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("normalized model list = %#v, want %#v", got, want)
	}
}

func TestAPIChannelModelsChangedIgnoresOrderAndDuplicates(t *testing.T) {
	current := `["deepseek-chat","mimo-v2.5-pro"]`
	next, err := normalizeAPIChannelModelList([]string{"mimo-v2.5-pro", "deepseek-chat", "mimo-v2.5-pro"})
	if err != nil {
		t.Fatal(err)
	}
	if apiChannelModelsChanged(&current, next) {
		t.Fatal("expected equivalent normalized model lists to keep health state")
	}
}

func TestAPIChannelModelsChangedDetectsTargetModelChange(t *testing.T) {
	current := `["deepseek-chat"]`
	next, err := normalizeAPIChannelModelList([]string{"mimo-v2.5-pro"})
	if err != nil {
		t.Fatal(err)
	}
	if !apiChannelModelsChanged(&current, next) {
		t.Fatal("expected model list change to reset health state")
	}
}

func TestAPIChannelTimeoutChanged(t *testing.T) {
	ch := &model.APIChannel{TimeoutSeconds: 300}
	same := 300
	changed := 30
	if apiChannelTimeoutChanged(ch, &same) {
		t.Fatal("expected same timeout to keep health state")
	}
	if !apiChannelTimeoutChanged(ch, &changed) {
		t.Fatal("expected timeout change to reset health state")
	}
}

func TestAPIChannelStatusChanged(t *testing.T) {
	ch := &model.APIChannel{Status: model.APIChannelStatusEnabled}
	same := int8(model.APIChannelStatusEnabled)
	changed := int8(model.APIChannelStatusDisabled)
	if apiChannelStatusChanged(ch, &same) {
		t.Fatal("expected same status to keep health state")
	}
	if !apiChannelStatusChanged(ch, &changed) {
		t.Fatal("expected status change to reset health state")
	}
}
