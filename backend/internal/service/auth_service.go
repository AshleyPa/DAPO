// Package service 业务编排层。事务、幂等、跨 repo 协作发生在这里。
package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/jwtx"
	"github.com/kleinai/backend/pkg/logger"
)

// AuthService 用户认证。
type AuthService struct {
	db       *gorm.DB
	user     *repo.UserRepo
	jwt      *jwtx.Manager
	verifier *EmailVerificationService
	invite   *InviteRewardService
}

// NewAuthService 构造。
func NewAuthService(db *gorm.DB, userRepo *repo.UserRepo, jwt *jwtx.Manager, verifier *EmailVerificationService, invite *InviteRewardService) *AuthService {
	return &AuthService{db: db, user: userRepo, jwt: jwt, verifier: verifier, invite: invite}
}

var (
	emailRe = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)
	phoneRe = regexp.MustCompile(`^1[3-9]\d{9}$`)
)

// Register 用户注册（事务内创建用户 + 邀请关系 + 注册赠点流水可在后续扩展）。
func (s *AuthService) Register(ctx context.Context, req *dto.RegisterReq, ip string) (*model.User, *dto.TokenPair, error) {
	account := strings.ToLower(strings.TrimSpace(req.Account))
	if !emailRe.MatchString(account) {
		return nil, nil, errcode.InvalidParam.WithMsg("请使用邮箱注册")
	}
	if s.verifier == nil {
		return nil, nil, errcode.Internal.WithMsg("邮箱验证服务未初始化")
	}
	now := time.Now().UTC()

	user := &model.User{
		UUID:            uuid.NewString(),
		Status:          1,
		PlanCode:        "free",
		InviteCode:      genInviteCode(),
		RegisterIP:      &ip,
		EmailVerifiedAt: &now,
	}
	user.Email = &account
	if username := defaultUsername(account); username != "" {
		user.Username = &username
	}

	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		return nil, nil, errcode.Internal.Wrap(err)
	}
	user.Password = hash

	if req.InviteCode != "" {
		if inv, err := s.user.GetByInviteCode(ctx, req.InviteCode); err == nil && inv != nil {
			user.InviterID = &inv.ID
		}
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.verifier.ConsumeTx(ctx, tx, account, model.EmailVerificationSceneRegister, req.Code); err != nil {
			return err
		}
		if err := tx.Create(user).Error; err != nil {
			return wrapDup(err)
		}
		if user.InviterID != nil {
			if err := tx.Exec(
				"INSERT IGNORE INTO user_invite_relation (user_id, inviter_id, invite_code) VALUES (?, ?, ?)",
				user.ID, *user.InviterID, req.InviteCode,
			).Error; err != nil {
				return err
			}
		}
		if err := s.invite.GrantRegistrationBonusesTx(ctx, tx, user.ID, user.InviterID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if e, ok := errcode.As(err); ok && e.Code == errcode.UserExists.Code {
			return nil, nil, errcode.InvalidParam.WithMsg("注册失败，请检查邮箱或验证码后重试")
		}
		return nil, nil, err
	}

	tok, err := s.issue(user)
	if err != nil {
		return nil, nil, err
	}
	logger.FromCtx(ctx).Info("auth.register", zap.Uint64("uid", user.ID), zap.String("ip", ip))
	return user, tok, nil
}

// Login 登录。
func (s *AuthService) Login(ctx context.Context, req *dto.LoginReq, ip string) (*model.User, *dto.TokenPair, error) {
	account := strings.TrimSpace(req.Account)
	if emailRe.MatchString(account) {
		account = strings.ToLower(account)
	}
	u, err := s.user.GetByAccount(ctx, account)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, nil, errcode.Unauthorized.WithMsg("账号或密码错误")
		}
		return nil, nil, errcode.DBError.Wrap(err)
	}
	if !u.IsActive() {
		return nil, nil, errcode.Forbidden.WithMsg("账号已停用")
	}
	if !crypto.VerifyPassword(u.Password, req.Password) {
		return nil, nil, errcode.Unauthorized.WithMsg("账号或密码错误")
	}
	if err := s.user.UpdateLogin(ctx, u.ID, ip); err != nil {
		logger.FromCtx(ctx).Warn("update login failed", zap.Error(err))
	}

	tok, err := s.issue(u)
	if err != nil {
		return nil, nil, err
	}
	return u, tok, nil
}

// Refresh 用 refresh token 换新 access token。
func (s *AuthService) Refresh(ctx context.Context, refresh string) (*dto.TokenPair, error) {
	cl, err := s.jwt.ParseRefresh(refresh)
	if err != nil {
		return nil, errcode.TokenExpired.Wrap(err)
	}
	u, err := s.user.GetByID(ctx, cl.UID)
	if err != nil {
		return nil, errcode.UserNotFound
	}
	if !u.IsActive() {
		return nil, errcode.Forbidden
	}
	if cl.TokenVersion != u.TokenVersion {
		return nil, errcode.TokenInvalid
	}
	return s.issue(u)
}

// ChangePassword 修改密码。
func (s *AuthService) ChangePassword(ctx context.Context, uid uint64, req *dto.ChangePasswordReq) error {
	u, err := s.user.GetByID(ctx, uid)
	if err != nil {
		return errcode.UserNotFound
	}
	if !crypto.VerifyPassword(u.Password, req.OldPassword) {
		return errcode.Unauthorized.WithMsg("原密码不正确")
	}
	hash, err := crypto.HashPassword(req.NewPassword)
	if err != nil {
		return errcode.Internal.Wrap(err)
	}
	return s.user.UpdatePassword(ctx, uid, hash)
}

// SendEmailCode 发送注册/找回密码邮箱验证码。
func (s *AuthService) SendEmailCode(ctx context.Context, req *dto.SendEmailCodeReq, ip string) error {
	if s.verifier == nil {
		return errcode.Internal.WithMsg("邮箱验证服务未初始化")
	}
	return s.verifier.SendCode(ctx, req, ip)
}

// ResetPassword 通过邮箱验证码重置密码。
func (s *AuthService) ResetPassword(ctx context.Context, req *dto.ResetPasswordReq) error {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !emailRe.MatchString(email) {
		return errcode.InvalidParam.WithMsg("请输入有效邮箱")
	}
	if s.verifier == nil {
		return errcode.Internal.WithMsg("邮箱验证服务未初始化")
	}
	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		return errcode.Internal.Wrap(err)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.verifier.ConsumeTx(ctx, tx, email, model.EmailVerificationSceneResetPassword, req.Code); err != nil {
			return err
		}
		var u model.User
		if err := tx.Where("email = ? AND deleted_at IS NULL", email).First(&u).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errcode.InvalidParam.WithMsg("重置失败，请检查邮箱或验证码后重试")
			}
			return errcode.DBError.Wrap(err)
		}
		if !u.IsActive() {
			return errcode.InvalidParam.WithMsg("重置失败，请检查邮箱或验证码后重试")
		}
		if err := tx.Model(&model.User{}).
			Where("id = ?", u.ID).
			Updates(map[string]any{
				"password":      hash,
				"token_version": gorm.Expr("token_version + 1"),
			}).Error; err != nil {
			return errcode.DBError.Wrap(err)
		}
		return nil
	})
}

// Logout invalidates all currently issued user tokens for the account.
func (s *AuthService) Logout(ctx context.Context, uid uint64) error {
	if err := s.user.IncrementTokenVersion(ctx, uid); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

// issue 颁发 access + refresh。
func (s *AuthService) issue(u *model.User) (*dto.TokenPair, error) {
	jti := uuid.NewString()
	access, accExp, err := s.jwt.IssueAccess(u.ID, jwtx.SubjectUser, jti, []string{u.PlanCode}, u.TokenVersion)
	if err != nil {
		return nil, errcode.Internal.Wrap(err)
	}
	refresh, refExp, err := s.jwt.IssueRefresh(u.ID, jwtx.SubjectUser, jti, u.TokenVersion)
	if err != nil {
		return nil, errcode.Internal.Wrap(err)
	}
	now := time.Now()
	return &dto.TokenPair{
		AccessToken:     access,
		RefreshToken:    refresh,
		TokenType:       "Bearer",
		AccessExpireIn:  int64(accExp.Sub(now).Seconds()),
		RefreshExpireIn: int64(refExp.Sub(now).Seconds()),
	}, nil
}

// === helpers ===

// genInviteCode 8 位邀请码：K + 7 位大写 base32（无 0/1/I/O）。
func genInviteCode() string {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // 32 char
	b, _ := crypto.RandomBytes(8)
	out := make([]byte, 8)
	out[0] = 'K'
	for i := 1; i < 8; i++ {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out)
}

func defaultUsername(email string) string {
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return ""
}

// wrapDup 把 MySQL 唯一索引冲突映射成 UserExists。
func wrapDup(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "Error 1062") || strings.Contains(msg, "Duplicate entry") {
		return errcode.UserExists.Wrap(err)
	}
	return err
}
