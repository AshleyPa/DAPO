package service

import (
	"context"
	"crypto/rand"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/config"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/logger"
	"github.com/kleinai/backend/pkg/mailer"
)

const verificationTTL = 10 * time.Minute

type EmailVerificationService struct {
	codes    *repo.EmailVerificationRepo
	users    *repo.UserRepo
	baseSMTP config.SMTP
	sys      *SystemConfigService
}

func NewEmailVerificationService(codes *repo.EmailVerificationRepo, users *repo.UserRepo, smtpCfg config.SMTP, sys *SystemConfigService) *EmailVerificationService {
	return &EmailVerificationService{codes: codes, users: users, baseSMTP: smtpCfg, sys: sys}
}

func (s *EmailVerificationService) SendCode(ctx context.Context, req *dto.SendEmailCodeReq, ip string) error {
	email, scene, err := normalizeEmailScene(req.Email, req.Scene)
	if err != nil {
		return err
	}
	m := mailer.New(s.effectiveSMTP(ctx))
	if m.Disabled() {
		return errcode.Internal.WithMsg("邮件服务未配置")
	}
	if scene == model.EmailVerificationSceneRegister {
		if u, err := s.users.GetByEmail(ctx, email); err == nil && u != nil {
			return nil
		} else if err != nil && err != repo.ErrNotFound {
			return errcode.DBError.Wrap(err)
		}
	}
	if scene == model.EmailVerificationSceneResetPassword {
		if u, err := s.users.GetByEmail(ctx, email); err == nil {
			if u == nil || !u.IsActive() {
				return nil
			}
		} else if err == repo.ErrNotFound {
			return nil
		} else {
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
	if err := m.Send(ctx, mailer.Message{To: email, Subject: subject, HTML: html}); err != nil {
		if expireErr := s.codes.MarkExpired(ctx, row.ID, time.Now().UTC()); expireErr != nil {
			logger.FromCtx(ctx).Warn("email_verification.expire_after_send_failed_failed", zap.String("email", email), zap.String("scene", scene), zap.Error(expireErr))
		}
		logger.FromCtx(ctx).Warn("email_verification.send_failed", zap.String("email", email), zap.String("scene", scene), zap.Error(err))
		return errcode.Internal.WithMsg("验证码邮件发送失败")
	}
	return nil
}

func (s *EmailVerificationService) effectiveSMTP(ctx context.Context) config.SMTP {
	cfg := s.baseSMTP
	cfg.Host = s.smtpString(ctx, "KLEIN_SMTP_HOST", SettingSMTPHost, cfg.Host)
	cfg.Port = s.smtpInt(ctx, "KLEIN_SMTP_PORT", SettingSMTPPort, cfg.Port)
	cfg.Username = s.smtpString(ctx, "KLEIN_SMTP_USERNAME", SettingSMTPUsername, cfg.Username)
	cfg.Password = s.smtpString(ctx, "KLEIN_SMTP_PASSWORD", SettingSMTPPassword, cfg.Password)
	cfg.FromEmail = s.smtpString(ctx, "KLEIN_SMTP_FROM_EMAIL", SettingSMTPFromEmail, cfg.FromEmail)
	cfg.FromName = s.smtpString(ctx, "KLEIN_SMTP_FROM_NAME", SettingSMTPFromName, cfg.FromName)
	cfg.UseSSL = s.smtpBool(ctx, "KLEIN_SMTP_USE_SSL", SettingSMTPUseSSL, cfg.UseSSL)
	cfg.UseStartTLS = s.smtpBool(ctx, "KLEIN_SMTP_USE_STARTTLS", SettingSMTPUseStartTLS, cfg.UseStartTLS)
	return cfg
}

func (s *EmailVerificationService) smtpString(ctx context.Context, envKey, configKey, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return value
	}
	if s.sys != nil {
		return s.sys.GetString(ctx, configKey, fallback)
	}
	return fallback
}

func (s *EmailVerificationService) smtpInt(ctx context.Context, envKey, configKey string, fallback int) int {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	if s.sys != nil {
		return int(s.sys.GetInt(ctx, configKey, int64(fallback)))
	}
	return fallback
}

func (s *EmailVerificationService) smtpBool(ctx context.Context, envKey, configKey string, fallback bool) bool {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return envBool(value)
	}
	if s.sys != nil {
		return s.sys.GetBool(ctx, configKey, fallback)
	}
	return fallback
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
