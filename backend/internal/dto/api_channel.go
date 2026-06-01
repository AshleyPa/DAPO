package dto

type APIChannelCreateReq struct {
	Code           string   `json:"code" binding:"required,min=1,max=64"`
	Name           string   `json:"name" binding:"required,min=1,max=128"`
	ProviderName   string   `json:"provider_name" binding:"omitempty,max=64"`
	Adapter        string   `json:"adapter" binding:"required,min=1,max=64"`
	BaseURL        string   `json:"base_url" binding:"required,min=8,max=512"`
	APIKey         string   `json:"api_key" binding:"omitempty,max=4096"`
	Models         []string `json:"models" binding:"omitempty,max=200,dive,max=128"`
	Capabilities   []string `json:"capabilities" binding:"omitempty,max=20,dive,max=32"`
	ProxyID        *uint64  `json:"proxy_id"`
	Priority       int      `json:"priority" binding:"omitempty,min=0,max=10000"`
	Weight         int      `json:"weight" binding:"omitempty,min=1,max=10000"`
	RPMLimit       int      `json:"rpm_limit" binding:"omitempty,min=0"`
	TPMLimit       int      `json:"tpm_limit" binding:"omitempty,min=0"`
	TimeoutSeconds int      `json:"timeout_seconds" binding:"omitempty,min=5,max=1800"`
	Status         *int8    `json:"status" binding:"omitempty,oneof=0 1"`
	Remark         string   `json:"remark" binding:"omitempty,max=512"`
}

type APIChannelUpdateReq struct {
	Code           *string  `json:"code" binding:"omitempty,min=1,max=64"`
	Name           *string  `json:"name" binding:"omitempty,min=1,max=128"`
	ProviderName   *string  `json:"provider_name" binding:"omitempty,max=64"`
	Adapter        *string  `json:"adapter" binding:"omitempty,min=1,max=64"`
	BaseURL        *string  `json:"base_url" binding:"omitempty,min=8,max=512"`
	APIKey         *string  `json:"api_key" binding:"omitempty,max=4096"`
	ClearAPIKey    *bool    `json:"clear_api_key"`
	Models         []string `json:"models" binding:"omitempty,max=200,dive,max=128"`
	Capabilities   []string `json:"capabilities" binding:"omitempty,max=20,dive,max=32"`
	ProxyID        *uint64  `json:"proxy_id"`
	ClearProxy     *bool    `json:"clear_proxy"`
	Priority       *int     `json:"priority" binding:"omitempty,min=0,max=10000"`
	Weight         *int     `json:"weight" binding:"omitempty,min=1,max=10000"`
	RPMLimit       *int     `json:"rpm_limit" binding:"omitempty,min=0"`
	TPMLimit       *int     `json:"tpm_limit" binding:"omitempty,min=0"`
	TimeoutSeconds *int     `json:"timeout_seconds" binding:"omitempty,min=5,max=1800"`
	Status         *int8    `json:"status" binding:"omitempty,oneof=0 1"`
	Remark         *string  `json:"remark" binding:"omitempty,max=512"`
}

type APIChannelListReq struct {
	Adapter  string `form:"adapter" binding:"omitempty,max=64"`
	Status   *int8  `form:"status"`
	Keyword  string `form:"keyword" binding:"omitempty,max=64"`
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=200"`
}

type APIChannelResp struct {
	ID              uint64   `json:"id"`
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	ProviderName    string   `json:"provider_name"`
	Adapter         string   `json:"adapter"`
	BaseURL         string   `json:"base_url"`
	HasAPIKey       bool     `json:"has_api_key"`
	KeyCount        int64    `json:"key_count"`
	EnabledKeyCount int64    `json:"enabled_key_count"`
	Models          []string `json:"models"`
	Capabilities    []string `json:"capabilities"`
	ProxyID         *uint64  `json:"proxy_id,omitempty"`
	Priority        int      `json:"priority"`
	Weight          int      `json:"weight"`
	RPMLimit        int      `json:"rpm_limit"`
	TPMLimit        int      `json:"tpm_limit"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	Status          int8     `json:"status"`
	LastTestAt      int64    `json:"last_test_at,omitempty"`
	LastTestStatus  int8     `json:"last_test_status"`
	LastTestError   string   `json:"last_test_error,omitempty"`
	Remark          string   `json:"remark,omitempty"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

type APIChannelSecretsResp struct {
	APIKey string `json:"api_key"`
}

type APIChannelTestResp struct {
	OK               bool   `json:"ok"`
	Status           int    `json:"status"`
	LatencyMs        int64  `json:"latency_ms"`
	Error            string `json:"error,omitempty"`
	TestedAt         int64  `json:"tested_at"`
	CredentialSource string `json:"credential_source,omitempty"`
	KeyID            uint64 `json:"key_id,omitempty"`
	KeyName          string `json:"key_name,omitempty"`
}

type APIChannelKeyCreateReq struct {
	Name     string `json:"name" binding:"omitempty,max=128"`
	APIKey   string `json:"api_key" binding:"required,min=1,max=4096"`
	Priority int    `json:"priority" binding:"omitempty,min=0,max=10000"`
	Weight   int    `json:"weight" binding:"omitempty,min=1,max=10000"`
	RPMLimit int    `json:"rpm_limit" binding:"omitempty,min=0"`
	TPMLimit int    `json:"tpm_limit" binding:"omitempty,min=0"`
	Status   *int8  `json:"status" binding:"omitempty,oneof=0 1"`
}

type APIChannelKeyUpdateReq struct {
	Name     *string `json:"name" binding:"omitempty,max=128"`
	APIKey   *string `json:"api_key" binding:"omitempty,max=4096"`
	Priority *int    `json:"priority" binding:"omitempty,min=0,max=10000"`
	Weight   *int    `json:"weight" binding:"omitempty,min=1,max=10000"`
	RPMLimit *int    `json:"rpm_limit" binding:"omitempty,min=0"`
	TPMLimit *int    `json:"tpm_limit" binding:"omitempty,min=0"`
	Status   *int8   `json:"status" binding:"omitempty,oneof=0 1"`
}

type APIChannelKeyResp struct {
	ID         uint64 `json:"id"`
	ChannelID  uint64 `json:"channel_id"`
	Name       string `json:"name"`
	HasAPIKey  bool   `json:"has_api_key"`
	Priority   int    `json:"priority"`
	Weight     int    `json:"weight"`
	RPMLimit   int    `json:"rpm_limit"`
	TPMLimit   int    `json:"tpm_limit"`
	Status     int8   `json:"status"`
	LastUsedAt int64  `json:"last_used_at,omitempty"`
	LastError  string `json:"last_error,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}
