package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/service"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/response"
)

// AdminProviderRouteHandler exposes dry-run provider routing tools.
type AdminProviderRouteHandler struct {
	svc *service.ProviderRouteTestService
}

func NewAdminProviderRouteHandler(svc *service.ProviderRouteTestService) *AdminProviderRouteHandler {
	return &AdminProviderRouteHandler{svc: svc}
}

// Health GET /admin/api/v1/provider-routes/health
func (h *AdminProviderRouteHandler) Health(c *gin.Context) {
	resp, err := h.svc.Health(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, resp)
}

// Test POST /admin/api/v1/provider-routes/test
func (h *AdminProviderRouteHandler) Test(c *gin.Context) {
	var req dto.ProviderRouteTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	resp, err := h.svc.Test(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, resp)
}
