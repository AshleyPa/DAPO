package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/model"
)

type APIChannelRepo struct{ db *gorm.DB }

func NewAPIChannelRepo(db *gorm.DB) *APIChannelRepo { return &APIChannelRepo{db: db} }

type APIChannelListFilter struct {
	Adapter  string
	Status   *int8
	Keyword  string
	Page     int
	PageSize int
}

type APIChannelKeyListFilter struct {
	ChannelID uint64
	Status    *int8
	Page      int
	PageSize  int
}

func (r *APIChannelRepo) Create(ctx context.Context, ch *model.APIChannel) error {
	return r.db.WithContext(ctx).Create(ch).Error
}

func (r *APIChannelRepo) GetByID(ctx context.Context, id uint64) (*model.APIChannel, error) {
	var ch model.APIChannel
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&ch).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &ch, nil
}

func (r *APIChannelRepo) GetByCode(ctx context.Context, code string) (*model.APIChannel, error) {
	var ch model.APIChannel
	err := r.db.WithContext(ctx).
		Where("code = ? AND deleted_at IS NULL", code).
		First(&ch).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &ch, nil
}

func (r *APIChannelRepo) List(ctx context.Context, f APIChannelListFilter) ([]*model.APIChannel, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.APIChannel{}).Where("deleted_at IS NULL")
	if f.Adapter != "" {
		q = q.Where("adapter = ?", f.Adapter)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.Keyword != "" {
		k := "%" + f.Keyword + "%"
		q = q.Where("(code LIKE ? OR name LIKE ? OR provider_name LIKE ? OR base_url LIKE ? OR remark LIKE ?)", k, k, k, k, k)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*model.APIChannel
	err := q.Order("priority ASC, id DESC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *APIChannelRepo) Update(ctx context.Context, id uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.APIChannel{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(fields).Error
}

func (r *APIChannelRepo) SoftDelete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&model.APIChannel{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", time.Now().UTC()).Error
}

func (r *APIChannelRepo) CreateKey(ctx context.Context, key *model.APIChannelKey) error {
	return r.db.WithContext(ctx).Create(key).Error
}

func (r *APIChannelRepo) GetKeyByID(ctx context.Context, id uint64) (*model.APIChannelKey, error) {
	var key model.APIChannelKey
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&key).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &key, nil
}

func (r *APIChannelRepo) ListKeys(ctx context.Context, f APIChannelKeyListFilter) ([]*model.APIChannelKey, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 500 {
		f.PageSize = 100
	}
	q := r.db.WithContext(ctx).Model(&model.APIChannelKey{}).Where("deleted_at IS NULL")
	if f.ChannelID > 0 {
		q = q.Where("channel_id = ?", f.ChannelID)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*model.APIChannelKey
	err := q.Order("priority ASC, id ASC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *APIChannelRepo) CountKeys(ctx context.Context, channelID uint64, status *int8) (int64, error) {
	q := r.db.WithContext(ctx).Model(&model.APIChannelKey{}).
		Where("channel_id = ? AND deleted_at IS NULL", channelID)
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *APIChannelRepo) UpdateKey(ctx context.Context, id uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.APIChannelKey{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(fields).Error
}

func (r *APIChannelRepo) SoftDeleteKey(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&model.APIChannelKey{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", time.Now().UTC()).Error
}
