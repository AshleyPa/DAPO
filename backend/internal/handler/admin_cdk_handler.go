// Package handler 管理后台 - CDK handler。
package handler

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/middleware"
	"github.com/kleinai/backend/internal/service"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/response"
)

// AdminCDKHandler 管理后台 CDK 批次 handler。
type AdminCDKHandler struct {
	svc *service.CDKService
}

// NewAdminCDKHandler 构造。
func NewAdminCDKHandler(svc *service.CDKService) *AdminCDKHandler {
	return &AdminCDKHandler{svc: svc}
}

// ListBatches GET /admin/api/v1/cdk/batches
func (h *AdminCDKHandler) ListBatches(c *gin.Context) {
	var req dto.CDKBatchListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	rows, total, err := h.svc.ListBatches(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	page, pageSize := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	response.Page(c, rows, total, page, pageSize)
}

// CreateBatch POST /admin/api/v1/cdk/batches
func (h *AdminCDKHandler) CreateBatch(c *gin.Context) {
	var req dto.CDKBatchCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	var expire *time.Time
	if req.ExpireAt > 0 {
		t := time.Unix(req.ExpireAt, 0).UTC()
		expire = &t
	}
	uid := middleware.UID(c)
	batch, err := h.svc.GenerateBatch(c.Request.Context(), uid, req.BatchNo, req.Name, req.Points, req.Qty, req.PerUserLimit, expire)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.CDKBatchCreateResp{ID: batch.ID, BatchNo: batch.BatchNo, TotalQty: batch.TotalQty})
}

// ListCodes GET /admin/api/v1/cdk/batches/:id/codes
func (h *AdminCDKHandler) ListCodes(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var req dto.CDKCodeListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	rows, total, err := h.svc.ListCodes(c.Request.Context(), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	page, pageSize := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 200
	}
	response.Page(c, rows, total, page, pageSize)
}
