package handler

import (
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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

func (h *PromptGalleryHandler) SeedDefaults(c *gin.Context) {
	inserted, err := h.svc.SeedDefaults(c.Request.Context(), middleware.MustUID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"inserted": inserted})
}

func (h *PromptGalleryHandler) UploadCover(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Fail(c, errcode.InvalidParam.WithMsg("请选择封面图片"))
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > 8<<20 {
		response.Fail(c, errcode.InvalidParam.WithMsg("封面图片大小需在 8MB 以内"))
		return
	}
	head := make([]byte, 512)
	n, _ := file.Read(head)
	if seeker, ok := file.(io.Seeker); ok {
		_, _ = seeker.Seek(0, io.SeekStart)
	}
	mime := http.DetectContentType(head[:n])
	ext := coverExt(mime, header.Filename)
	if ext == "" {
		response.Fail(c, errcode.InvalidParam.WithMsg("仅支持 JPG/PNG/WebP/AVIF/GIF 图片"))
		return
	}
	root := strings.TrimSpace(os.Getenv("KLEIN_STORAGE_ROOT"))
	if root == "" {
		root = "/app/storage/public"
	}
	rel := path.Join("prompt-gallery", time.Now().Format("2006/01/02"), uuid.NewString()+ext)
	dst := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		response.Fail(c, errcode.Internal.Wrap(err))
		return
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		response.Fail(c, errcode.Internal.Wrap(err))
		return
	}
	written, err := io.Copy(out, io.LimitReader(file, 8<<20))
	closeErr := out.Close()
	if err != nil {
		_ = os.Remove(dst)
		response.Fail(c, errcode.Internal.Wrap(err))
		return
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		response.Fail(c, errcode.Internal.Wrap(closeErr))
		return
	}
	if written <= 0 {
		_ = os.Remove(dst)
		response.Fail(c, errcode.InvalidParam.WithMsg("封面图片为空"))
		return
	}
	response.OK(c, gin.H{"url": "/api/v1/gen/cached/" + rel})
}

func coverExt(mime, filename string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "image/gif":
		return ".gif"
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".png", ".webp", ".avif", ".gif":
		return strings.ToLower(filepath.Ext(filename))
	default:
		return ""
	}
}
