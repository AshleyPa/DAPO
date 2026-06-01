package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/outbound"
)

type APIChannelAdminService struct {
	repo     *repo.APIChannelRepo
	aes      *crypto.AESGCM
	proxySvc *ProxyService
}

func NewAPIChannelAdminService(r *repo.APIChannelRepo, aes *crypto.AESGCM, proxySvc ...*ProxyService) *APIChannelAdminService {
	var p *ProxyService
	if len(proxySvc) > 0 {
		p = proxySvc[0]
	}
	return &APIChannelAdminService{repo: r, aes: aes, proxySvc: p}
}

func (s *APIChannelAdminService) List(ctx context.Context, req *dto.APIChannelListReq) ([]*dto.APIChannelResp, int64, error) {
	items, total, err := s.repo.List(ctx, repo.APIChannelListFilter{
		Adapter:  normalizeAPIChannelAdapterLoose(req.Adapter),
		Status:   req.Status,
		Keyword:  strings.TrimSpace(req.Keyword),
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		return nil, 0, errcode.DBError.Wrap(err)
	}
	out := make([]*dto.APIChannelResp, 0, len(items))
	for _, item := range items {
		resp := apiChannelResp(item)
		if totalKeys, enabledKeys, err := s.keyCounts(ctx, item.ID); err == nil {
			resp.KeyCount = totalKeys
			resp.EnabledKeyCount = enabledKeys
		}
		out = append(out, resp)
	}
	return out, total, nil
}

func (s *APIChannelAdminService) Create(ctx context.Context, adminID uint64, req *dto.APIChannelCreateReq) (*model.APIChannel, error) {
	code, err := normalizeAPIChannelCode(req.Code)
	if err != nil {
		return nil, err
	}
	adapter, err := normalizeAPIChannelAdapter(req.Adapter)
	if err != nil {
		return nil, err
	}
	baseURL, err := normalizeAPIChannelBaseURL(req.BaseURL)
	if err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(req.APIKey)
	var enc []byte
	if apiKey == MaskedSecretValue {
		return nil, errcode.InvalidParam.WithMsg("新建 API 渠道不能使用掩码密钥")
	}
	if apiKey != "" {
		var err error
		enc, err = s.aes.Encrypt([]byte(apiKey))
		if err != nil {
			return nil, errcode.Internal.Wrap(err)
		}
	}
	modelsList, err := normalizeAPIChannelModelList(req.Models)
	if err != nil {
		return nil, err
	}
	models, err := marshalStringList(modelsList)
	if err != nil {
		return nil, errcode.InvalidParam.Wrap(err)
	}
	caps := normalizeCapabilityList(req.Capabilities)
	if len(caps) == 0 {
		caps = defaultCapabilitiesForAdapter(adapter)
	}
	capabilities, err := marshalStringList(caps)
	if err != nil {
		return nil, errcode.InvalidParam.Wrap(err)
	}
	status := int8(model.APIChannelStatusEnabled)
	if req.Status != nil {
		status = *req.Status
	}
	priority := req.Priority
	if priority == 0 {
		priority = 100
	}
	weight := req.Weight
	if weight == 0 {
		weight = 100
	}
	timeout := req.TimeoutSeconds
	if timeout == 0 {
		timeout = 300
	}
	ch := &model.APIChannel{
		Code:           code,
		Name:           strings.TrimSpace(req.Name),
		ProviderName:   strings.ToLower(strings.TrimSpace(req.ProviderName)),
		Adapter:        adapter,
		BaseURL:        baseURL,
		CredentialEnc:  enc,
		Models:         stringPtrOrNil(models),
		Capabilities:   stringPtrOrNil(capabilities),
		ProxyID:        req.ProxyID,
		Priority:       priority,
		Weight:         weight,
		RPMLimit:       req.RPMLimit,
		TPMLimit:       req.TPMLimit,
		TimeoutSeconds: timeout,
		Status:         status,
		CreatedBy:      &adminID,
	}
	if remark := strings.TrimSpace(req.Remark); remark != "" {
		ch.Remark = &remark
	}
	if err := s.repo.Create(ctx, ch); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return ch, nil
}

func (s *APIChannelAdminService) Update(ctx context.Context, id uint64, req *dto.APIChannelUpdateReq) error {
	ch, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return errcode.ResourceMissing
	}
	fields := map[string]any{}
	healthDirty := false
	if req.Code != nil {
		code, err := normalizeAPIChannelCode(*req.Code)
		if err != nil {
			return err
		}
		fields["code"] = code
	}
	if req.Name != nil {
		fields["name"] = strings.TrimSpace(*req.Name)
	}
	if req.ProviderName != nil {
		fields["provider_name"] = strings.ToLower(strings.TrimSpace(*req.ProviderName))
	}
	if req.Adapter != nil {
		adapter, err := normalizeAPIChannelAdapter(*req.Adapter)
		if err != nil {
			return err
		}
		fields["adapter"] = adapter
		healthDirty = true
		if len(req.Capabilities) == 0 {
			caps, _ := marshalStringList(defaultCapabilitiesForAdapter(adapter))
			fields["capabilities"] = stringPtrOrNil(caps)
		}
	}
	if req.BaseURL != nil {
		baseURL, err := normalizeAPIChannelBaseURL(*req.BaseURL)
		if err != nil {
			return err
		}
		fields["base_url"] = baseURL
		healthDirty = true
	}
	credentialDirty, err := s.applyAPIChannelCredentialUpdate(req, fields)
	if err != nil {
		return err
	}
	healthDirty = healthDirty || credentialDirty
	if req.Models != nil {
		modelsList, err := normalizeAPIChannelModelList(req.Models)
		if err != nil {
			return err
		}
		models, err := marshalStringList(modelsList)
		if err != nil {
			return errcode.InvalidParam.Wrap(err)
		}
		fields["models"] = stringPtrOrNil(models)
		if apiChannelModelsChanged(ch.Models, modelsList) {
			healthDirty = true
		}
	}
	if req.Capabilities != nil {
		caps := normalizeCapabilityList(req.Capabilities)
		raw, err := marshalStringList(caps)
		if err != nil {
			return errcode.InvalidParam.Wrap(err)
		}
		fields["capabilities"] = stringPtrOrNil(raw)
	}
	if req.ClearProxy != nil && *req.ClearProxy {
		fields["proxy_id"] = nil
		healthDirty = true
	} else if req.ProxyID != nil {
		fields["proxy_id"] = *req.ProxyID
		healthDirty = true
	}
	if req.Priority != nil {
		fields["priority"] = *req.Priority
	}
	if req.Weight != nil {
		fields["weight"] = *req.Weight
	}
	if req.RPMLimit != nil {
		fields["rpm_limit"] = *req.RPMLimit
	}
	if req.TPMLimit != nil {
		fields["tpm_limit"] = *req.TPMLimit
	}
	if req.TimeoutSeconds != nil {
		fields["timeout_seconds"] = *req.TimeoutSeconds
		if apiChannelTimeoutChanged(ch, req.TimeoutSeconds) {
			healthDirty = true
		}
	}
	if req.Status != nil {
		fields["status"] = *req.Status
		if apiChannelStatusChanged(ch, req.Status) {
			healthDirty = true
		}
	}
	if req.Remark != nil {
		if remark := strings.TrimSpace(*req.Remark); remark == "" {
			fields["remark"] = nil
		} else {
			fields["remark"] = remark
		}
	}
	if healthDirty {
		resetAPIChannelHealthFields(fields)
	}
	if err := s.repo.Update(ctx, id, fields); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *APIChannelAdminService) applyAPIChannelCredentialUpdate(req *dto.APIChannelUpdateReq, fields map[string]any) (bool, error) {
	if req == nil {
		return false, nil
	}
	clear := req.ClearAPIKey != nil && *req.ClearAPIKey
	nextKey := ""
	if req.APIKey != nil {
		nextKey = strings.TrimSpace(*req.APIKey)
	}
	if clear {
		if nextKey != "" && nextKey != MaskedSecretValue {
			return false, errcode.InvalidParam.WithMsg("清除 Legacy API Key 时不能同时设置新 Key")
		}
		fields["credential_enc"] = nil
		return true, nil
	}
	if req.APIKey == nil || nextKey == "" || nextKey == MaskedSecretValue {
		return false, nil
	}
	if s.aes == nil {
		return false, errcode.Internal.WithMsg("API Key 加密器不可用")
	}
	enc, err := s.aes.Encrypt([]byte(nextKey))
	if err != nil {
		return false, errcode.Internal.Wrap(err)
	}
	fields["credential_enc"] = enc
	return true, nil
}

func resetAPIChannelHealthFields(fields map[string]any) {
	fields["last_test_at"] = nil
	fields["last_test_status"] = int8(0)
	fields["last_test_error"] = nil
}

func (s *APIChannelAdminService) Delete(ctx context.Context, id uint64) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return errcode.ResourceMissing
	}
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *APIChannelAdminService) Secrets(ctx context.Context, id uint64) (*dto.APIChannelSecretsResp, error) {
	ch, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, errcode.ResourceMissing
	}
	if len(ch.CredentialEnc) == 0 {
		return &dto.APIChannelSecretsResp{}, nil
	}
	plain, err := s.aes.Decrypt(ch.CredentialEnc)
	if err != nil {
		return nil, errcode.Internal.WithMsg("API Key 解密失败")
	}
	return &dto.APIChannelSecretsResp{APIKey: string(plain)}, nil
}

func (s *APIChannelAdminService) Test(ctx context.Context, id uint64) (*dto.APIChannelTestResp, error) {
	ch, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, errcode.ResourceMissing
	}
	started := time.Now()
	status, ok, errMsg, credRef := s.probe(ctx, ch)
	latency := time.Since(started).Milliseconds()
	now := time.Now().UTC()
	lastStatus := int8(model.AccountTestFail)
	if ok {
		lastStatus = model.AccountTestOK
		errMsg = ""
	}
	fields := map[string]any{
		"last_test_at":     now,
		"last_test_status": lastStatus,
	}
	if errMsg == "" {
		fields["last_test_error"] = nil
	} else {
		fields["last_test_error"] = truncate(errMsg, 512)
	}
	if err := s.repo.Update(ctx, id, fields); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return &dto.APIChannelTestResp{
		OK:               ok,
		Status:           status,
		LatencyMs:        latency,
		Error:            errMsg,
		TestedAt:         now.Unix(),
		CredentialSource: credentialRefSource(credRef),
		KeyID:            credentialRefKeyID(credRef),
		KeyName:          credentialRefKeyName(credRef),
	}, nil
}

func (s *APIChannelAdminService) ListKeys(ctx context.Context, channelID uint64) ([]*dto.APIChannelKeyResp, error) {
	if _, err := s.repo.GetByID(ctx, channelID); err != nil {
		return nil, errcode.ResourceMissing
	}
	items, _, err := s.repo.ListKeys(ctx, repo.APIChannelKeyListFilter{ChannelID: channelID, Page: 1, PageSize: 500})
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	out := make([]*dto.APIChannelKeyResp, 0, len(items))
	for _, item := range items {
		out = append(out, apiChannelKeyResp(item))
	}
	return out, nil
}

func (s *APIChannelAdminService) CreateKey(ctx context.Context, channelID uint64, req *dto.APIChannelKeyCreateReq) (*model.APIChannelKey, error) {
	if _, err := s.repo.GetByID(ctx, channelID); err != nil {
		return nil, errcode.ResourceMissing
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" || apiKey == MaskedSecretValue {
		return nil, errcode.InvalidParam.WithMsg("API Key 不能为空")
	}
	enc, err := s.aes.Encrypt([]byte(apiKey))
	if err != nil {
		return nil, errcode.Internal.Wrap(err)
	}
	priority := req.Priority
	if priority == 0 {
		priority = 100
	}
	weight := req.Weight
	if weight == 0 {
		weight = 100
	}
	status := int8(model.APIChannelKeyStatusEnabled)
	if req.Status != nil {
		status = *req.Status
	}
	key := &model.APIChannelKey{
		ChannelID:     channelID,
		Name:          strings.TrimSpace(req.Name),
		CredentialEnc: enc,
		Priority:      priority,
		Weight:        weight,
		RPMLimit:      req.RPMLimit,
		TPMLimit:      req.TPMLimit,
		Status:        status,
	}
	if key.Name == "" {
		key.Name = "default"
	}
	if err := s.repo.CreateKey(ctx, key); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	if err := s.resetChannelHealth(ctx, channelID); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *APIChannelAdminService) UpdateKey(ctx context.Context, channelID, keyID uint64, req *dto.APIChannelKeyUpdateReq) error {
	key, err := s.repo.GetKeyByID(ctx, keyID)
	if err != nil || key == nil || key.ChannelID != channelID {
		return errcode.ResourceMissing
	}
	fields := map[string]any{}
	if req.Name != nil {
		fields["name"] = strings.TrimSpace(*req.Name)
	}
	if req.APIKey != nil {
		apiKey := strings.TrimSpace(*req.APIKey)
		if apiKey != "" && apiKey != MaskedSecretValue {
			enc, err := s.aes.Encrypt([]byte(apiKey))
			if err != nil {
				return errcode.Internal.Wrap(err)
			}
			fields["credential_enc"] = enc
			fields["last_error"] = nil
		}
	}
	if req.Priority != nil {
		fields["priority"] = *req.Priority
	}
	if req.Weight != nil {
		fields["weight"] = *req.Weight
	}
	if req.RPMLimit != nil {
		fields["rpm_limit"] = *req.RPMLimit
	}
	if req.TPMLimit != nil {
		fields["tpm_limit"] = *req.TPMLimit
	}
	if req.Status != nil {
		fields["status"] = *req.Status
	}
	if err := s.repo.UpdateKey(ctx, keyID, fields); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return s.resetChannelHealth(ctx, channelID)
}

func (s *APIChannelAdminService) DeleteKey(ctx context.Context, channelID, keyID uint64) error {
	key, err := s.repo.GetKeyByID(ctx, keyID)
	if err != nil || key == nil || key.ChannelID != channelID {
		return errcode.ResourceMissing
	}
	if err := s.repo.SoftDeleteKey(ctx, keyID); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return s.resetChannelHealth(ctx, channelID)
}

func (s *APIChannelAdminService) resetChannelHealth(ctx context.Context, channelID uint64) error {
	fields := map[string]any{}
	resetAPIChannelHealthFields(fields)
	if err := s.repo.Update(ctx, channelID, fields); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *APIChannelAdminService) probe(ctx context.Context, ch *model.APIChannel) (int, bool, string, *APIChannelCredentialRef) {
	credRef, err := s.apiKey(ctx, ch)
	if err != nil {
		return 0, false, err.Error(), nil
	}
	proxyURL, err := s.resolveProxyURL(ctx, ch.ProxyID)
	if err != nil {
		return 0, false, err.Error(), credRef
	}
	client, err := outbound.NewClient(outbound.Options{
		ProxyURL: proxyURL,
		Timeout:  apiChannelProbeTimeout(ch),
		Mode:     outbound.ModeUTLS,
		Profile:  outbound.ProfileChrome,
	})
	if err != nil {
		return 0, false, err.Error(), credRef
	}
	authHeader := "Bearer " + credRef.Token
	status, body, err := apiChannelProbeRequest(ctx, client, http.MethodGet, openAICompatibleModelsEndpoint(ch.BaseURL), authHeader, "")
	if err != nil {
		return 0, false, err.Error(), credRef
	}
	if status/100 == 2 {
		if ok, msg := apiChannelProbeModelsOK(ch, body); !ok {
			return status, false, msg, credRef
		}
		return status, true, "", credRef
	}
	// Some compatible providers do not expose /models. Probe a protocol endpoint
	// with an intentionally incomplete body; non-auth validation errors prove the
	// credential, base URL and proxy path are usable without generating content.
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		if ok, probeStatus, msg := s.probeProtocolEndpoint(ctx, client, ch, authHeader); ok || msg != "" {
			return probeStatus, ok, msg, credRef
		}
	}
	return status, false, apiChannelHTTPError(status, body), credRef
}

func (s *APIChannelAdminService) probeProtocolEndpoint(ctx context.Context, client *http.Client, ch *model.APIChannel, authHeader string) (bool, int, string) {
	endpoint, payload := apiChannelProbeEndpoint(ch)
	if endpoint == "" {
		return false, 0, ""
	}
	status, body, err := apiChannelProbeRequest(ctx, client, http.MethodPost, endpoint, authHeader, payload)
	if err != nil {
		return false, 0, err.Error()
	}
	msg := apiChannelHTTPError(status, body)
	if status/100 == 2 {
		return true, status, ""
	}
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		if openAICompatibleAuthFailure(msg) {
			return false, status, msg
		}
		return true, status, ""
	}
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		return false, status, msg
	}
	return false, status, msg
}

func apiChannelProbeEndpoint(ch *model.APIChannel) (string, string) {
	if ch == nil {
		return "", ""
	}
	switch ch.Adapter {
	case model.APIChannelAdapterOpenAIChat:
		return openAICompatibleChatEndpoint(ch.BaseURL), `{}`
	case model.APIChannelAdapterOpenAIImages, model.APIChannelAdapterNovaAsync, model.APIChannelAdapterPic2APIImages:
		return openAICompatibleImageEndpoint(ch.BaseURL), `{}`
	case model.APIChannelAdapterOpenAIVideo:
		return openAICompatibleVideoEndpoint(ch.BaseURL), `{}`
	case model.APIChannelAdapterOpenAIResponses:
		base := normalizeOpenAICompatibleBase(ch.BaseURL)
		if strings.HasSuffix(base, "/v1") {
			return base + "/responses", `{}`
		}
		return base + "/v1/responses", `{}`
	default:
		return "", ""
	}
}

func apiChannelProbeRequest(ctx context.Context, client *http.Client, method, endpoint, authHeader, payload string) (int, []byte, error) {
	var body io.Reader
	if payload != "" {
		body = strings.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")
	if payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, raw, nil
}

func apiChannelHTTPError(status int, body []byte) string {
	msg := strings.TrimSpace(string(body))
	if len(msg) > 300 {
		msg = msg[:300]
	}
	if msg == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	return fmt.Sprintf("HTTP %d: %s", status, msg)
}

func apiChannelProbeModelsOK(ch *model.APIChannel, raw []byte) (bool, string) {
	if ch == nil {
		return true, ""
	}
	expected := parseStringListJSON(ch.Models)
	if len(expected) == 0 {
		return true, ""
	}
	listed := parseAPIChannelProbeModelIDs(raw)
	if len(listed) == 0 {
		return true, ""
	}
	for _, want := range expected {
		wantKey := strings.ToLower(strings.TrimSpace(want))
		if wantKey == "" {
			continue
		}
		for _, got := range listed {
			if strings.ToLower(strings.TrimSpace(got)) == wantKey {
				return true, ""
			}
		}
	}
	return false, fmt.Sprintf("HTTP 200: /models did not list configured channel models; expected one of %s, got %s", summarizeModelIDs(expected, 6), summarizeModelIDs(listed, 8))
}

func parseAPIChannelProbeModelIDs(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if id, ok := v["id"].(string); ok {
				addAPIChannelProbeModelID(&out, seen, id)
			}
			if modelCode, ok := v["model"].(string); ok {
				addAPIChannelProbeModelID(&out, seen, modelCode)
			}
			if data, ok := v["data"]; ok {
				walk(data)
			}
			if models, ok := v["models"]; ok {
				walk(models)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		case []string:
			for _, item := range v {
				addAPIChannelProbeModelID(&out, seen, item)
			}
		case string:
			addAPIChannelProbeModelID(&out, seen, v)
		}
	}
	walk(payload)
	return out
}

func addAPIChannelProbeModelID(out *[]string, seen map[string]bool, value string) {
	item := strings.TrimSpace(value)
	key := strings.ToLower(item)
	if item == "" || seen[key] {
		return
	}
	seen[key] = true
	*out = append(*out, item)
}

func summarizeModelIDs(values []string, limit int) string {
	if limit <= 0 || len(values) <= limit {
		return strings.Join(values, ",")
	}
	return strings.Join(values[:limit], ",") + fmt.Sprintf(",...(+%d)", len(values)-limit)
}

func apiChannelProbeTimeout(ch *model.APIChannel) time.Duration {
	if ch == nil || ch.TimeoutSeconds <= 0 {
		return 20 * time.Second
	}
	timeout := time.Duration(ch.TimeoutSeconds) * time.Second
	if timeout > 60*time.Second {
		return 60 * time.Second
	}
	if timeout < 5*time.Second {
		return 5 * time.Second
	}
	return timeout
}

func (s *APIChannelAdminService) apiKey(ctx context.Context, ch *model.APIChannel) (*APIChannelCredentialRef, error) {
	return firstAPIChannelCredential(ctx, s.repo, s.aes, ch)
}

func credentialRefSource(ref *APIChannelCredentialRef) string {
	if ref == nil {
		return ""
	}
	return ref.Source
}

func credentialRefKeyID(ref *APIChannelCredentialRef) uint64 {
	if ref == nil {
		return 0
	}
	return ref.KeyID
}

func credentialRefKeyName(ref *APIChannelCredentialRef) string {
	if ref == nil {
		return ""
	}
	return ref.KeyName
}

func (s *APIChannelAdminService) resolveProxyURL(ctx context.Context, proxyID *uint64) (string, error) {
	if s.proxySvc == nil || proxyID == nil || *proxyID == 0 {
		return "", nil
	}
	p, err := s.proxySvc.GetByID(ctx, *proxyID)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", fmt.Errorf("代理不存在")
	}
	if p.Status != model.ProxyStatusEnabled {
		return "", fmt.Errorf("代理未启用")
	}
	u, err := s.proxySvc.BuildURL(p)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", fmt.Errorf("代理地址不可用")
	}
	return u.String(), nil
}

func normalizeAPIChannelCode(value string) (string, error) {
	code := strings.ToLower(strings.TrimSpace(value))
	if code == "" {
		return "", errcode.InvalidParam.WithMsg("渠道编码不能为空")
	}
	for _, r := range code {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", errcode.InvalidParam.WithMsg("渠道编码只能包含字母、数字、点、下划线和短横线")
	}
	return code, nil
}

func normalizeAPIChannelAdapter(value string) (string, error) {
	adapter := normalizeAPIChannelAdapterLoose(value)
	switch adapter {
	case model.APIChannelAdapterOpenAIChat,
		model.APIChannelAdapterOpenAIImages,
		model.APIChannelAdapterOpenAIVideo,
		model.APIChannelAdapterOpenAIResponses,
		model.APIChannelAdapterNovaAsync,
		model.APIChannelAdapterPic2APIImages:
		return adapter, nil
	default:
		return "", errcode.InvalidParam.WithMsg("渠道协议只能是 openai_compatible_chat/openai_compatible_images/openai_compatible_video/openai_responses/nova_async/pic2api_images")
	}
}

func normalizeAPIChannelAdapterLoose(value string) string {
	adapter := strings.ToLower(strings.TrimSpace(value))
	switch adapter {
	case "openai", "openai_chat", "openai-compatible-chat", "chat":
		return model.APIChannelAdapterOpenAIChat
	case "images", "openai_images", "openai-compatible-images", "image":
		return model.APIChannelAdapterOpenAIImages
	case "video", "videos", "openai_video", "openai-compatible-video":
		return model.APIChannelAdapterOpenAIVideo
	case "responses", "openai-response", "openai-responses":
		return model.APIChannelAdapterOpenAIResponses
	case "nova", "nova-async":
		return model.APIChannelAdapterNovaAsync
	case "pic2api", "pic2api_image", "pic2api-images":
		return model.APIChannelAdapterPic2APIImages
	default:
		return adapter
	}
}

func normalizeAPIChannelBaseURL(value string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(value), "/")
	if base == "" {
		return "", errcode.InvalidParam.WithMsg("Base URL 不能为空")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errcode.InvalidParam.WithMsg("Base URL 必须是完整 http(s) 地址")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errcode.InvalidParam.WithMsg("Base URL 只支持 http 或 https")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errcode.InvalidParam.WithMsg("Base URL 不能包含用户名、密码、查询参数或片段；API Key 请放入 Key 池")
	}
	return base, nil
}

func normalizeStringList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func normalizeAPIChannelModelList(values []string) ([]string, error) {
	items := normalizeStringList(values)
	for _, item := range items {
		if err := validateAPIChannelModelCode(item); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func apiChannelModelsChanged(current *string, next []string) bool {
	return !slices.Equal(parseStringListJSON(current), next)
}

func apiChannelTimeoutChanged(ch *model.APIChannel, next *int) bool {
	return ch != nil && next != nil && ch.TimeoutSeconds != *next
}

func apiChannelStatusChanged(ch *model.APIChannel, next *int8) bool {
	return ch != nil && next != nil && ch.Status != *next
}

func validateAPIChannelModelCode(value string) error {
	modelCode := strings.TrimSpace(value)
	if modelCode == "" {
		return nil
	}
	lower := strings.ToLower(modelCode)
	sensitiveMarkers := []string{
		"api_key",
		"apikey",
		"access_key",
		"access-token",
		"access_token",
		"refresh-token",
		"refresh_token",
		"authorization",
		"bearer",
		"credential",
		"password",
		"secret",
		"token=",
	}
	for _, marker := range sensitiveMarkers {
		if strings.Contains(lower, marker) {
			return errcode.InvalidParam.WithMsg("可服务模型不能包含 API Key、token 或 secret 等敏感字段")
		}
	}
	if strings.Contains(lower, "://") || strings.ContainsAny(modelCode, "?#=@") {
		return errcode.InvalidParam.WithMsg("可服务模型只能填写模型编码，不能填写 URL、查询参数或凭证片段")
	}
	for _, r := range modelCode {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return errcode.InvalidParam.WithMsg("可服务模型不能包含空白或控制字符")
		}
	}
	return nil
}

func normalizeCapabilityList(values []string) []string {
	allowed := map[string]bool{"chat": true, "image": true, "video": true, "audio": true, "embedding": true}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.ToLower(strings.TrimSpace(value))
		if item == "" || !allowed[item] || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func defaultCapabilitiesForAdapter(adapter string) []string {
	switch adapter {
	case model.APIChannelAdapterOpenAIImages, model.APIChannelAdapterOpenAIResponses, model.APIChannelAdapterNovaAsync, model.APIChannelAdapterPic2APIImages:
		return []string{"image"}
	case model.APIChannelAdapterOpenAIVideo:
		return []string{"video"}
	default:
		return []string{"chat"}
	}
}

func marshalStringList(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func stringPtrOrNil(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func (s *APIChannelAdminService) keyCounts(ctx context.Context, channelID uint64) (int64, int64, error) {
	total, err := s.repo.CountKeys(ctx, channelID, nil)
	if err != nil {
		return 0, 0, err
	}
	status := int8(model.APIChannelKeyStatusEnabled)
	enabled, err := s.repo.CountKeys(ctx, channelID, &status)
	if err != nil {
		return total, 0, err
	}
	return total, enabled, nil
}

func apiChannelResp(ch *model.APIChannel) *dto.APIChannelResp {
	resp := &dto.APIChannelResp{
		ID:             ch.ID,
		Code:           ch.Code,
		Name:           ch.Name,
		ProviderName:   ch.ProviderName,
		Adapter:        ch.Adapter,
		BaseURL:        ch.BaseURL,
		HasAPIKey:      len(ch.CredentialEnc) > 0,
		Models:         parseStringListJSON(ch.Models),
		Capabilities:   parseStringListJSON(ch.Capabilities),
		ProxyID:        ch.ProxyID,
		Priority:       ch.Priority,
		Weight:         ch.Weight,
		RPMLimit:       ch.RPMLimit,
		TPMLimit:       ch.TPMLimit,
		TimeoutSeconds: ch.TimeoutSeconds,
		Status:         ch.Status,
		LastTestStatus: ch.LastTestStatus,
		CreatedAt:      ch.CreatedAt.Unix(),
		UpdatedAt:      ch.UpdatedAt.Unix(),
	}
	if ch.LastTestAt != nil {
		resp.LastTestAt = ch.LastTestAt.Unix()
	}
	if ch.LastTestError != nil {
		resp.LastTestError = *ch.LastTestError
	}
	if ch.Remark != nil {
		resp.Remark = *ch.Remark
	}
	return resp
}

func apiChannelKeyResp(key *model.APIChannelKey) *dto.APIChannelKeyResp {
	resp := &dto.APIChannelKeyResp{
		ID:        key.ID,
		ChannelID: key.ChannelID,
		Name:      key.Name,
		HasAPIKey: len(key.CredentialEnc) > 0,
		Priority:  key.Priority,
		Weight:    key.Weight,
		RPMLimit:  key.RPMLimit,
		TPMLimit:  key.TPMLimit,
		Status:    key.Status,
		CreatedAt: key.CreatedAt.Unix(),
		UpdatedAt: key.UpdatedAt.Unix(),
	}
	if key.LastUsedAt != nil {
		resp.LastUsedAt = key.LastUsedAt.Unix()
	}
	if key.LastError != nil {
		resp.LastError = *key.LastError
	}
	return resp
}

func parseStringListJSON(raw *string) []string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(*raw), &values); err != nil {
		return []string{}
	}
	return normalizeStringList(values)
}
