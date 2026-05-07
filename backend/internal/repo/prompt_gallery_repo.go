package repo

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/model"
)

type PromptGalleryRepo struct{ db *gorm.DB }

func NewPromptGalleryRepo(db *gorm.DB) *PromptGalleryRepo { return &PromptGalleryRepo{db: db} }

type PromptGalleryListFilter struct {
	Keyword     string
	Modality    string
	Category    string
	Locale      string
	Status      *int
	OnlyEnabled bool
	Page        int
	PageSize    int
	Limit       int
}

func (r *PromptGalleryRepo) List(ctx context.Context, f PromptGalleryListFilter) ([]*model.PromptGalleryItem, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.PromptGalleryItem{})
	if f.OnlyEnabled {
		q = q.Where("status = ?", model.PromptGalleryStatusEnabled)
	} else if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if v := strings.TrimSpace(f.Modality); v != "" {
		q = q.Where("modality = ?", v)
	}
	if v := strings.TrimSpace(f.Category); v != "" {
		q = q.Where("category = ?", v)
	}
	if v := strings.TrimSpace(f.Locale); v != "" {
		q = q.Where("locale = ?", v)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("CAST(id AS CHAR) = ? OR title LIKE ? OR subtitle LIKE ? OR category LIKE ? OR prompt LIKE ?", kw, like, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*model.PromptGalleryItem
	if f.Limit > 0 {
		if f.Limit > 100 {
			f.Limit = 100
		}
		err := q.Order("sort_order ASC, id ASC").Limit(f.Limit).Find(&rows).Error
		return rows, total, err
	}
	err := q.Order("sort_order ASC, id DESC").Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error
	return rows, total, err
}

func (r *PromptGalleryRepo) Create(ctx context.Context, row *model.PromptGalleryItem) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *PromptGalleryRepo) Update(ctx context.Context, id uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.PromptGalleryItem{}).Where("id = ?", id).Updates(fields).Error
}

func (r *PromptGalleryRepo) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&model.PromptGalleryItem{}, id).Error
}

func (r *PromptGalleryRepo) Reorder(ctx context.Context, items map[uint64]int, updatedBy uint64) error {
	if len(items) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for id, sort := range items {
			if err := tx.Model(&model.PromptGalleryItem{}).
				Where("id = ?", id).
				Updates(map[string]any{"sort_order": sort, "updated_by": updatedBy}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
