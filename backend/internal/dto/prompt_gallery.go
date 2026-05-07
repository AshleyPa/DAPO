package dto

type PromptGalleryListReq struct {
	Keyword  string `form:"keyword" binding:"omitempty,max=128"`
	Modality string `form:"modality" binding:"omitempty,oneof=image text video"`
	Category string `form:"category" binding:"omitempty,max=64"`
	Locale   string `form:"locale" binding:"omitempty,max=16"`
	Status   *int   `form:"status" binding:"omitempty,oneof=0 1"`
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=200"`
}

type PublicPromptGalleryListReq struct {
	Modality string `form:"modality" binding:"omitempty,oneof=image text video"`
	Category string `form:"category" binding:"omitempty,max=64"`
	Locale   string `form:"locale" binding:"omitempty,max=16"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
}

type PromptGalleryResp struct {
	ID              uint64         `json:"id"`
	Modality        string         `json:"modality"`
	Category        string         `json:"category"`
	Title           string         `json:"title"`
	Subtitle        string         `json:"subtitle,omitempty"`
	CoverURL        string         `json:"cover_url"`
	Prompt          string         `json:"prompt"`
	Tags            []string       `json:"tags"`
	VariablesSchema map[string]any `json:"variables_schema"`
	SortOrder       int            `json:"sort_order"`
	Status          int8           `json:"status"`
	Locale          string         `json:"locale"`
	CreatedAt       int64          `json:"created_at"`
	UpdatedAt       int64          `json:"updated_at"`
}

type PromptGalleryCreateReq struct {
	Modality        string         `json:"modality" binding:"required,oneof=image text video"`
	Category        string         `json:"category" binding:"omitempty,max=64"`
	Title           string         `json:"title" binding:"required,max=80"`
	Subtitle        string         `json:"subtitle" binding:"omitempty,max=160"`
	CoverURL        string         `json:"cover_url" binding:"required,max=512"`
	Prompt          string         `json:"prompt" binding:"required,max=12000"`
	Tags            []string       `json:"tags" binding:"omitempty,max=20,dive,max=32"`
	VariablesSchema map[string]any `json:"variables_schema"`
	SortOrder       int            `json:"sort_order" binding:"omitempty,min=-999999,max=999999"`
	Status          *int8          `json:"status" binding:"omitempty,oneof=0 1"`
	Locale          string         `json:"locale" binding:"omitempty,max=16"`
}

type PromptGalleryUpdateReq struct {
	Modality        *string        `json:"modality" binding:"omitempty,oneof=image text video"`
	Category        *string        `json:"category" binding:"omitempty,max=64"`
	Title           *string        `json:"title" binding:"omitempty,max=80"`
	Subtitle        *string        `json:"subtitle" binding:"omitempty,max=160"`
	CoverURL        *string        `json:"cover_url" binding:"omitempty,max=512"`
	Prompt          *string        `json:"prompt" binding:"omitempty,max=12000"`
	Tags            []string       `json:"tags" binding:"omitempty,max=20,dive,max=32"`
	VariablesSchema map[string]any `json:"variables_schema"`
	SortOrder       *int           `json:"sort_order" binding:"omitempty,min=-999999,max=999999"`
	Status          *int8          `json:"status" binding:"omitempty,oneof=0 1"`
	Locale          *string        `json:"locale" binding:"omitempty,max=16"`
}

type PromptGalleryReorderReq struct {
	Items []PromptGalleryReorderItem `json:"items" binding:"required,min=1,max=200,dive"`
}

type PromptGalleryReorderItem struct {
	ID        uint64 `json:"id" binding:"required,min=1"`
	SortOrder int    `json:"sort_order" binding:"omitempty,min=-999999,max=999999"`
}
