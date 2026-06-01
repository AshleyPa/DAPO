package model

import "time"

const (
	ModelCatalogStatusDisabled = 0
	ModelCatalogStatusEnabled  = 1
)

const (
	ModelCatalogKindText  = "text"
	ModelCatalogKindImage = "image"
	ModelCatalogKindVideo = "video"
	ModelCatalogKindChat  = "chat"
)

const (
	ModelCatalogPricingFixed  = "fixed"
	ModelCatalogPricingToken  = "token"
	ModelCatalogPricingChar   = "char"
	ModelCatalogPricingMatrix = "matrix"
	ModelCatalogPricingManual = "manual"
)

const (
	ModelSourceStatusDisabled = 0
	ModelSourceStatusEnabled  = 1
)

const (
	ModelSourceTypeAPIChannel  = "api_channel"
	ModelSourceTypeAccountPool = "account_pool"
)

// ModelCatalog is the Model Gateway's public model registry.
//
// It is intentionally separated from the upstream `model` seed table and the
// legacy billing.model_prices config while the migration is staged locally.
type ModelCatalog struct {
	ID                   uint64     `gorm:"primaryKey;column:id" json:"id"`
	ModelCode            string     `gorm:"column:model_code;size:128;not null;uniqueIndex:uk_model_catalog_code" json:"model_code"`
	DisplayName          string     `gorm:"column:display_name;size:128;not null" json:"display_name"`
	EntryKind            string     `gorm:"column:entry_kind;size:16;not null;index:idx_model_catalog_kind_status,priority:1" json:"entry_kind"`
	ProviderHint         string     `gorm:"column:provider_hint;size:64;not null;default:''" json:"provider_hint"`
	UpstreamDefaultModel string     `gorm:"column:upstream_default_model;size:128;not null;default:''" json:"upstream_default_model"`
	Capabilities         *string    `gorm:"column:capabilities;type:json" json:"capabilities,omitempty"`
	ParametersSchema     *string    `gorm:"column:parameters_schema;type:json" json:"parameters_schema,omitempty"`
	PricingMode          string     `gorm:"column:pricing_mode;size:32;not null;default:fixed" json:"pricing_mode"`
	UnitPoints           int64      `gorm:"column:unit_points;not null;default:0" json:"unit_points"`
	InputUnitPoints      int64      `gorm:"column:input_unit_points;not null;default:0" json:"input_unit_points"`
	OutputUnitPoints     int64      `gorm:"column:output_unit_points;not null;default:0" json:"output_unit_points"`
	PriceRules           *string    `gorm:"column:price_rules;type:json" json:"price_rules,omitempty"`
	MinPlan              string     `gorm:"column:min_plan;size:32;not null;default:free" json:"min_plan"`
	Tags                 *string    `gorm:"column:tags;type:json" json:"tags,omitempty"`
	Description          *string    `gorm:"column:description;type:text" json:"description,omitempty"`
	SortOrder            int        `gorm:"column:sort_order;not null;default:0" json:"sort_order"`
	Visible              int8       `gorm:"column:visible;not null;default:1;index:idx_model_catalog_visible_sort,priority:1" json:"visible"`
	Status               int8       `gorm:"column:status;not null;default:1;index:idx_model_catalog_kind_status,priority:2" json:"status"`
	CreatedBy            *uint64    `gorm:"column:created_by" json:"created_by,omitempty"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt            *time.Time `gorm:"column:deleted_at;index:idx_model_catalog_deleted" json:"-"`
}

func (ModelCatalog) TableName() string { return "model_catalog" }

// ModelSourceMapping maps a public model to an official API channel or reverse
// account pool candidate. Runtime consumers will be added in a later phase.
type ModelSourceMapping struct {
	ID            uint64     `gorm:"primaryKey;column:id" json:"id"`
	ModelCode     string     `gorm:"column:model_code;size:128;not null;index:idx_model_source_model,priority:1" json:"model_code"`
	SourceType    string     `gorm:"column:source_type;size:32;not null;index:idx_model_source_source,priority:1" json:"source_type"`
	SourceCode    string     `gorm:"column:source_code;size:128;not null;index:idx_model_source_source,priority:2" json:"source_code"`
	UpstreamModel string     `gorm:"column:upstream_model;size:128;not null;default:''" json:"upstream_model"`
	Adapter       string     `gorm:"column:adapter;size:64;not null;default:''" json:"adapter"`
	AuthType      string     `gorm:"column:auth_type;size:32;not null;default:''" json:"auth_type"`
	ImageAPIMode  string     `gorm:"column:image_api_mode;size:32;not null;default:''" json:"image_api_mode"`
	Strategy      string     `gorm:"column:strategy;size:32;not null;default:round_robin" json:"strategy"`
	Priority      int        `gorm:"column:priority;not null;default:100" json:"priority"`
	Weight        int        `gorm:"column:weight;not null;default:100" json:"weight"`
	Status        int8       `gorm:"column:status;not null;default:1;index:idx_model_source_model,priority:2" json:"status"`
	Remark        *string    `gorm:"column:remark;size:512" json:"remark,omitempty"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;index:idx_model_source_deleted" json:"-"`
}

func (ModelSourceMapping) TableName() string { return "model_source_mapping" }
