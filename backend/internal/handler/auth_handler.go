// Package handler HTTP 入参解析、调 service、出参响应。禁止访问 db。
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/middleware"
	"github.com/kleinai/backend/internal/service"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/response"
)

// AuthHandler 用户端 auth handler。
type AuthHandler struct {
	auth *service.AuthService
	user *service.UserService
}

// NewAuthHandler 构造。
func NewAuthHandler(a *service.AuthService, u *service.UserService) *AuthHandler {
	return &AuthHandler{auth: a, user: u}
}

// Register POST /api/v1/auth/register
func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	u, tok, err := h.auth.Register(c.Request.Context(), &req, c.ClientIP())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"uid":         u.ID,
		"uuid":        u.UUID,
		"invite_code": u.InviteCode,
		"token":       tok,
	})
}

// Login POST /api/v1/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	u, tok, err := h.auth.Login(c.Request.Context(), &req, c.ClientIP())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"uid":   u.ID,
		"uuid":  u.UUID,
		"token": tok,
	})
}

// SendEmailCode POST /api/v1/auth/email/code
func (h *AuthHandler) SendEmailCode(c *gin.Context) {
	var req dto.SendEmailCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	if err := h.auth.SendEmailCode(c.Request.Context(), &req, c.ClientIP()); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "验证码已发送"})
}

// ResetPassword POST /api/v1/auth/password/reset
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req dto.ResetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	if err := h.auth.ResetPassword(c.Request.Context(), &req); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "密码已重置"})
}

// Refresh POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req dto.RefreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	tok, err := h.auth.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tok)
}

// Logout POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	uid := middleware.MustUID(c)
	if err := h.auth.Logout(c.Request.Context(), uid); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Me GET /api/v1/users/me
func (h *AuthHandler) Me(c *gin.Context) {
	uid := middleware.MustUID(c)
	me, err := h.user.Me(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, me)
}

// ChangePassword POST /api/v1/users/password
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req dto.ChangePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.Wrap(err))
		return
	}
	uid := middleware.MustUID(c)
	if err := h.auth.ChangePassword(c.Request.Context(), uid, &req); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
