package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/model"
)

// ProxyRepo 代理仓储。
type ProxyRepo struct{ db *gorm.DB }

// NewProxyRepo 构造。
func NewProxyRepo(db *gorm.DB) *ProxyRepo { return &ProxyRepo{db: db} }

// Create 新增。
func (r *ProxyRepo) Create(ctx context.Context, p *model.Proxy) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// CreateMany 批量新增代理。
func (r *ProxyRepo) CreateMany(ctx context.Context, items []*model.Proxy) error {
	if len(items) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(items, 200).Error
}

// GetByID 主键查询（未软删）。
func (r *ProxyRepo) GetByID(ctx context.Context, id uint64) (*model.Proxy, error) {
	var p model.Proxy
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).First(&p).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &p, nil
}

// ProxyListFilter 列表过滤。
type ProxyListFilter struct {
	Status   *int8
	Keyword  string
	Page     int
	PageSize int
}

// List 分页列表。
func (r *ProxyRepo) List(ctx context.Context, f ProxyListFilter) ([]*model.Proxy, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 50
	}
	q := r.db.WithContext(ctx).Model(&model.Proxy{}).Where("deleted_at IS NULL")
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.Keyword != "" {
		k := "%" + f.Keyword + "%"
		q = q.Where("(name LIKE ? OR host LIKE ? OR remark LIKE ?)", k, k, k)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*model.Proxy
	if err := q.Order("id DESC").
		Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ListEnabled 取所有启用代理（用于下拉、调度）。
func (r *ProxyRepo) ListEnabled(ctx context.Context) ([]*model.Proxy, error) {
	var items []*model.Proxy
	err := r.db.WithContext(ctx).
		Where("status = ? AND deleted_at IS NULL", model.ProxyStatusEnabled).
		Order("id ASC").Find(&items).Error
	return items, err
}

// Update 部分字段更新。
func (r *ProxyRepo) Update(ctx context.Context, id uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Proxy{}).
		Where("id = ?", id).Updates(fields).Error
}

// SoftDelete 软删除。
func (r *ProxyRepo) SoftDelete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&model.Proxy{}).
		Where("id = ?", id).Update("deleted_at", time.Now().UTC()).Error
}

// SoftDeleteMany 按 ID 列表批量软删除。
func (r *ProxyRepo) SoftDeleteMany(ctx context.Context, ids []uint64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&model.Proxy{}).
		Where("id IN ? AND deleted_at IS NULL", ids).
		Update("deleted_at", now)
	return res.RowsAffected, res.Error
}

// SoftDeleteBySubscriptionID 删除某个订阅上次同步出的代理。
func (r *ProxyRepo) SoftDeleteBySubscriptionID(ctx context.Context, subscriptionID uint64) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Model(&model.Proxy{}).
		Where("subscription_id = ? AND deleted_at IS NULL", subscriptionID).
		Update("deleted_at", now).Error
}

// MarkCheck 记录探测结果。
func (r *ProxyRepo) MarkCheck(ctx context.Context, id uint64, ok bool, latencyMs int, errMsg string) error {
	now := time.Now().UTC()
	st := model.ProxyCheckOK
	if !ok {
		st = model.ProxyCheckFail
	}
	fields := map[string]any{
		"last_check_at": now,
		"last_check_ok": st,
		"last_check_ms": latencyMs,
		"last_error":    errMsg,
	}
	return r.db.WithContext(ctx).Model(&model.Proxy{}).
		Where("id = ?", id).Updates(fields).Error
}

// ListSubscriptions 返回未删除订阅。
func (r *ProxyRepo) ListSubscriptions(ctx context.Context) ([]*model.ProxySubscription, error) {
	var items []*model.ProxySubscription
	err := r.db.WithContext(ctx).
		Where("deleted_at IS NULL").
		Order("id DESC").
		Find(&items).Error
	return items, err
}

// GetSubscriptionByID 查询订阅。
func (r *ProxyRepo) GetSubscriptionByID(ctx context.Context, id uint64) (*model.ProxySubscription, error) {
	var sub model.ProxySubscription
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&sub).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &sub, nil
}

// CreateSubscription 新增订阅。
func (r *ProxyRepo) CreateSubscription(ctx context.Context, sub *model.ProxySubscription) error {
	return r.db.WithContext(ctx).Create(sub).Error
}

// UpdateSubscription 更新订阅。
func (r *ProxyRepo) UpdateSubscription(ctx context.Context, id uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.ProxySubscription{}).
		Where("id = ?", id).Updates(fields).Error
}

// SoftDeleteSubscription 软删除订阅，并删除它同步出的代理。
func (r *ProxyRepo) SoftDeleteSubscription(ctx context.Context, id uint64) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ProxySubscription{}).
			Where("id = ?", id).
			Update("deleted_at", now).Error; err != nil {
			return err
		}
		return tx.Model(&model.Proxy{}).
			Where("subscription_id = ? AND deleted_at IS NULL", id).
			Update("deleted_at", now).Error
	})
}
