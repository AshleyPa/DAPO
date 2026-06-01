package model

import "time"

const (
	APIChannelStatusDisabled = 0
	APIChannelStatusEnabled  = 1
)

const (
	APIChannelKeyStatusDisabled = 0
	APIChannelKeyStatusEnabled  = 1
)

const (
	APIChannelAdapterOpenAIChat      = "openai_compatible_chat"
	APIChannelAdapterOpenAIImages    = "openai_compatible_images"
	APIChannelAdapterOpenAIVideo     = "openai_compatible_video"
	APIChannelAdapterOpenAIResponses = "openai_responses"
	APIChannelAdapterNovaAsync       = "nova_async"
	APIChannelAdapterPic2APIImages   = "pic2api_images"
)

// APIChannel is an official/API-key based upstream channel.
//
// It is intentionally separate from Account, which models reverse or account
// pool credentials. Runtime routing will consume APIChannel in a later phase.
type APIChannel struct {
	ID             uint64     `gorm:"primaryKey;column:id" json:"id"`
	Code           string     `gorm:"column:code;size:64;not null;uniqueIndex:uk_api_channel_code" json:"code"`
	Name           string     `gorm:"column:name;size:128;not null" json:"name"`
	ProviderName   string     `gorm:"column:provider_name;size:64;not null;default:''" json:"provider_name"`
	Adapter        string     `gorm:"column:adapter;size:64;not null" json:"adapter"`
	BaseURL        string     `gorm:"column:base_url;size:512;not null" json:"base_url"`
	CredentialEnc  []byte     `gorm:"column:credential_enc;type:blob" json:"-"`
	Models         *string    `gorm:"column:models;type:json" json:"models,omitempty"`
	Capabilities   *string    `gorm:"column:capabilities;type:json" json:"capabilities,omitempty"`
	ProxyID        *uint64    `gorm:"column:proxy_id" json:"proxy_id,omitempty"`
	Priority       int        `gorm:"column:priority;not null;default:100" json:"priority"`
	Weight         int        `gorm:"column:weight;not null;default:100" json:"weight"`
	RPMLimit       int        `gorm:"column:rpm_limit;not null;default:0" json:"rpm_limit"`
	TPMLimit       int        `gorm:"column:tpm_limit;not null;default:0" json:"tpm_limit"`
	TimeoutSeconds int        `gorm:"column:timeout_seconds;not null;default:300" json:"timeout_seconds"`
	Status         int8       `gorm:"column:status;not null;default:1;index:idx_api_channel_status" json:"status"`
	LastTestAt     *time.Time `gorm:"column:last_test_at" json:"last_test_at,omitempty"`
	LastTestStatus int8       `gorm:"column:last_test_status;not null;default:0" json:"last_test_status"`
	LastTestError  *string    `gorm:"column:last_test_error;size:512" json:"last_test_error,omitempty"`
	Remark         *string    `gorm:"column:remark;size:512" json:"remark,omitempty"`
	CreatedBy      *uint64    `gorm:"column:created_by" json:"created_by,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt      *time.Time `gorm:"column:deleted_at;index" json:"-"`
}

func (APIChannel) TableName() string { return "api_channel" }

// APIChannelKey is one encrypted credential inside an official/API-key channel.
// It lets a channel rotate or load-balance several upstream keys without mixing
// those keys into the reverse account pool.
type APIChannelKey struct {
	ID            uint64     `gorm:"primaryKey;column:id" json:"id"`
	ChannelID     uint64     `gorm:"column:channel_id;not null;index:idx_api_channel_key_channel_status,priority:1" json:"channel_id"`
	Name          string     `gorm:"column:name;size:128;not null;default:''" json:"name"`
	CredentialEnc []byte     `gorm:"column:credential_enc;type:blob;not null" json:"-"`
	Priority      int        `gorm:"column:priority;not null;default:100;index:idx_api_channel_key_channel_status,priority:3" json:"priority"`
	Weight        int        `gorm:"column:weight;not null;default:100" json:"weight"`
	RPMLimit      int        `gorm:"column:rpm_limit;not null;default:0" json:"rpm_limit"`
	TPMLimit      int        `gorm:"column:tpm_limit;not null;default:0" json:"tpm_limit"`
	Status        int8       `gorm:"column:status;not null;default:1;index:idx_api_channel_key_channel_status,priority:2" json:"status"`
	LastUsedAt    *time.Time `gorm:"column:last_used_at" json:"last_used_at,omitempty"`
	LastError     *string    `gorm:"column:last_error;size:512" json:"last_error,omitempty"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;index:idx_api_channel_key_deleted" json:"-"`
}

func (APIChannelKey) TableName() string { return "api_channel_key" }
