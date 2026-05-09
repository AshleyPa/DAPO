package model

import "time"

// Proxy 协议常量。
const (
	ProxyProtoHTTP    = "http"
	ProxyProtoHTTPS   = "https"
	ProxyProtoSOCKS5  = "socks5"
	ProxyProtoSOCKS5H = "socks5h"
)

// Proxy 状态。
const (
	ProxyStatusEnabled  = 1
	ProxyStatusDisabled = 0
)

// 测试结果。
const (
	ProxyCheckUnknown = 0
	ProxyCheckOK      = 1
	ProxyCheckFail    = 2
)

// Proxy 出站代理实体。表 `proxy`。
type Proxy struct {
	ID             uint64     `gorm:"primaryKey;column:id" json:"id"`
	Name           string     `gorm:"column:name;size:128;not null" json:"name"`
	Protocol       string     `gorm:"column:protocol;size:16;not null" json:"protocol"`
	Host           string     `gorm:"column:host;size:255;not null" json:"host"`
	Port           uint16     `gorm:"column:port;not null" json:"port"`
	Username       *string    `gorm:"column:username;size:255" json:"username,omitempty"`
	PasswordEnc    []byte     `gorm:"column:password_enc;type:blob" json:"-"`
	Status         int8       `gorm:"column:status;not null;default:1" json:"status"`
	LastCheckAt    *time.Time `gorm:"column:last_check_at" json:"last_check_at,omitempty"`
	LastCheckOK    int8       `gorm:"column:last_check_ok;not null;default:0" json:"last_check_ok"`
	LastCheckMs    int        `gorm:"column:last_check_ms;not null;default:0" json:"last_check_ms"`
	LastError      *string    `gorm:"column:last_error;size:255" json:"last_error,omitempty"`
	Remark         *string    `gorm:"column:remark;size:255" json:"remark,omitempty"`
	SubscriptionID *uint64    `gorm:"column:subscription_id" json:"subscription_id,omitempty"`
	SubNodeName    string     `gorm:"column:sub_node_name;size:256;not null;default:''" json:"sub_node_name,omitempty"`
	CreatedBy      *uint64    `gorm:"column:created_by" json:"created_by,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt      *time.Time `gorm:"column:deleted_at;index" json:"-"`
}

// TableName 表名。
func (Proxy) TableName() string { return "proxy" }

// ProxySubscription 保存 Clash/Mihomo 订阅。URL 必须加密存储。
type ProxySubscription struct {
	ID              uint64     `gorm:"primaryKey;column:id" json:"id"`
	Name            string     `gorm:"column:name;size:128;not null" json:"name"`
	URLEnc          []byte     `gorm:"column:url_enc;type:blob;not null" json:"-"`
	PortStart       int        `gorm:"column:port_start;not null;default:17001" json:"port_start"`
	NodeCount       int        `gorm:"column:node_count;not null;default:0" json:"node_count"`
	AutoSync        bool       `gorm:"column:auto_sync;not null;default:1" json:"auto_sync"`
	SyncIntervalMin int        `gorm:"column:sync_interval_min;not null;default:60" json:"sync_interval_min"`
	LastSyncAt      *time.Time `gorm:"column:last_sync_at" json:"last_sync_at,omitempty"`
	LastError       *string    `gorm:"column:last_error;size:512" json:"last_error,omitempty"`
	Status          int8       `gorm:"column:status;not null;default:1" json:"status"`
	CreatedBy       *uint64    `gorm:"column:created_by" json:"created_by,omitempty"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	DeletedAt       *time.Time `gorm:"column:deleted_at;index" json:"-"`
}

func (ProxySubscription) TableName() string { return "proxy_subscription" }

// ClashNode 是从 Clash/YAML 或兼容订阅中解析出的节点摘要。
type ClashNode struct {
	Name   string                 `json:"name"`
	Type   string                 `json:"type"`
	Server string                 `json:"server"`
	Port   int                    `json:"port"`
	Raw    map[string]interface{} `json:"-"`
}
