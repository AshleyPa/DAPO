package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/response"
)

const (
	adminLogRouteSnapshotKey   = "_model_gateway_route_snapshot"
	adminLogPricingSnapshotKey = "_model_gateway_pricing_snapshot"
	adminLogOutputSnapshotKey  = "_model_gateway_output_snapshot"
	adminLogVideoJobKey        = "_model_gateway_video_job"
)

type AdminLogHandler struct {
	gen    *repo.GenerationRepo
	acc    *repo.AccountRepo
	aes    *crypto.AESGCM
	wallet *repo.WalletRepo
}

func NewAdminLogHandler(gen *repo.GenerationRepo, acc *repo.AccountRepo, aes *crypto.AESGCM, wallet ...*repo.WalletRepo) *AdminLogHandler {
	h := &AdminLogHandler{gen: gen, acc: acc, aes: aes}
	if len(wallet) > 0 {
		h.wallet = wallet[0]
	}
	return h
}

func (h *AdminLogHandler) GenerationLogs(c *gin.Context) {
	var req dto.AdminGenerationLogListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	rows, total, err := h.gen.ListAdminLogs(c.Request.Context(), repo.AdminGenerationLogFilter{
		Keyword:  req.Keyword,
		Kind:     req.Kind,
		Status:   req.Status,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		response.Fail(c, errcode.DBError.Wrap(err))
		return
	}
	out := make([]*dto.AdminGenerationLogResp, 0, len(rows))
	for _, r := range rows {
		item := &dto.AdminGenerationLogResp{
			TaskID:     r.TaskID,
			CreatedAt:  r.CreatedAt.Unix(),
			UserID:     r.UserID,
			UserLabel:  r.UserLabel,
			Kind:       r.Kind,
			ModelCode:  r.ModelCode,
			Prompt:     r.Prompt,
			Status:     r.Status,
			CostPoints: r.CostPoints,
		}
		if r.APIKeyID != nil {
			item.APIKeyID = *r.APIKeyID
		}
		if r.KeyLabel != nil {
			item.KeyLabel = *r.KeyLabel
		}
		if r.DurationMs != nil {
			item.DurationMs = *r.DurationMs
		}
		if r.PreviewURL != nil && *r.PreviewURL != "" {
			item.PreviewURL = fmt.Sprintf("/admin/api/v1/logs/generations/%s/preview", r.TaskID)
		}
		if r.Error != nil {
			item.Error = *r.Error
		}
		if snapshot := modelGatewayRouteSnapshotFromParams(r.Params); snapshot != nil {
			item.ModelGatewayRouteSnapshot = snapshot
		}
		if snapshot := pricingSnapshotFromParams(r.Params); snapshot != nil {
			item.PricingSnapshot = snapshot
		}
		out = append(out, item)
	}
	page, pageSize := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	response.Page(c, out, total, page, pageSize)
}

func (h *AdminLogHandler) ModelGatewayAudit(c *gin.Context) {
	var req dto.ModelGatewayAuditListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	rows, total, err := h.gen.ListModelGatewayAudit(c.Request.Context(), repo.AdminModelGatewayAuditFilter{
		Keyword:       req.Keyword,
		Kind:          req.Kind,
		ModelCode:     req.ModelCode,
		SourceCode:    req.SourceCode,
		SkipReason:    req.SkipReason,
		PricingSource: req.PricingSource,
		Settlement:    req.Settlement,
		AuditType:     req.AuditType,
		Status:        req.Status,
		Page:          req.Page,
		PageSize:      req.PageSize,
	})
	if err != nil {
		response.Fail(c, errcode.DBError.Wrap(err))
		return
	}
	out := make([]*dto.ModelGatewayAuditResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, modelGatewayAuditRespFromRow(r))
	}
	page, pageSize := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	response.Page(c, out, total, page, pageSize)
}

func modelGatewayRouteSnapshotFromParams(raw *string) any {
	return paramsObjectValue(raw, adminLogRouteSnapshotKey)
}

func pricingSnapshotFromParams(raw *string) any {
	return paramsObjectValue(raw, adminLogPricingSnapshotKey)
}

func outputSnapshotFromParams(raw *string) any {
	return paramsObjectValue(raw, adminLogOutputSnapshotKey)
}

func videoJobSnapshotFromParams(raw *string) any {
	return paramsObjectValue(raw, adminLogVideoJobKey)
}

func paramsObjectValue(raw *string, key string) any {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(*raw), &params); err != nil {
		return nil
	}
	snapshot, ok := params[key]
	if !ok {
		return nil
	}
	return snapshot
}

func modelGatewayAuditRespFromRow(r *repo.AdminGenerationLogRow) *dto.ModelGatewayAuditResp {
	item := &dto.ModelGatewayAuditResp{
		TaskID:     r.TaskID,
		CreatedAt:  r.CreatedAt.Unix(),
		UserID:     r.UserID,
		UserLabel:  r.UserLabel,
		Kind:       r.Kind,
		ModelCode:  r.ModelCode,
		Status:     r.Status,
		CostPoints: r.CostPoints,
	}
	if r.DurationMs != nil {
		item.DurationMs = *r.DurationMs
	}
	if r.PreviewURL != nil && *r.PreviewURL != "" {
		item.PreviewURL = fmt.Sprintf("/admin/api/v1/logs/generations/%s/preview", r.TaskID)
	}

	routeSnapshot := modelGatewayRouteSnapshotFromParams(r.Params)
	if routeSnapshot != nil {
		item.ModelGatewayRouteSnapshot = routeSnapshot
		fillModelGatewayAuditRouteFields(item, routeSnapshot)
	}
	pricingSnapshot := pricingSnapshotFromParams(r.Params)
	if pricingSnapshot != nil {
		item.PricingSnapshot = pricingSnapshot
		fillModelGatewayAuditPricingFields(item, pricingSnapshot)
	}
	if outputSnapshot := outputSnapshotFromParams(r.Params); outputSnapshot != nil {
		item.OutputSnapshot = outputSnapshot
	}
	if videoJobSnapshot := videoJobSnapshotFromParams(r.Params); videoJobSnapshot != nil {
		item.VideoJobSnapshot = videoJobSnapshot
	}
	return item
}

func fillModelGatewayAuditRouteFields(item *dto.ModelGatewayAuditResp, snapshot any) {
	m := auditMap(snapshot)
	if m == nil {
		return
	}
	if n, ok := auditInt(m["selected_index"]); ok {
		item.SelectedIndex = &n
	}
	if n, ok := auditInt(m["candidate_count"]); ok {
		item.CandidateCount = n
	}
	if n, ok := auditInt(m["skipped_count"]); ok {
		item.SkippedCount = n
	}
	if item.CandidateCount == 0 {
		item.CandidateCount = len(auditMapSlice(m["candidates"]))
	}
	if item.SkippedCount == 0 {
		item.SkippedCount = len(auditMapSlice(m["skipped_candidates"]))
	}
	selected := selectedAuditCandidate(m)
	if selected != nil {
		item.SelectedSourceType = auditString(selected["source_type"])
		item.SelectedSourceCode = auditString(selected["source_code"])
		item.SelectedSourceName = auditString(selected["source_name"])
		item.SelectedProvider = auditString(selected["provider"])
		item.SelectedAdapter = auditString(selected["adapter"])
		item.SelectedUpstreamModel = auditString(selected["upstream_model"])
	}
	item.SkipReasons = auditSkipReasons(m)
}

func fillModelGatewayAuditPricingFields(item *dto.ModelGatewayAuditResp, snapshot any) {
	m := auditMap(snapshot)
	if m == nil {
		return
	}
	item.PricingSource = auditString(m["pricing_source"])
	item.PricingMode = auditString(m["pricing_mode"])
	item.Settlement = auditString(m["settlement"])
	item.PreDeductPoints = auditInt64(m["pre_deduct_points"])
	item.ActualPoints = auditInt64(m["actual_points"])
	item.RefundPoints = auditInt64(m["refund_points"])
	item.ExtraPoints = auditInt64(m["extra_points"])
	if item.ActualPoints == 0 {
		item.ActualPoints = auditInt64(m["estimated_total_points"])
	}
	if item.ActualPoints == 0 {
		item.ActualPoints = auditInt64(m["estimated_points"])
	}
}

func selectedAuditCandidate(route map[string]any) map[string]any {
	selectedIndex, ok := auditInt(route["selected_index"])
	if !ok {
		return nil
	}
	for _, c := range auditMapSlice(route["candidates"]) {
		idx, ok := auditInt(c["index"])
		if ok && idx == selectedIndex {
			return c
		}
	}
	candidates := auditMapSlice(route["candidates"])
	if selectedIndex > 0 && selectedIndex <= len(candidates) {
		return candidates[selectedIndex-1]
	}
	if selectedIndex >= 0 && selectedIndex < len(candidates) {
		return candidates[selectedIndex]
	}
	return nil
}

func auditSkipReasons(route map[string]any) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, c := range auditMapSlice(route["skipped_candidates"]) {
		reason := strings.TrimSpace(auditString(c["skip_reason"]))
		if reason == "" || seen[reason] {
			continue
		}
		seen[reason] = true
		out = append(out, reason)
	}
	return out
}

func auditMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func auditMapSlice(v any) []map[string]any {
	switch arr := v.(type) {
	case []map[string]any:
		return arr
	case []any:
		out := make([]map[string]any, 0, len(arr))
		for _, item := range arr {
			if m := auditMap(item); m != nil {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func auditString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(s))
	}
}

func auditInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func auditInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func (h *AdminLogHandler) GenerationUpstreamLogs(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		response.Fail(c, errcode.InvalidParam.WithMsg("empty task_id"))
		return
	}
	rows, err := h.gen.ListUpstreamLogs(c.Request.Context(), taskID)
	if err != nil {
		response.Fail(c, errcode.DBError.Wrap(err))
		return
	}
	response.OK(c, upstreamLogRows(rows))
}

func (h *AdminLogHandler) GenerationBillingProof(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		response.Fail(c, errcode.InvalidParam.WithMsg("empty task_id"))
		return
	}
	if h.wallet == nil {
		response.Fail(c, errcode.ResourceMissing)
		return
	}
	proof, err := h.wallet.GetTaskBillingProof(c.Request.Context(), taskID)
	if err != nil {
		response.Fail(c, errcode.DBError.Wrap(err))
		return
	}
	response.OK(c, billingProofResp(taskID, proof))
}

func (h *AdminLogHandler) UpstreamFailures(c *gin.Context) {
	var req dto.AdminUpstreamFailureListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	rows, total, err := h.gen.ListUpstreamFailures(c.Request.Context(), repo.UpstreamFailureLogFilter{
		Keyword:    req.Keyword,
		Provider:   req.Provider,
		AccountID:  req.AccountID,
		Stage:      req.Stage,
		StatusCode: req.StatusCode,
		SinceHours: req.SinceHours,
		Page:       req.Page,
		PageSize:   req.PageSize,
	})
	if err != nil {
		response.Fail(c, errcode.DBError.Wrap(err))
		return
	}
	page, pageSize := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	response.Page(c, upstreamLogRows(rows), total, page, pageSize)
}

func upstreamLogRows(rows []*repo.AdminGenerationUpstreamLogRow) []*dto.AdminGenerationUpstreamLogResp {
	out := make([]*dto.AdminGenerationUpstreamLogResp, 0, len(rows))
	for _, r := range rows {
		item := &dto.AdminGenerationUpstreamLogResp{
			ID:         r.ID,
			TaskID:     r.TaskID,
			Provider:   r.Provider,
			AccountID:  r.AccountID,
			Stage:      r.Stage,
			StatusCode: r.StatusCode,
			DurationMs: r.DurationMs,
			CreatedAt:  r.CreatedAt.Unix(),
		}
		if r.Kind != nil {
			item.Kind = *r.Kind
		}
		if r.ModelCode != nil {
			item.ModelCode = *r.ModelCode
		}
		if r.Method != nil {
			item.Method = *r.Method
		}
		if r.URL != nil {
			item.URL = *r.URL
		}
		if r.RequestExcerpt != nil {
			item.RequestExcerpt = *r.RequestExcerpt
		}
		if r.ResponseExcerpt != nil {
			item.ResponseExcerpt = *r.ResponseExcerpt
		}
		if r.Error != nil {
			item.Error = *r.Error
		}
		if r.Meta != nil {
			item.Meta = *r.Meta
		}
		out = append(out, item)
	}
	return out
}

func billingProofResp(taskID string, proof *repo.TaskBillingProof) *dto.AdminGenerationBillingProofResp {
	out := &dto.AdminGenerationBillingProofResp{
		TaskID:     taskID,
		WalletLogs: []*dto.AdminGenerationBillingWalletResp{},
		Refunds:    []*dto.AdminGenerationRefundRecordResp{},
	}
	if proof == nil {
		return out
	}
	if rec := proof.ConsumeRecord; rec != nil {
		out.Consume = &dto.AdminGenerationConsumeRecordResp{
			ID:          rec.ID,
			TaskID:      rec.TaskID,
			UserID:      rec.UserID,
			Kind:        rec.Kind,
			ModelCode:   rec.ModelCode,
			Count:       rec.Count,
			UnitPoints:  rec.UnitPoints,
			TotalPoints: rec.TotalPoints,
			Status:      rec.Status,
			AccountID:   rec.AccountID,
			CreatedAt:   rec.CreatedAt.Unix(),
			UpdatedAt:   rec.UpdatedAt.Unix(),
		}
		out.Summary.ConsumeRecordFound = true
		out.Summary.ConsumeStatus = rec.Status
		out.Summary.ConsumeTotalPoints = rec.TotalPoints
	}
	for _, log := range proof.WalletLogs {
		if log == nil {
			continue
		}
		item := &dto.AdminGenerationBillingWalletResp{
			ID:           log.ID,
			UserID:       log.UserID,
			Direction:    log.Direction,
			BizType:      log.BizType,
			BizID:        log.BizID,
			Points:       log.Points,
			PointsBefore: log.PointsBefore,
			PointsAfter:  log.PointsAfter,
			CreatedAt:    log.CreatedAt.Unix(),
		}
		if log.Remark != nil {
			item.Remark = *log.Remark
		}
		out.WalletLogs = append(out.WalletLogs, item)
		out.Summary.WalletNetPoints += log.Points
		if log.BizType == model.BizRefund && log.Points > 0 {
			out.Summary.WalletRefundPoints += log.Points
		}
		if log.BizType == model.BizConsume && strings.HasSuffix(log.BizID, ":extra") && log.Points < 0 {
			out.Summary.WalletExtraPoints += -log.Points
		}
	}
	for _, refund := range proof.RefundRecords {
		if refund == nil {
			continue
		}
		out.Refunds = append(out.Refunds, &dto.AdminGenerationRefundRecordResp{
			ID:        refund.ID,
			TaskID:    refund.TaskID,
			UserID:    refund.UserID,
			Points:    refund.Points,
			Reason:    refund.Reason,
			Operator:  refund.Operator,
			CreatedAt: refund.CreatedAt.Unix(),
		})
	}
	out.Summary.WalletLogCount = len(out.WalletLogs)
	out.Summary.RefundRecordCount = len(out.Refunds)
	if out.Summary.WalletNetPoints < 0 {
		out.Summary.WalletSpendPoints = -out.Summary.WalletNetPoints
	}
	return out
}

// GenerationPreview proxies a request-log preview through the admin origin.
func (h *AdminLogHandler) GenerationPreview(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	t, err := h.gen.GetByTaskID(c.Request.Context(), taskID)
	if err != nil {
		response.Fail(c, errcode.ResourceMissing)
		return
	}
	results, err := h.gen.ListResultsByTask(c.Request.Context(), taskID)
	if err != nil || len(results) == 0 {
		response.Fail(c, errcode.ResourceMissing)
		return
	}
	r := results[0]
	rawURL := r.URL
	if t.Kind != "video" && r.ThumbURL != nil && *r.ThumbURL != "" {
		rawURL = *r.ThumbURL
	}
	if t.Kind == "video" && c.Query("thumb") == "1" && r.ThumbURL != nil && *r.ThumbURL != "" {
		rawURL = *r.ThumbURL
	}
	h.servePreviewURL(c, t, rawURL)
}

func (h *AdminLogHandler) servePreviewURL(c *gin.Context, t *model.GenerationTask, rawURL string) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		response.Fail(c, errcode.ResourceMissing)
		return
	}
	if strings.HasPrefix(rawURL, "/api/v1/gen/cached/") {
		serveAdminCachedAsset(c, strings.TrimPrefix(rawURL, "/api/v1/gen/cached/"))
		return
	}
	if strings.HasPrefix(rawURL, "/admin/api/v1/") {
		response.Fail(c, errcode.ResourceMissing)
		return
	}
	target := adminNormalizeGrokAssetURL(rawURL)
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		if !strings.Contains(target, "assets.grok.com") {
			c.Redirect(http.StatusFound, target)
			return
		}
		cookie, err := h.grokCookieForTask(c.Request.Context(), t)
		if err != nil {
			response.Fail(c, errcode.GPTUnavailable.WithMsg("资源下载凭证不可用"))
			return
		}
		proxyRemoteAsset(c, target, cookie, rawURL)
		return
	}
	response.Fail(c, errcode.ResourceMissing)
}

func serveAdminCachedAsset(c *gin.Context, rel string) {
	rel = strings.TrimLeft(rel, "/")
	if !isAdminPublicCachedAssetPath(rel) {
		response.Fail(c, errcode.InvalidParam.WithMsg("invalid asset path"))
		return
	}
	root := strings.TrimSpace(os.Getenv("KLEIN_STORAGE_ROOT"))
	if root == "" {
		root = "/app/storage/public"
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.File(filepath.Join(root, filepath.FromSlash(rel)))
}

func isAdminPublicCachedAssetPath(rel string) bool {
	if rel == "" || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
		return false
	}
	return strings.HasPrefix(rel, "generated/") || strings.HasPrefix(rel, "prompt-gallery/")
}

func proxyRemoteAsset(c *gin.Context, target, cookie, rawURL string) {
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target, nil)
	if err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://grok.com/")
	req.Header.Set("Accept", "*/*")
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		response.Fail(c, errcode.GPTUnavailable.Wrap(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		response.Fail(c, errcode.GPTUnavailable.WithMsg(fmt.Sprintf("资源下载失败 HTTP %d", resp.StatusCode)))
		return
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, adminAssetName(rawURL)))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, resp.Body)
}

func adminNormalizeGrokAssetURL(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "data:") {
		return v
	}
	return "https://assets.grok.com/" + strings.TrimLeft(v, "/")
}

func (h *AdminLogHandler) grokCookieForTask(ctx context.Context, t *model.GenerationTask) (string, error) {
	if t.AccountID == nil || h.acc == nil || h.aes == nil {
		return "", fmt.Errorf("missing account")
	}
	acc, err := h.acc.GetByID(ctx, *t.AccountID)
	if err != nil {
		return "", err
	}
	plain, err := h.aes.Decrypt(acc.CredentialEnc)
	if err != nil {
		return "", err
	}
	cred := strings.TrimSpace(string(plain))
	if strings.Contains(cred, "=") {
		if !strings.Contains(cred, "sso-rw=") {
			if token := adminCookieValue(cred, "sso"); token != "" {
				cred = strings.TrimRight(cred, "; ") + "; sso-rw=" + token
			}
		}
		return cred, nil
	}
	return "sso=" + cred + "; sso-rw=" + cred, nil
}

func adminCookieValue(cookie, name string) string {
	prefix := name + "="
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, prefix) {
			return strings.TrimPrefix(part, prefix)
		}
	}
	return ""
}

func adminAssetName(rawURL string) string {
	name := "asset"
	if i := strings.LastIndex(rawURL, "/"); i >= 0 && i+1 < len(rawURL) {
		name = rawURL[i+1:]
	}
	name = strings.TrimSpace(strings.Split(name, "?")[0])
	if name == "" {
		return "asset"
	}
	return name
}

func (h *AdminLogHandler) PurgeGenerationLogs(c *gin.Context) {
	var req dto.AdminGenerationLogPurgeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	before := time.Now().UTC().AddDate(0, 0, -req.Days)
	deleted, err := h.gen.SoftDeleteAdminLogsBefore(c.Request.Context(), before)
	if err != nil {
		response.Fail(c, errcode.DBError.Wrap(err))
		return
	}
	response.OK(c, &dto.AdminGenerationLogPurgeResp{Deleted: deleted})
}
