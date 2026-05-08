package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kleinai/backend/internal/model"
)

type EmailVerificationRepo struct{ db *gorm.DB }

func NewEmailVerificationRepo(db *gorm.DB) *EmailVerificationRepo {
	return &EmailVerificationRepo{db: db}
}

func (r *EmailVerificationRepo) Create(ctx context.Context, row *model.EmailVerificationCode) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *EmailVerificationRepo) ExpirePending(ctx context.Context, email, scene string, now time.Time) error {
	return r.db.WithContext(ctx).Model(&model.EmailVerificationCode{}).
		Where("email = ? AND scene = ? AND status = ? AND expires_at <= ?", email, scene, model.EmailVerificationStatusPending, now).
		Update("status", model.EmailVerificationStatusExpired).Error
}

func (r *EmailVerificationRepo) CountSince(ctx context.Context, email, scene string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailVerificationCode{}).
		Where("email = ? AND scene = ? AND created_at >= ?", email, scene, since).
		Count(&n).Error
	return n, err
}

func (r *EmailVerificationRepo) LatestCreated(ctx context.Context, email, scene string) (*model.EmailVerificationCode, error) {
	var row model.EmailVerificationCode
	err := r.db.WithContext(ctx).
		Where("email = ? AND scene = ?", email, scene).
		Order("id DESC").
		First(&row).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &row, nil
}

func (r *EmailVerificationRepo) LatestPendingForUpdate(ctx context.Context, tx *gorm.DB, email, scene string, now time.Time) (*model.EmailVerificationCode, error) {
	var row model.EmailVerificationCode
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("email = ? AND scene = ? AND status = ? AND expires_at > ?", email, scene, model.EmailVerificationStatusPending, now).
		Order("id DESC").
		First(&row).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &row, nil
}

func (r *EmailVerificationRepo) IncrementAttempts(ctx context.Context, tx *gorm.DB, id uint64) error {
	return tx.WithContext(ctx).Model(&model.EmailVerificationCode{}).
		Where("id = ?", id).
		UpdateColumn("attempts", gorm.Expr("attempts + 1")).Error
}

func (r *EmailVerificationRepo) MarkUsed(ctx context.Context, tx *gorm.DB, id uint64, now time.Time) error {
	res := tx.WithContext(ctx).Model(&model.EmailVerificationCode{}).
		Where("id = ? AND status = ?", id, model.EmailVerificationStatusPending).
		Updates(map[string]any{
			"status":  model.EmailVerificationStatusUsed,
			"used_at": now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}
