// Package model Prompt Gallery content models.
package model

import "time"

const (
	PromptGalleryStatusDisabled = 0
	PromptGalleryStatusEnabled  = 1

	PromptGalleryModalityImage = "image"
	PromptGalleryModalityText  = "text"
	PromptGalleryModalityVideo = "video"
)

// PromptGalleryItem is a configurable quick prompt card shown on the user frontend.
type PromptGalleryItem struct {
	ID              uint64    `gorm:"primaryKey;column:id" json:"id"`
	Modality        string    `gorm:"column:modality;size:16;not null;index:idx_modality_status_sort,priority:1;index:idx_modality_category,priority:1" json:"modality"`
	Category        string    `gorm:"column:category;size:64;not null;default:'';index:idx_modality_category,priority:2" json:"category"`
	Title           string    `gorm:"column:title;size:80;not null" json:"title"`
	Subtitle        *string   `gorm:"column:subtitle;size:160" json:"subtitle,omitempty"`
	CoverURL        string    `gorm:"column:cover_url;size:512;not null" json:"cover_url"`
	Prompt          string    `gorm:"column:prompt;type:text;not null" json:"prompt"`
	Tags            string    `gorm:"column:tags;type:json;not null" json:"tags"`
	VariablesSchema string    `gorm:"column:variables_schema;type:json;not null" json:"variables_schema"`
	SortOrder       int       `gorm:"column:sort_order;not null;default:0;index:idx_modality_status_sort,priority:3" json:"sort_order"`
	Status          int8      `gorm:"column:status;not null;default:1;index:idx_modality_status_sort,priority:2" json:"status"`
	Locale          string    `gorm:"column:locale;size:16;not null;default:zh-CN" json:"locale"`
	CreatedBy       *uint64   `gorm:"column:created_by" json:"created_by,omitempty"`
	UpdatedBy       *uint64   `gorm:"column:updated_by" json:"updated_by,omitempty"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName returns the prompt gallery table name.
func (PromptGalleryItem) TableName() string { return "prompt_gallery_item" }
