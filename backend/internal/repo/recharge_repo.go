package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/model"
)

type RechargeRepo struct{ db *gorm.DB }

func NewRechargeRepo(db *gorm.DB) *RechargeRepo { return &RechargeRepo{db: db} }

func (r *RechargeRepo) Create(ctx context.Context, row *model.RechargeRecord) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *RechargeRepo) GetByOrderNo(ctx context.Context, orderNo string) (*model.RechargeRecord, error) {
	var row model.RechargeRecord
	err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&row).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &row, nil
}

func (r *RechargeRepo) GetUserOrder(ctx context.Context, userID uint64, orderNo string) (*model.RechargeRecord, error) {
	var row model.RechargeRecord
	err := r.db.WithContext(ctx).Where("user_id = ? AND order_no = ?", userID, orderNo).First(&row).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &row, nil
}

func (r *RechargeRepo) GetByIdem(ctx context.Context, userID uint64, idemKey string) (*model.RechargeRecord, error) {
	var row model.RechargeRecord
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND idem_key = ?", userID, idemKey).
		First(&row).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &row, nil
}

func (r *RechargeRepo) ListUserOrders(ctx context.Context, userID uint64, page, pageSize int) ([]*model.RechargeRecord, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.RechargeRecord{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*model.RechargeRecord
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

func (r *RechargeRepo) ListPendingByChannelBefore(ctx context.Context, channel string, before time.Time, limit int) ([]*model.RechargeRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []*model.RechargeRecord
	err := r.db.WithContext(ctx).
		Where("channel = ? AND status = ? AND created_at <= ?", channel, model.RechargeStatusPending, before).
		Order("created_at ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r *RechargeRepo) Update(ctx context.Context, id uint64, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.RechargeRecord{}).Where("id = ?", id).Updates(fields).Error
}

func (r *RechargeRepo) UpdateIfStatus(ctx context.Context, id uint64, status int8, fields map[string]any) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.RechargeRecord{}).
		Where("id = ? AND status = ?", id, status).
		Updates(fields)
	return res.RowsAffected, res.Error
}
