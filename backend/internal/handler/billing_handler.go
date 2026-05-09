// Package handler 用户端计费 handler。
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/middleware"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/service"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/response"
)

// BillingHandler 用户端计费 handler。
type BillingHandler struct {
	billing  *service.BillingService
	cdk      *service.CDKService
	recharge *service.RechargeService
}

// NewBillingHandler 构造。
func NewBillingHandler(b *service.BillingService, cdk *service.CDKService, recharge *service.RechargeService) *BillingHandler {
	return &BillingHandler{billing: b, cdk: cdk, recharge: recharge}
}

// Logs GET /api/v1/billing/logs?page=&page_size=
func (h *BillingHandler) Logs(c *gin.Context) {
	uid := middleware.MustUID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	logs, total, err := h.billing.ListWalletLogs(c.Request.Context(), uid, page, pageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]*dto.WalletLogResp, 0, len(logs))
	for _, l := range logs {
		r := &dto.WalletLogResp{
			ID:           l.ID,
			Direction:    l.Direction,
			BizType:      l.BizType,
			BizID:        l.BizID,
			Points:       l.Points,
			PointsBefore: l.PointsBefore,
			PointsAfter:  l.PointsAfter,
			CreatedAt:    l.CreatedAt.Unix(),
		}
		if l.Remark != nil {
			r.Remark = *l.Remark
		}
		out = append(out, r)
	}
	response.Page(c, out, total, page, pageSize)
}

// RedeemCDK POST /api/v1/billing/cdk/redeem
func (h *BillingHandler) RedeemCDK(c *gin.Context) {
	var req dto.CDKRedeemReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	uid := middleware.MustUID(c)
	pts, err := h.cdk.Redeem(c.Request.Context(), uid, req.Code)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"points":  pts,
		"biz":     model.BizCDK,
		"message": "兑换成功",
	})
}

// RechargePackages GET /api/v1/billing/recharge/packages
func (h *BillingHandler) RechargePackages(c *gin.Context) {
	items, err := h.recharge.ListPackages(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"list": items})
}

// CreateRechargeOrder POST /api/v1/billing/recharge/orders
func (h *BillingHandler) CreateRechargeOrder(c *gin.Context) {
	var req dto.CreateRechargeOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	uid := middleware.MustUID(c)
	row, err := h.recharge.CreateOrder(c.Request.Context(), uid, &req, c.ClientIP(), c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, row)
}

// GetRechargeOrder GET /api/v1/billing/recharge/orders/:order_no
func (h *BillingHandler) GetRechargeOrder(c *gin.Context) {
	uid := middleware.MustUID(c)
	row, err := h.recharge.GetUserOrder(c.Request.Context(), uid, c.Param("order_no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, row)
}

// CancelRechargeOrder POST /api/v1/billing/recharge/orders/:order_no/cancel
func (h *BillingHandler) CancelRechargeOrder(c *gin.Context) {
	uid := middleware.MustUID(c)
	row, err := h.recharge.CancelUserOrder(c.Request.Context(), uid, c.Param("order_no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, row)
}

// RechargeOrders GET /api/v1/billing/recharge/orders
func (h *BillingHandler) RechargeOrders(c *gin.Context) {
	uid := middleware.MustUID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	rows, total, err := h.recharge.ListUserOrders(c.Request.Context(), uid, page, pageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, rows, total, page, pageSize)
}

// AlipayNotify POST /api/v1/billing/recharge/alipay/notify
func (h *BillingHandler) AlipayNotify(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		c.String(200, "fail")
		return
	}
	text, err := h.recharge.HandleAlipayNotify(c.Request.Context(), c.Request.PostForm)
	if err != nil {
		c.String(200, "fail")
		return
	}
	c.String(200, text)
}
