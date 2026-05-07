package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/middleware"
	"github.com/kleinai/backend/internal/service"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/response"
)

type PromptGalleryHandler struct {
	svc *service.PromptGalleryService
}

func NewPromptGalleryHandler(svc *service.PromptGalleryService) *PromptGalleryHandler {
	return &PromptGalleryHandler{svc: svc}
}

func (h *PromptGalleryHandler) PublicList(c *gin.Context) {
	var req dto.PublicPromptGalleryListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	rows, err := h.svc.ListPublic(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"list": rows})
}

func (h *PromptGalleryHandler) AdminList(c *gin.Context) {
	var req dto.PromptGalleryListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	rows, total, err := h.svc.ListAdmin(c.Request.Context(), &req)
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

func (h *PromptGalleryHandler) Create(c *gin.Context) {
	var req dto.PromptGalleryCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	row, err := h.svc.Create(c.Request.Context(), &req, middleware.MustUID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": row.ID})
}

func (h *PromptGalleryHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var req dto.PromptGalleryUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	if err := h.svc.Update(c.Request.Context(), id, &req, middleware.MustUID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *PromptGalleryHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *PromptGalleryHandler) Reorder(c *gin.Context) {
	var req dto.PromptGalleryReorderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	if err := h.svc.Reorder(c.Request.Context(), &req, middleware.MustUID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"updated": len(req.Items)})
}
