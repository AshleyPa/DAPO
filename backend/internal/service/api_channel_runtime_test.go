package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
)

func TestAPIChannelLimiterRPM(t *testing.T) {
	limiter := newAPIChannelLimiter()
	if !limiter.allow("k1", 2, 0, 0) {
		t.Fatal("first request should pass")
	}
	if !limiter.allow("k1", 2, 0, 0) {
		t.Fatal("second request should pass")
	}
	if limiter.allow("k1", 2, 0, 0) {
		t.Fatal("third request should be rate-limited")
	}
}

func TestAPIChannelLimiterTPM(t *testing.T) {
	limiter := newAPIChannelLimiter()
	if !limiter.allow("k1", 0, 10, 6) {
		t.Fatal("first token bucket request should pass")
	}
	if limiter.allow("k1", 0, 10, 5) {
		t.Fatal("second token bucket request should be rate-limited")
	}
}

func TestAllowAPIChannelCredentialLimitUsesDistributedLimiter(t *testing.T) {
	limiter := &fakeAPIChannelDistributedLimiter{allowed: true}
	if !allowAPIChannelCredentialLimit(context.Background(), newAPIChannelLimiter(), limiter, "api_channel_key:123", 2, 10, 6) {
		t.Fatal("distributed limiter should allow request")
	}
	if got := len(limiter.calls); got != 2 {
		t.Fatalf("distributed limiter calls = %d, want 2", got)
	}
	if limiter.calls[0].key != "ratelimit:model_gateway:api_channel_key:123:tpm" || limiter.calls[0].rate != 10 || limiter.calls[0].n != 6 {
		t.Fatalf("tpm call = %#v", limiter.calls[0])
	}
	if limiter.calls[1].key != "ratelimit:model_gateway:api_channel_key:123:rpm" || limiter.calls[1].rate != 2 || limiter.calls[1].n != 1 {
		t.Fatalf("rpm call = %#v", limiter.calls[1])
	}
}

func TestAllowAPIChannelCredentialLimitDeniesDistributedLimiter(t *testing.T) {
	limiter := &fakeAPIChannelDistributedLimiter{allowed: false}
	if allowAPIChannelCredentialLimit(context.Background(), newAPIChannelLimiter(), limiter, "api_channel_key:124", 1, 0, 0) {
		t.Fatal("distributed limiter denial should reject request")
	}
	if got := len(limiter.calls); got != 1 {
		t.Fatalf("distributed limiter calls = %d, want 1", got)
	}
}

func TestAllowAPIChannelCredentialLimitFallsBackWhenDistributedErrors(t *testing.T) {
	local := newAPIChannelLimiter()
	limiter := &fakeAPIChannelDistributedLimiter{err: errors.New("redis unavailable")}
	if !allowAPIChannelCredentialLimit(context.Background(), local, limiter, "api_channel_key:125", 1, 0, 0) {
		t.Fatal("first request should pass through local fallback")
	}
	if allowAPIChannelCredentialLimit(context.Background(), local, limiter, "api_channel_key:125", 1, 0, 0) {
		t.Fatal("second request should be limited by local fallback")
	}
	if got := len(limiter.calls); got != 2 {
		t.Fatalf("distributed limiter calls = %d, want 2", got)
	}
}

func TestAPIChannelKeyPickerWeightedRoundRobin(t *testing.T) {
	picker := newAPIChannelKeyPicker()
	keys := []*model.APIChannelKey{
		{ID: 1, Weight: 2},
		{ID: 2, Weight: 1},
	}
	got := []uint64{
		picker.order(88, 1, keys)[0].ID,
		picker.order(88, 1, keys)[0].ID,
		picker.order(88, 1, keys)[0].ID,
	}
	want := []uint64{1, 2, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("weighted order = %#v, want %#v", got, want)
		}
	}
}

func TestSelectAPIChannelCredentialUsesPriorityThenWeight(t *testing.T) {
	aes := newTestAES(t)
	lowPriorityToken := encryptTestToken(t, aes, "low-priority")
	highPriorityToken := encryptTestToken(t, aes, "high-priority")
	lister := fakeAPIChannelKeyLister{keys: []*model.APIChannelKey{
		{ID: 10, ChannelID: 9001, CredentialEnc: lowPriorityToken, Priority: 5, Weight: 100, Status: model.APIChannelKeyStatusEnabled},
		{ID: 11, ChannelID: 9001, CredentialEnc: highPriorityToken, Priority: 1, Weight: 1, Status: model.APIChannelKeyStatusEnabled},
	}}
	ref, err := selectAPIChannelCredential(context.Background(), lister, aes, &model.APIChannel{ID: 9001, CredentialEnc: encryptTestToken(t, aes, "legacy")}, 0)
	if err != nil {
		t.Fatalf("selectAPIChannelCredential() error = %v", err)
	}
	if ref.Token != "high-priority" || ref.KeyID != 11 {
		t.Fatalf("selected token/key = %q/%d", ref.Token, ref.KeyID)
	}
}

func TestSelectAPIChannelCredentialRotatesSamePriorityByWeight(t *testing.T) {
	aes := newTestAES(t)
	lister := fakeAPIChannelKeyLister{keys: []*model.APIChannelKey{
		{ID: 21, ChannelID: 9002, CredentialEnc: encryptTestToken(t, aes, "k1"), Priority: 1, Weight: 2, Status: model.APIChannelKeyStatusEnabled},
		{ID: 22, ChannelID: 9002, CredentialEnc: encryptTestToken(t, aes, "k2"), Priority: 1, Weight: 1, Status: model.APIChannelKeyStatusEnabled},
	}}
	ch := &model.APIChannel{ID: 9002, CredentialEnc: encryptTestToken(t, aes, "legacy")}
	got := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		ref, err := selectAPIChannelCredential(context.Background(), lister, aes, ch, 0)
		if err != nil {
			t.Fatalf("select #%d error = %v", i+1, err)
		}
		got = append(got, ref.Token)
	}
	want := []string{"k1", "k2", "k1"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selected tokens = %#v, want %#v", got, want)
		}
	}
}

func TestSelectAPIChannelCredentialWithLimiterSkipsRateLimitedKey(t *testing.T) {
	aes := newTestAES(t)
	lister := fakeAPIChannelKeyLister{keys: []*model.APIChannelKey{
		{ID: 31, ChannelID: 9003, CredentialEnc: encryptTestToken(t, aes, "limited"), Priority: 1, Weight: 1, RPMLimit: 1, Status: model.APIChannelKeyStatusEnabled},
		{ID: 32, ChannelID: 9003, CredentialEnc: encryptTestToken(t, aes, "available"), Priority: 1, Weight: 1, RPMLimit: 1, Status: model.APIChannelKeyStatusEnabled},
	}}
	limiter := &fakeAPIChannelDistributedLimiter{
		allowedByKey: map[string]bool{
			"ratelimit:model_gateway:api_channel_key:31:rpm": false,
			"ratelimit:model_gateway:api_channel_key:32:rpm": true,
		},
	}
	ref, err := selectAPIChannelCredentialWithLimiter(context.Background(), lister, aes, &model.APIChannel{ID: 9003, CredentialEnc: encryptTestToken(t, aes, "legacy")}, 0, limiter)
	if err != nil {
		t.Fatalf("selectAPIChannelCredentialWithLimiter() error = %v", err)
	}
	if ref.Token != "available" || ref.KeyID != 32 {
		t.Fatalf("selected token/key = %q/%d", ref.Token, ref.KeyID)
	}
}

func TestAddAPIChannelCredentialMeta(t *testing.T) {
	meta := map[string]any{}
	addAPIChannelCredentialMeta(meta, &APIChannelCredentialRef{
		Source:  apiChannelCredentialSourceKeyPool,
		KeyID:   42,
		KeyName: "primary",
	})
	if got := meta["api_channel_credential_source"]; got != apiChannelCredentialSourceKeyPool {
		t.Fatalf("credential source = %v", got)
	}
	if got := meta["api_channel_key_id"]; got != uint64(42) {
		t.Fatalf("key id = %v", got)
	}
	if got := meta["api_channel_key_name"]; got != "primary" {
		t.Fatalf("key name = %v", got)
	}
}

func TestRecordAPIChannelCredentialUsage(t *testing.T) {
	rec := &fakeAPIChannelKeyRecorder{}
	recordAPIChannelCredentialError(context.Background(), rec, &APIChannelCredentialRef{KeyID: 42}, errors.New(strings.Repeat("x", 600)))
	if rec.calls != 1 {
		t.Fatalf("calls = %d", rec.calls)
	}
	if rec.id != 42 {
		t.Fatalf("id = %d", rec.id)
	}
	if _, ok := rec.fields["last_used_at"].(time.Time); !ok {
		t.Fatalf("last_used_at = %#v", rec.fields["last_used_at"])
	}
	if got := rec.fields["last_error"].(string); len([]rune(got)) != 512 {
		t.Fatalf("last_error length = %d", len([]rune(got)))
	}

	recordAPIChannelCredentialSuccess(context.Background(), rec, &APIChannelCredentialRef{KeyID: 42})
	if rec.calls != 2 {
		t.Fatalf("calls after success = %d", rec.calls)
	}
	if rec.fields["last_error"] != nil {
		t.Fatalf("success should clear last_error, got %#v", rec.fields["last_error"])
	}
}

func TestRecordAPIChannelCredentialUsageSkipsLegacyCredential(t *testing.T) {
	rec := &fakeAPIChannelKeyRecorder{}
	recordAPIChannelCredentialSuccess(context.Background(), rec, &APIChannelCredentialRef{Source: apiChannelCredentialSourceLegacy})
	if rec.calls != 0 {
		t.Fatalf("legacy credential should not update key pool, calls = %d", rec.calls)
	}
}

func TestAPIChannelVideoAcceptedErrorLocksFallback(t *testing.T) {
	err := &apiChannelVideoAcceptedError{
		RemoteTaskID: "remote-1",
		Phase:        "unknown_timeout",
		Message:      "poll timed out after accepted",
	}
	if !isAPIChannelVideoAcceptedError(err) {
		t.Fatal("accepted video error should be detected")
	}
	if canRetryAPIChannelRouteError(err) {
		t.Fatal("accepted video error must not fall back to another route")
	}
	if !canRetryAPIChannelRouteError(errors.New("api channel video submit http 503: upstream unavailable")) {
		t.Fatal("submit-before-accept 503 should remain retryable")
	}
}

func TestAPIChannelVideoPayloadFiltersInternalParams(t *testing.T) {
	payload := apiChannelVideoPayload(&model.GenerationTask{
		ModelCode: "vid-v1",
		Prompt:    "make a video",
		Count:     1,
	}, ProviderRoute{UpstreamModel: "vid-upstream"}, map[string]any{
		"duration":                      float64(5),
		"quality":                       "hd",
		"_model_gateway_route_snapshot": map[string]any{"secret": "no"},
	}, []string{"https://example.test/ref.png"})
	if payload["model"] != "vid-upstream" {
		t.Fatalf("model = %#v", payload["model"])
	}
	if payload["duration"] != float64(5) {
		t.Fatalf("duration = %#v", payload["duration"])
	}
	if _, ok := payload["_model_gateway_route_snapshot"]; ok {
		t.Fatal("internal route snapshot leaked into payload")
	}
	if payload["image"] != "https://example.test/ref.png" {
		t.Fatalf("image = %#v", payload["image"])
	}
}

func TestAPIChannelVideoAssetFromLocalTaskEnvelope(t *testing.T) {
	body := map[string]any{
		"status": "succeeded",
		"result": map[string]any{
			"data": []any{
				map[string]any{
					"url":         "https://cdn.example.test/generated_video.mp4",
					"cover_url":   "https://cdn.example.test/cover.jpg",
					"duration_ms": float64(5000),
					"width":       float64(1280),
					"height":      float64(720),
				},
			},
		},
	}
	asset, ok := apiChannelVideoAssetFromBody(body)
	if !ok {
		t.Fatal("expected video asset")
	}
	if asset.URL != "https://cdn.example.test/generated_video.mp4" {
		t.Fatalf("url = %q", asset.URL)
	}
	if asset.ThumbURL != "https://cdn.example.test/cover.jpg" {
		t.Fatalf("cover = %q", asset.ThumbURL)
	}
	if asset.DurationMs != 5000 || asset.Width != 1280 || asset.Height != 720 {
		t.Fatalf("asset dimensions/duration = %#v", asset)
	}
	if !apiChannelVideoBodyTerminalSuccess(body) {
		t.Fatal("succeeded body should be terminal success")
	}
}

func TestProviderRouteSnapshotSupportsVideoAPIChannel(t *testing.T) {
	payload := providerRouteSnapshotPayload("vid-v1", string(provider.KindVideo), []ProviderRoute{
		{
			SourceType:    model.ModelSourceTypeAPIChannel,
			SourceCode:    "video-official",
			Adapter:       model.APIChannelAdapterOpenAIVideo,
			Provider:      generationProviderAPIChannel,
			UpstreamModel: "vid-v1",
			Strategy:      "round_robin",
		},
	}, 0)
	candidates, ok := payload["candidates"].([]map[string]any)
	if !ok || len(candidates) != 1 {
		t.Fatalf("candidates = %#v", payload["candidates"])
	}
	if candidates[0]["adapter"] != model.APIChannelAdapterOpenAIVideo {
		t.Fatalf("adapter = %#v", candidates[0]["adapter"])
	}
}

func TestRunAPIChannelVideoSubmitPollSuccess(t *testing.T) {
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/video/generations":
			return jsonResponse(200, `{"task_id":"remote-1","status":"queued"}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/v1/video/generations/remote-1":
			return jsonResponse(200, `{"status":"succeeded","data":[{"url":"https://cdn.example.test/generated_video.mp4","cover_url":"https://cdn.example.test/cover.jpg"}]}`), nil
		default:
			return jsonResponse(404, `{"error":"not found"}`), nil
		}
	})}

	stages := []string{}
	log := func(ctx context.Context, e provider.UpstreamLogEntry) {
		stages = append(stages, e.Stage)
	}
	snapshots := []map[string]any{}
	svc := &GenerationService{videoJobSnapshotHook: func(ctx context.Context, taskID string, snapshot map[string]any) {
		if taskID != "local-task" {
			t.Fatalf("snapshot taskID = %q", taskID)
		}
		snapshots = append(snapshots, snapshot)
	}}
	res, err := svc.runAPIChannelVideo(context.Background(), client, log,
		&model.GenerationTask{TaskID: "local-task", ModelCode: "vid-v1", Prompt: "make video", Count: 1},
		ProviderRoute{SourceCode: "video-official", Adapter: model.APIChannelAdapterOpenAIVideo, UpstreamModel: "vid-v1"},
		&model.APIChannel{BaseURL: "https://video.example.test/v1"},
		&APIChannelCredentialRef{Token: "test"},
		map[string]any{"duration": float64(5)},
		nil,
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("runAPIChannelVideo() error = %v", err)
	}
	if res == nil || len(res.Assets) != 1 || res.Assets[0].URL != "https://cdn.example.test/generated_video.mp4" {
		t.Fatalf("result = %#v", res)
	}
	for _, want := range []string{"api_channel.video.submit", "api_channel.video.accepted", "api_channel.video.poll", "api_channel.video.success"} {
		if !containsString(stages, want) {
			t.Fatalf("stages = %#v, missing %s", stages, want)
		}
	}
	assertVideoJobSnapshot(t, snapshots, "accepted", "remote-1")
	assertVideoJobSnapshot(t, snapshots, "terminal_success", "remote-1")
}

func TestRunAPIChannelVideoTerminalFailureLocksFallback(t *testing.T) {
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/video/generations":
			return jsonResponse(200, `{"task_id":"remote-2","status":"queued"}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/v1/video/generations/remote-2":
			return jsonResponse(200, `{"status":"failed","error":{"message":"upstream rejected video"}}`), nil
		default:
			return jsonResponse(404, `{"error":"not found"}`), nil
		}
	})}

	snapshots := []map[string]any{}
	svc := &GenerationService{videoJobSnapshotHook: func(ctx context.Context, taskID string, snapshot map[string]any) {
		snapshots = append(snapshots, snapshot)
	}}
	_, err := svc.runAPIChannelVideo(context.Background(), client, nil,
		&model.GenerationTask{TaskID: "local-task", ModelCode: "vid-v1", Prompt: "make video", Count: 1},
		ProviderRoute{SourceCode: "video-official", Adapter: model.APIChannelAdapterOpenAIVideo, UpstreamModel: "vid-v1"},
		&model.APIChannel{BaseURL: "https://video.example.test/v1"},
		&APIChannelCredentialRef{Token: "test"},
		nil,
		nil,
		5*time.Second,
	)
	if !isAPIChannelVideoAcceptedError(err) {
		t.Fatalf("error = %v, want accepted video error", err)
	}
	if canRetryAPIChannelRouteError(err) {
		t.Fatal("terminal failure after accepted must not fall back")
	}
	assertVideoJobSnapshot(t, snapshots, "accepted", "remote-2")
	failed := assertVideoJobSnapshot(t, snapshots, "terminal_failed", "remote-2")
	if failed["error"] != "upstream rejected video" {
		t.Fatalf("terminal_failed error = %#v", failed["error"])
	}
}

func TestRunAPIChannelVideoUnknownTimeoutLocksFallback(t *testing.T) {
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/video/generations" {
			return jsonResponse(200, `{"task_id":"remote-timeout","status":"queued"}`), nil
		}
		return jsonResponse(500, `{"error":"poll should not run after immediate deadline"}`), nil
	})}

	stages := []string{}
	snapshots := []map[string]any{}
	svc := &GenerationService{videoJobSnapshotHook: func(ctx context.Context, taskID string, snapshot map[string]any) {
		snapshots = append(snapshots, snapshot)
	}}
	_, err := svc.runAPIChannelVideo(context.Background(), client, func(ctx context.Context, e provider.UpstreamLogEntry) {
		stages = append(stages, e.Stage)
	},
		&model.GenerationTask{TaskID: "local-task", ModelCode: "vid-v1", Prompt: "make video", Count: 1},
		ProviderRoute{SourceCode: "video-official", Adapter: model.APIChannelAdapterOpenAIVideo, UpstreamModel: "vid-v1"},
		&model.APIChannel{BaseURL: "https://video.example.test/v1"},
		&APIChannelCredentialRef{Token: "test"},
		nil,
		nil,
		-time.Second,
	)
	if !isAPIChannelVideoAcceptedError(err) {
		t.Fatalf("error = %v, want accepted video error", err)
	}
	if canRetryAPIChannelRouteError(err) {
		t.Fatal("unknown timeout after accepted must not fall back")
	}
	if !containsString(stages, "api_channel.video.unknown_timeout") {
		t.Fatalf("stages = %#v, missing unknown_timeout", stages)
	}
	assertVideoJobSnapshot(t, snapshots, "accepted", "remote-timeout")
	timeoutSnapshot := assertVideoJobSnapshot(t, snapshots, "unknown_timeout", "remote-timeout")
	if timeoutSnapshot["unknown_timeout_at"] == "" {
		t.Fatalf("unknown_timeout_at missing: %#v", timeoutSnapshot)
	}
}

func TestRunAPIChannelVideoAcceptedMissingIDLocksFallback(t *testing.T) {
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/video/generations" {
			return jsonResponse(200, `{"status":"queued"}`), nil
		}
		return jsonResponse(404, `{"error":"not found"}`), nil
	})}

	snapshots := []map[string]any{}
	svc := &GenerationService{videoJobSnapshotHook: func(ctx context.Context, taskID string, snapshot map[string]any) {
		snapshots = append(snapshots, snapshot)
	}}
	_, err := svc.runAPIChannelVideo(context.Background(), client, nil,
		&model.GenerationTask{TaskID: "local-task", ModelCode: "vid-v1", Prompt: "make video", Count: 1},
		ProviderRoute{SourceCode: "video-official", Adapter: model.APIChannelAdapterOpenAIVideo, UpstreamModel: "vid-v1"},
		&model.APIChannel{BaseURL: "https://video.example.test/v1"},
		&APIChannelCredentialRef{Token: "test"},
		nil,
		nil,
		5*time.Second,
	)
	if !isAPIChannelVideoAcceptedError(err) {
		t.Fatalf("error = %v, want accepted video error", err)
	}
	if canRetryAPIChannelRouteError(err) {
		t.Fatal("accepted missing id must not fall back")
	}
	assertVideoJobSnapshot(t, snapshots, "accepted_missing_id", "")
}

func TestRunAPIChannelVideoSubmitBeforeAcceptFailureCanFallback(t *testing.T) {
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/video/generations" {
			return jsonResponse(503, `{"error":"temporarily unavailable"}`), nil
		}
		return jsonResponse(404, `{"error":"not found"}`), nil
	})}

	snapshotCalls := 0
	svc := &GenerationService{videoJobSnapshotHook: func(ctx context.Context, taskID string, snapshot map[string]any) {
		snapshotCalls++
	}}
	_, err := svc.runAPIChannelVideo(context.Background(), client, nil,
		&model.GenerationTask{TaskID: "local-task", ModelCode: "vid-v1", Prompt: "make video", Count: 1},
		ProviderRoute{SourceCode: "video-official", Adapter: model.APIChannelAdapterOpenAIVideo, UpstreamModel: "vid-v1"},
		&model.APIChannel{BaseURL: "https://video.example.test/v1"},
		&APIChannelCredentialRef{Token: "test"},
		nil,
		nil,
		5*time.Second,
	)
	if err == nil {
		t.Fatal("expected submit failure")
	}
	if !canRetryAPIChannelRouteError(err) {
		t.Fatalf("submit-before-accept failure should be retryable, got %v", err)
	}
	if snapshotCalls != 0 {
		t.Fatalf("submit-before-accept failure should not write video job snapshot, calls=%d", snapshotCalls)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertVideoJobSnapshot(t *testing.T, snapshots []map[string]any, phase, remoteTaskID string) map[string]any {
	t.Helper()
	for _, snapshot := range snapshots {
		if snapshot["phase"] != phase {
			continue
		}
		if snapshot["remote_task_id"] != remoteTaskID {
			t.Fatalf("snapshot phase %s remote_task_id = %#v, want %q", phase, snapshot["remote_task_id"], remoteTaskID)
		}
		if snapshot["source_type"] != model.ModelSourceTypeAPIChannel {
			t.Fatalf("snapshot source_type = %#v", snapshot["source_type"])
		}
		if snapshot["source_code"] != "video-official" {
			t.Fatalf("snapshot source_code = %#v", snapshot["source_code"])
		}
		if snapshot["adapter"] != model.APIChannelAdapterOpenAIVideo {
			t.Fatalf("snapshot adapter = %#v", snapshot["adapter"])
		}
		if snapshot["upstream_model"] != "vid-v1" {
			t.Fatalf("snapshot upstream_model = %#v", snapshot["upstream_model"])
		}
		if snapshot["fallback_locked"] != true {
			t.Fatalf("snapshot fallback_locked = %#v", snapshot["fallback_locked"])
		}
		return snapshot
	}
	t.Fatalf("missing video job snapshot phase=%s remote_task_id=%s in %#v", phase, remoteTaskID, snapshots)
	return nil
}

type fakeRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type fakeAPIChannelKeyRecorder struct {
	calls  int
	id     uint64
	fields map[string]any
}

func (r *fakeAPIChannelKeyRecorder) UpdateKey(ctx context.Context, id uint64, fields map[string]any) error {
	r.calls++
	r.id = id
	r.fields = fields
	return nil
}

type fakeAPIChannelKeyLister struct {
	keys []*model.APIChannelKey
}

func (l fakeAPIChannelKeyLister) ListKeys(ctx context.Context, f repo.APIChannelKeyListFilter) ([]*model.APIChannelKey, int64, error) {
	out := make([]*model.APIChannelKey, 0, len(l.keys))
	for _, key := range l.keys {
		if key == nil {
			continue
		}
		if f.ChannelID > 0 && key.ChannelID != f.ChannelID {
			continue
		}
		if f.Status != nil && key.Status != *f.Status {
			continue
		}
		out = append(out, key)
	}
	return out, int64(len(out)), nil
}

type fakeAPIChannelDistributedLimiter struct {
	allowed      bool
	allowedByKey map[string]bool
	err          error
	calls        []fakeAPIChannelDistributedLimiterCall
}

type fakeAPIChannelDistributedLimiterCall struct {
	key  string
	rate int
	n    int
}

func (l *fakeAPIChannelDistributedLimiter) allowN(ctx context.Context, key string, ratePerMin, n int) (bool, error) {
	l.calls = append(l.calls, fakeAPIChannelDistributedLimiterCall{key: key, rate: ratePerMin, n: n})
	if l.err != nil {
		return false, l.err
	}
	if l.allowedByKey != nil {
		return l.allowedByKey[key], nil
	}
	return l.allowed, nil
}

func newTestAES(t *testing.T) *crypto.AESGCM {
	t.Helper()
	aes, err := crypto.NewAESGCM([]byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatalf("NewAESGCM() error = %v", err)
	}
	return aes
}

func encryptTestToken(t *testing.T, aes *crypto.AESGCM, token string) []byte {
	t.Helper()
	enc, err := aes.Encrypt([]byte(token))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	return enc
}
