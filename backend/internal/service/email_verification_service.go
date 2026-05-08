package service

import (
	"context"
	"crypto/rand"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/mailer"
)

const verificationTTL = 10 * time.Minute

type EmailVerificationService struct {
	codes  *repo.EmailVerificationRepo
	users  *repo.UserRepo
	mailer *mailer.Mailer
}

func NewEmailVerificationService(codes *repo.EmailVerificationRepo, users *repo.UserRepo, mailer *mailer.Mailer) *EmailVerificationService {
	return &EmailVerificationService{codes: codes, users: users, mailer: mailer}
}

func (s *EmailVerificationService) SendCode(ctx context.Context, req *dto.SendEmailCodeReq, ip string) error {
	email, scene, err := normalizeEmailScene(req.Email, req.Scene)
	if err != nil {
		return err
	}
	if s.mailer == nil || s.mailer.Disabled() {
		return errcode.Internal.WithMsg("邮件服务未配置")
	}
	if scene == model.EmailVerificationSceneRegister {
		if u, err := s.users.GetByEmail(ctx, email); err == nil && u != nil {
			return errcode.UserExists.WithMsg("该邮箱已注册")
		} else if err != nil && err != repo.ErrNotFound {
			return errcode.DBError.Wrap(err)
		}
	}

	now := time.Now().UTC()
	_ = s.codes.ExpirePending(ctx, email, scene, now)
	if latest, err := s.codes.LatestCreated(ctx, email, scene); err == nil && now.Sub(latest.CreatedAt) < time.Minute {
		return errcode.RateLimited.WithMsg("验证码发送过于频繁，请稍后再试")
	} else if err != nil && err != repo.ErrNotFound {
		return errcode.DBError.Wrap(err)
	}
	if n, err := s.codes.CountSince(ctx, email, scene, now.Add(-24*time.Hour)); err != nil {
		return errcode.DBError.Wrap(err)
	} else if n >= 10 {
		return errcode.RateLimited.WithMsg("今日验证码发送次数已达上限")
	}

	code, err := randomDigits(6)
	if err != nil {
		return errcode.Internal.Wrap(err)
	}
	salt, err := crypto.RandomString(16)
	if err != nil {
		return errcode.Internal.Wrap(err)
	}
	sendIP := strings.TrimSpace(ip)
	row := &model.EmailVerificationCode{
		Email:     email,
		Scene:     scene,
		CodeHash:  crypto.SHA256Salt(code, salt),
		Salt:      salt,
		Status:    model.EmailVerificationStatusPending,
		SendIP:    &sendIP,
		ExpiresAt: now.Add(verificationTTL),
	}
	if sendIP == "" {
		row.SendIP = nil
	}
	if err := s.codes.Create(ctx, row); err != nil {
		return errcode.DBError.Wrap(err)
	}
	subject, html := mailer.RenderVerificationCode(scene, code, int(verificationTTL/time.Minute))
	if err := s.mailer.Send(ctx, mailer.Message{To: email, Subject: subject, HTML: html}); err != nil {
		return errcode.Internal.WithMsg("验证码邮件发送失败")
	}
	return nil
}

func (s *EmailVerificationService) ConsumeTx(ctx context.Context, tx *gorm.DB, email, scene, code string) error {
	email, scene, err := normalizeEmailScene(email, scene)
	if err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return errcode.InvalidParam.WithMsg("验证码错误或已过期")
	}
	now := time.Now().UTC()
	row, err := s.codes.LatestPendingForUpdate(ctx, tx, email, scene, now)
	if err != nil {
		if err == repo.ErrNotFound {
			return errcode.InvalidParam.WithMsg("验证码错误或已过期")
		}
		return errcode.DBError.Wrap(err)
	}
	if row.Attempts >= 5 {
		return errcode.RateLimited.WithMsg("验证码错误次数过多，请重新获取")
	}
	if crypto.SHA256Salt(code, row.Salt) != row.CodeHash {
		_ = s.codes.IncrementAttempts(ctx, tx, row.ID)
		return errcode.InvalidParam.WithMsg("验证码错误或已过期")
	}
	if err := s.codes.MarkUsed(ctx, tx, row.ID, now); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func normalizeEmailScene(email, scene string) (string, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	scene = strings.TrimSpace(scene)
	if !emailRe.MatchString(email) {
		return "", "", errcode.InvalidParam.WithMsg("请输入有效邮箱")
	}
	switch scene {
	case model.EmailVerificationSceneRegister, model.EmailVerificationSceneResetPassword:
		return email, scene, nil
	default:
		return "", "", errcode.InvalidParam.WithMsg("验证码场景无效")
	}
}

func randomDigits(n int) (string, error) {
	out := make([]byte, n)
	for i := range out {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		out[i] = byte('0' + v.Int64())
	}
	return string(out), nil
}
