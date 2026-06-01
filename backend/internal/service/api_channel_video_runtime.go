package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
	"github.com/kleinai/backend/pkg/outbound"
)

const modelGatewayVideoJobSnapshotKey = "_model_gateway_video_job"

type apiChannelVideoAcceptedError struct {
	RemoteTaskID string
	Phase        string
	Message      string
}

func (e *apiChannelVideoAcceptedError) Error() string {
	if e == nil {
		return "api channel video failed after accepted"
	}
	parts := []string{"api channel video failed after accepted"}
	if e.Phase != "" {
		parts = append(parts, "phase="+e.Phase)
	}
	if e.RemoteTaskID != "" {
		parts = append(parts, "remote_task_id="+e.RemoteTaskID)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, ": ")
}

func isAPIChannelVideoAcceptedError(err error) bool {
	var accepted *apiChannelVideoAcceptedError
	return errors.As(err, &accepted)
}

func (s *GenerationService) generateVideoWithAPIChannelRoute(ctx context.Context, t *model.GenerationTask, route ProviderRoute, ch *model.APIChannel, params map[string]any, refs []string, timeout time.Duration) (*provider.Result, error) {
	if route.Adapter != model.APIChannelAdapterOpenAIVideo {
		return nil, fmt.Errorf("api channel adapter does not support video generation: %s", route.Adapter)
	}
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	credRef, err := selectAPIChannelCredentialWithLimiter(ctx, s.apiChannelRepo, s.aes, ch, 0, s.apiLimiter)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetRunningNoAccount(ctx, t.TaskID); err != nil {
		return nil, fmt.Errorf("api channel video set running failed: %w", err)
	}
	proxyURL, err := s.resolveProxyURLByID(ctx, ch.ProxyID)
	if err != nil {
		return nil, fmt.Errorf("api channel video proxy failed: %w", err)
	}
	client, err := apiChannelVideoHTTPClient(timeout, proxyURL)
	if err != nil {
		return nil, err
	}
	log := s.makeUpstreamLoggerForAPIChannel(t, route, ch, credRef)
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := s.runAPIChannelVideo(rctx, client, log, t, route, ch, credRef, params, refs, timeout)
	if err != nil {
		recordAPIChannelCredentialError(ctx, s.apiChannelRepo, credRef, err)
		return nil, err
	}
	recordAPIChannelCredentialSuccess(ctx, s.apiChannelRepo, credRef)
	return result, nil
}

func apiChannelVideoHTTPClient(timeout time.Duration, proxyURL string) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	if strings.TrimSpace(proxyURL) == "" {
		return &http.Client{Timeout: timeout}, nil
	}
	return outbound.NewClient(outbound.Options{Timeout: timeout, ProxyURL: proxyURL, Mode: outbound.ModeUTLS, Profile: outbound.ProfileChrome})
}

func (s *GenerationService) runAPIChannelVideo(ctx context.Context, client *http.Client, upstreamLog provider.UpstreamLogger, t *model.GenerationTask, route ProviderRoute, ch *model.APIChannel, credRef *APIChannelCredentialRef, params map[string]any, refs []string, timeout time.Duration) (*provider.Result, error) {
	createURL := openAICompatibleVideoEndpoint(ch.BaseURL)
	payload := apiChannelVideoPayload(t, route, params, refs)
	rawPayload, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(rawPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credRef.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "kleinai/1.0")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.submit", http.MethodPost, createURL, 0, time.Since(start), rawPayload, nil, err, nil)
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.submit_failed_before_accept", http.MethodPost, createURL, resp.StatusCode, time.Since(start), rawPayload, raw, nil, nil)
		return nil, fmt.Errorf("api channel video submit http %d: %s", resp.StatusCode, snippet(raw, 320))
	}
	apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.submit", http.MethodPost, createURL, resp.StatusCode, time.Since(start), rawPayload, raw, nil, nil)

	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, &apiChannelVideoAcceptedError{Phase: "submit.decode", Message: err.Error()}
	}
	if asset, ok := apiChannelVideoAssetFromBody(body); ok && apiChannelVideoBodyTerminalSuccess(body) {
		apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.success", http.MethodPost, createURL, resp.StatusCode, 0, nil, raw, nil, map[string]any{"immediate": true})
		return &provider.Result{TaskID: t.TaskID, Assets: []provider.Asset{asset}, Latency: time.Since(start)}, nil
	}

	remoteTaskID := apiChannelVideoTaskID(body)
	status := apiChannelVideoStatus(body)
	if remoteTaskID == "" {
		if apiChannelVideoStatusIsPending(status) || status == "" {
			_ = s.mergeAPIChannelVideoJob(ctx, t.TaskID, apiChannelVideoJobSnapshot(route, remoteTaskID, "accepted_missing_id", 0, status, map[string]any{"fallback_locked": true}))
			apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.accepted", http.MethodPost, createURL, resp.StatusCode, 0, nil, raw, nil, map[string]any{"missing_remote_task_id": true, "status": status})
			return nil, &apiChannelVideoAcceptedError{Phase: "accepted_missing_id", Message: "accepted response did not include a remote task id"}
		}
		return nil, fmt.Errorf("api channel video submit returned no result URL or task id: %s", snippet(raw, 320))
	}

	_ = s.mergeAPIChannelVideoJob(ctx, t.TaskID, apiChannelVideoJobSnapshot(route, remoteTaskID, "accepted", 0, status, map[string]any{"fallback_locked": true}))
	apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.accepted", http.MethodPost, createURL, resp.StatusCode, 0, nil, raw, nil, map[string]any{"remote_task_id": remoteTaskID, "status": status})
	return s.pollAPIChannelVideo(ctx, client, upstreamLog, t, route, createURL, remoteTaskID, timeout, start)
}

func (s *GenerationService) pollAPIChannelVideo(ctx context.Context, client *http.Client, upstreamLog provider.UpstreamLogger, t *model.GenerationTask, route ProviderRoute, createURL, remoteTaskID string, timeout time.Duration, startedAt time.Time) (*provider.Result, error) {
	pollURL := strings.TrimRight(createURL, "/") + "/" + url.PathEscape(remoteTaskID)
	deadline := time.Now().Add(timeout)
	for poll := 1; ; poll++ {
		if time.Now().After(deadline) {
			_ = s.mergeAPIChannelVideoJob(ctx, t.TaskID, apiChannelVideoJobSnapshot(route, remoteTaskID, "unknown_timeout", poll-1, "", map[string]any{"fallback_locked": true, "unknown_timeout_at": time.Now().UTC().Format(time.RFC3339)}))
			apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.unknown_timeout", http.MethodGet, pollURL, 0, time.Since(startedAt), nil, nil, nil, map[string]any{"remote_task_id": remoteTaskID, "poll_attempts": poll - 1})
			return nil, &apiChannelVideoAcceptedError{RemoteTaskID: remoteTaskID, Phase: "unknown_timeout", Message: "poll timed out after accepted; fallback is locked"}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return nil, &apiChannelVideoAcceptedError{RemoteTaskID: remoteTaskID, Phase: "poll.request", Message: err.Error()}
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "kleinai/1.0")
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.poll", http.MethodGet, pollURL, 0, time.Since(start), nil, nil, err, map[string]any{"remote_task_id": remoteTaskID, "poll": poll})
			return nil, &apiChannelVideoAcceptedError{RemoteTaskID: remoteTaskID, Phase: "poll.request", Message: err.Error()}
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		body := map[string]any{}
		decodeErr := json.Unmarshal(raw, &body)
		status := ""
		if decodeErr == nil {
			status = apiChannelVideoStatus(body)
		}
		apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.poll", http.MethodGet, pollURL, resp.StatusCode, time.Since(start), nil, raw, decodeErr, map[string]any{"remote_task_id": remoteTaskID, "poll": poll, "status": status})
		if resp.StatusCode/100 != 2 {
			_ = s.mergeAPIChannelVideoJob(ctx, t.TaskID, apiChannelVideoJobSnapshot(route, remoteTaskID, "poll_http_failed", poll, status, map[string]any{"http_status": resp.StatusCode, "fallback_locked": true}))
			return nil, &apiChannelVideoAcceptedError{RemoteTaskID: remoteTaskID, Phase: "poll_http_failed", Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, snippet(raw, 320))}
		}
		if decodeErr != nil {
			_ = s.mergeAPIChannelVideoJob(ctx, t.TaskID, apiChannelVideoJobSnapshot(route, remoteTaskID, "poll_decode_failed", poll, status, map[string]any{"fallback_locked": true}))
			return nil, &apiChannelVideoAcceptedError{RemoteTaskID: remoteTaskID, Phase: "poll_decode_failed", Message: decodeErr.Error()}
		}
		if apiChannelVideoBodyTerminalFailed(body) {
			reason := apiChannelVideoErrorMessage(body)
			_ = s.mergeAPIChannelVideoJob(ctx, t.TaskID, apiChannelVideoJobSnapshot(route, remoteTaskID, "terminal_failed", poll, status, map[string]any{"fallback_locked": true, "error": reason}))
			apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.failed", http.MethodGet, pollURL, resp.StatusCode, 0, nil, raw, nil, map[string]any{"remote_task_id": remoteTaskID, "poll": poll, "status": status})
			return nil, &apiChannelVideoAcceptedError{RemoteTaskID: remoteTaskID, Phase: "terminal_failed", Message: reason}
		}
		if asset, ok := apiChannelVideoAssetFromBody(body); ok && apiChannelVideoBodyTerminalSuccess(body) {
			_ = s.mergeAPIChannelVideoJob(ctx, t.TaskID, apiChannelVideoJobSnapshot(route, remoteTaskID, "terminal_success", poll, status, map[string]any{"fallback_locked": true, "completed_at": time.Now().UTC().Format(time.RFC3339)}))
			apiChannelVideoLog(ctx, upstreamLog, "api_channel.video.success", http.MethodGet, pollURL, resp.StatusCode, 0, nil, raw, nil, map[string]any{"remote_task_id": remoteTaskID, "poll": poll, "status": status})
			return &provider.Result{TaskID: t.TaskID, Assets: []provider.Asset{asset}, Latency: time.Since(startedAt)}, nil
		}
		_ = s.mergeAPIChannelVideoJob(ctx, t.TaskID, apiChannelVideoJobSnapshot(route, remoteTaskID, "polling", poll, status, map[string]any{"fallback_locked": true}))
		select {
		case <-ctx.Done():
			return nil, &apiChannelVideoAcceptedError{RemoteTaskID: remoteTaskID, Phase: "poll.context_done", Message: ctx.Err().Error()}
		case <-time.After(2 * time.Second):
		}
	}
}

func apiChannelVideoPayload(t *model.GenerationTask, route ProviderRoute, params map[string]any, refs []string) map[string]any {
	payload := map[string]any{
		"model":  route.UpstreamModel,
		"prompt": t.Prompt,
		"n":      t.Count,
		"async":  true,
	}
	if payload["model"] == "" {
		payload["model"] = t.ModelCode
	}
	for _, key := range []string{"duration", "duration_sec", "size", "ratio", "aspect_ratio", "quality", "fps", "callback_url"} {
		if value, ok := params[key]; ok && !isZeroAPIChannelVideoValue(value) {
			switch key {
			case "duration_sec":
				payload["duration"] = value
			default:
				payload[key] = value
			}
		}
	}
	if len(refs) > 0 {
		payload["image"] = refs[0]
		payload["images"] = refs
		payload["ref_assets"] = refs
	}
	extra := map[string]any{}
	for key, value := range params {
		if strings.HasPrefix(key, "_") || isZeroAPIChannelVideoValue(value) {
			continue
		}
		if _, exists := payload[key]; exists {
			continue
		}
		extra[key] = value
	}
	if len(extra) > 0 {
		payload["params"] = extra
	}
	return payload
}

func isZeroAPIChannelVideoValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case int:
		return v == 0
	case int64:
		return v == 0
	case float64:
		return v == 0
	default:
		return false
	}
}

func (s *GenerationService) mergeAPIChannelVideoJob(ctx context.Context, taskID string, snapshot map[string]any) error {
	if s == nil || taskID == "" || len(snapshot) == 0 {
		return nil
	}
	if s.videoJobSnapshotHook != nil {
		copied := make(map[string]any, len(snapshot))
		for key, value := range snapshot {
			copied[key] = value
		}
		s.videoJobSnapshotHook(ctx, taskID, copied)
	}
	if s.repo == nil {
		return nil
	}
	return s.repo.MergeParams(ctx, taskID, map[string]any{modelGatewayVideoJobSnapshotKey: snapshot})
}

func apiChannelVideoJobSnapshot(route ProviderRoute, remoteTaskID, phase string, pollAttempts int, lastStatus string, extra map[string]any) map[string]any {
	out := map[string]any{
		"version":          1,
		"source_type":      model.ModelSourceTypeAPIChannel,
		"source_code":      route.SourceCode,
		"adapter":          route.Adapter,
		"upstream_model":   route.UpstreamModel,
		"remote_task_id":   remoteTaskID,
		"phase":            phase,
		"poll_attempts":    pollAttempts,
		"last_poll_status": lastStatus,
		"fallback_locked":  true,
		"updated_at":       time.Now().UTC().Format(time.RFC3339),
	}
	if phase == "accepted" || phase == "accepted_missing_id" {
		out["accepted_at"] = out["updated_at"]
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func apiChannelVideoLog(ctx context.Context, log provider.UpstreamLogger, stage, method, url string, status int, duration time.Duration, request, response []byte, callErr error, meta map[string]any) {
	if log == nil {
		return
	}
	entry := provider.UpstreamLogEntry{
		Stage:      stage,
		Method:     method,
		URL:        url,
		StatusCode: status,
		DurationMs: duration.Milliseconds(),
		Meta:       meta,
	}
	if len(request) > 0 {
		entry.RequestExcerpt = snippet(request, 600)
	}
	if len(response) > 0 {
		entry.ResponseExcerpt = snippet(response, 600)
	}
	if callErr != nil {
		entry.Error = callErr.Error()
	}
	log(ctx, entry)
}

func apiChannelVideoTaskID(body map[string]any) string {
	if id := firstStringField(body, "task_id", "job_id", "prediction_id", "request_id", "id"); id != "" {
		return id
	}
	for _, key := range []string{"task", "job", "prediction", "data"} {
		if nested, ok := body[key].(map[string]any); ok {
			if id := apiChannelVideoTaskID(nested); id != "" {
				return id
			}
		}
	}
	return ""
}

func apiChannelVideoStatus(body map[string]any) string {
	if status := firstStringField(body, "status", "state", "phase"); status != "" {
		return strings.ToLower(strings.TrimSpace(status))
	}
	for _, key := range []string{"task", "job", "prediction", "data"} {
		if nested, ok := body[key].(map[string]any); ok {
			if status := apiChannelVideoStatus(nested); status != "" {
				return status
			}
		}
	}
	return ""
}

func apiChannelVideoBodyTerminalSuccess(body map[string]any) bool {
	status := apiChannelVideoStatus(body)
	if status == "" {
		_, ok := apiChannelVideoAssetFromBody(body)
		return ok
	}
	switch status {
	case "succeeded", "success", "completed", "complete", "done", "finished":
		return true
	default:
		return false
	}
}

func apiChannelVideoBodyTerminalFailed(body map[string]any) bool {
	switch apiChannelVideoStatus(body) {
	case "failed", "error", "errored", "cancelled", "canceled", "expired", "refunded":
		return true
	default:
		return false
	}
}

func apiChannelVideoStatusIsPending(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "queued", "pending", "processing", "running", "in_progress", "accepted", "submitted", "generating":
		return true
	default:
		return false
	}
}

func apiChannelVideoAssetFromBody(body map[string]any) (provider.Asset, bool) {
	rawURL := firstVideoURL(body)
	if rawURL == "" {
		return provider.Asset{}, false
	}
	return provider.Asset{
		URL:        rawURL,
		ThumbURL:   firstVideoCoverURL(body),
		Width:      firstIntFieldRecursive(body, "width"),
		Height:     firstIntFieldRecursive(body, "height"),
		DurationMs: firstIntFieldRecursive(body, "duration_ms"),
		Mime:       "video/mp4",
		Meta:       map[string]any{"source": "api_channel_video"},
	}, true
}

func firstVideoURL(v any) string {
	switch value := v.(type) {
	case map[string]any:
		for _, key := range []string{"video_url", "result_url", "output_url", "download_url", "file_url", "url"} {
			if s := stringValue(value[key]); looksLikeVideoResultURL(s) {
				return s
			}
		}
		if s := stringValue(value["output"]); looksLikeVideoResultURL(s) {
			return s
		}
		for _, key := range []string{"data", "result", "results", "output", "outputs", "video", "videos"} {
			if s := firstVideoURL(value[key]); s != "" {
				return s
			}
		}
	case []any:
		for _, item := range value {
			if s := firstVideoURL(item); s != "" {
				return s
			}
		}
	case []string:
		for _, item := range value {
			if looksLikeVideoResultURL(item) {
				return strings.TrimSpace(item)
			}
		}
	case string:
		if looksLikeVideoResultURL(value) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstVideoCoverURL(v any) string {
	switch value := v.(type) {
	case map[string]any:
		for _, key := range []string{"cover_url", "thumbnail_url", "thumb_url", "poster_url"} {
			if s := stringValue(value[key]); s != "" {
				return s
			}
		}
		for _, key := range []string{"data", "result", "results", "output", "outputs", "video", "videos"} {
			if s := firstVideoCoverURL(value[key]); s != "" {
				return s
			}
		}
	case []any:
		for _, item := range value {
			if s := firstVideoCoverURL(item); s != "" {
				return s
			}
		}
	}
	return ""
}

func looksLikeVideoResultURL(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	lower := strings.ToLower(v)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:video/") ||
		strings.HasPrefix(lower, "/api/") ||
		strings.Contains(lower, ".mp4") ||
		strings.Contains(lower, ".webm") ||
		strings.Contains(lower, "video")
}

func apiChannelVideoErrorMessage(body map[string]any) string {
	for _, key := range []string{"error", "message", "error_message", "detail"} {
		if msg := stringValue(body[key]); msg != "" {
			return msg
		}
		if nested, ok := body[key].(map[string]any); ok {
			if msg := apiChannelVideoErrorMessage(nested); msg != "" {
				return msg
			}
		}
	}
	return "upstream video task failed"
}

func firstStringField(body map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(body[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstIntFieldRecursive(v any, keys ...string) int {
	switch value := v.(type) {
	case map[string]any:
		for _, key := range keys {
			if n := intValue(value[key]); n > 0 {
				return n
			}
		}
		for _, item := range value {
			if n := firstIntFieldRecursive(item, keys...); n > 0 {
				return n
			}
		}
	case []any:
		for _, item := range value {
			if n := firstIntFieldRecursive(item, keys...); n > 0 {
				return n
			}
		}
	}
	return 0
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return ""
	}
}

func intValue(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return n
	default:
		return 0
	}
}
