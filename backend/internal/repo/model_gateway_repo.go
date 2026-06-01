package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/model"
)

type ModelCatalogRepo struct{ db *gorm.DB }

func NewModelCatalogRepo(db *gorm.DB) *ModelCatalogRepo { return &ModelCatalogRepo{db: db} }

type ModelCatalogListFilter struct {
	EntryKind string
	Status    *int8
	Visible   *int8
	Keyword   string
	Page      int
	PageSize  int
}

func (r *ModelCatalogRepo) Create(ctx context.Context, item *model.ModelCatalog) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *ModelCatalogRepo) GetByID(ctx context.Context, id uint64) (*model.ModelCatalog, error) {
	var item model.ModelCatalog
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&item).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &item, nil
}

func (r *ModelCatalogRepo) GetByCode(ctx context.Context, code string) (*model.ModelCatalog, error) {
	var item model.ModelCatalog
	err := r.db.WithContext(ctx).
		Where("model_code = ? AND deleted_at IS NULL", code).
		First(&item).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &item, nil
}

func (r *ModelCatalogRepo) List(ctx context.Context, f ModelCatalogListFilter) ([]*model.ModelCatalog, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.ModelCatalog{}).Where("deleted_at IS NULL")
	if f.EntryKind != "" {
		q = q.Where("entry_kind = ?", f.EntryKind)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.Visible != nil {
		q = q.Where("visible = ?", *f.Visible)
	}
	if f.Keyword != "" {
		k := "%" + f.Keyword + "%"
		q = q.Where("(model_code LIKE ? OR display_name LIKE ? OR provider_hint LIKE ? OR upstream_default_model LIKE ? OR description LIKE ?)", k, k, k, k, k)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*model.ModelCatalog
	err := q.Order("sort_order ASC, id DESC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *ModelCatalogRepo) Update(ctx context.Context, id uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.ModelCatalog{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(fields).Error
}

func (r *ModelCatalogRepo) SoftDelete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&model.ModelCatalog{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", time.Now().UTC()).Error
}

type ModelSourceRepo struct{ db *gorm.DB }

func NewModelSourceRepo(db *gorm.DB) *ModelSourceRepo { return &ModelSourceRepo{db: db} }

type ModelSourceListFilter struct {
	ModelCode  string
	SourceType string
	Status     *int8
	Page       int
	PageSize   int
}

func (r *ModelSourceRepo) Create(ctx context.Context, item *model.ModelSourceMapping) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *ModelSourceRepo) GetByID(ctx context.Context, id uint64) (*model.ModelSourceMapping, error) {
	var item model.ModelSourceMapping
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&item).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &item, nil
}

func (r *ModelSourceRepo) List(ctx context.Context, f ModelSourceListFilter) ([]*model.ModelSourceMapping, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 500 {
		f.PageSize = 100
	}
	q := r.db.WithContext(ctx).Model(&model.ModelSourceMapping{}).Where("deleted_at IS NULL")
	if f.ModelCode != "" {
		q = q.Where("model_code = ?", f.ModelCode)
	}
	if f.SourceType != "" {
		q = q.Where("source_type = ?", f.SourceType)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*model.ModelSourceMapping
	err := q.Order("priority ASC, id ASC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *ModelSourceRepo) CountByModelCode(ctx context.Context, modelCode string) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&model.ModelSourceMapping{}).
		Where("model_code = ? AND deleted_at IS NULL", modelCode).
		Count(&total).Error
	return total, err
}

func (r *ModelSourceRepo) Update(ctx context.Context, id uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.ModelSourceMapping{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(fields).Error
}

func (r *ModelSourceRepo) SoftDelete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&model.ModelSourceMapping{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", time.Now().UTC()).Error
}
