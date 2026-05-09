package dto

// ProxyCreateReq 创建代理。
type ProxyCreateReq struct {
	Name     string `json:"name"     binding:"required,min=1,max=128"`
	Protocol string `json:"protocol" binding:"required,oneof=http https socks5 socks5h"`
	Host     string `json:"host"     binding:"required,min=1,max=255"`
	Port     uint16 `json:"port"     binding:"required,min=1,max=65535"`
	Username string `json:"username" binding:"omitempty,max=255"`
	Password string `json:"password" binding:"omitempty,max=255"`
	Remark   string `json:"remark"   binding:"omitempty,max=255"`
}

// ProxyUpdateReq 更新代理。
type ProxyUpdateReq struct {
	Name     *string `json:"name"     binding:"omitempty,min=1,max=128"`
	Protocol *string `json:"protocol" binding:"omitempty,oneof=http https socks5 socks5h"`
	Host     *string `json:"host"     binding:"omitempty,min=1,max=255"`
	Port     *uint16 `json:"port"     binding:"omitempty,min=1,max=65535"`
	Username *string `json:"username" binding:"omitempty,max=255"`
	Password *string `json:"password" binding:"omitempty,max=255"`
	Status   *int8   `json:"status"   binding:"omitempty,oneof=0 1"`
	Remark   *string `json:"remark"   binding:"omitempty,max=255"`
}

// ProxyListReq 列表过滤。
type ProxyListReq struct {
	Status   *int8  `form:"status"`
	Keyword  string `form:"keyword"   binding:"omitempty,max=64"`
	Page     int    `form:"page"      binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=200"`
}

// ProxyResp 出参（密码不返回）。
type ProxyResp struct {
	ID             uint64  `json:"id"`
	Name           string  `json:"name"`
	Protocol       string  `json:"protocol"`
	Host           string  `json:"host"`
	Port           uint16  `json:"port"`
	Username       string  `json:"username,omitempty"`
	HasPassword    bool    `json:"has_password"`
	Status         int8    `json:"status"`
	LastCheckAt    int64   `json:"last_check_at,omitempty"`
	LastCheckOK    int8    `json:"last_check_ok"`
	LastCheckMs    int     `json:"last_check_ms"`
	LastError      string  `json:"last_error,omitempty"`
	Remark         string  `json:"remark,omitempty"`
	SubscriptionID *uint64 `json:"subscription_id,omitempty"`
	SubNodeName    string  `json:"sub_node_name,omitempty"`
	CreatedAt      int64   `json:"created_at"`
	UpdatedAt      int64   `json:"updated_at"`
}

// ProxyTestResp 代理测试响应。
type ProxyTestResp struct {
	OK        bool   `json:"ok"`
	LatencyMs int    `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// ProxyBatchImportReq 批量导入代理。
type ProxyBatchImportReq struct {
	Text string `json:"text" binding:"required"`
}

// ProxyBatchDeleteReq 按 ID 批量删除代理。
type ProxyBatchDeleteReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1,max=2000,dive,min=1"`
}

// ProxyBatchTestReq 按 ID 批量测试代理。
type ProxyBatchTestReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1,max=2000,dive,min=1"`
}

// ProxyBatchImportResult 批量导入结果。
type ProxyBatchImportResult struct {
	Created int      `json:"created"`
	Skipped int      `json:"skipped"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

// ProxyBatchTestResult 批量测试结果。
type ProxyBatchTestResult struct {
	Tested int      `json:"tested"`
	OK     int      `json:"ok"`
	Failed int      `json:"failed"`
	IDs    []uint64 `json:"ids,omitempty"`
}

// ProxyBatchDeleteResult 批量删除结果。
type ProxyBatchDeleteResult struct {
	Deleted int64 `json:"deleted"`
}

// ProxySubscriptionCreateReq 创建 Clash/Mihomo 订阅。
type ProxySubscriptionCreateReq struct {
	Name            string `json:"name" binding:"required,min=1,max=128"`
	URL             string `json:"url" binding:"required,min=8,max=4096"`
	PortStart       int    `json:"port_start" binding:"omitempty,min=1024,max=65000"`
	AutoSync        *bool  `json:"auto_sync"`
	SyncIntervalMin int    `json:"sync_interval_min" binding:"omitempty,min=5,max=10080"`
}

// ProxySubscriptionPreviewReq 预览订阅节点，不入库。
type ProxySubscriptionPreviewReq struct {
	URL string `json:"url" binding:"required,min=8,max=4096"`
}

// ProxySubscriptionResp 订阅响应，不返回原始订阅 URL。
type ProxySubscriptionResp struct {
	ID              uint64 `json:"id"`
	Name            string `json:"name"`
	PortStart       int    `json:"port_start"`
	NodeCount       int    `json:"node_count"`
	AutoSync        bool   `json:"auto_sync"`
	SyncIntervalMin int    `json:"sync_interval_min"`
	LastSyncAt      int64  `json:"last_sync_at,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	Status          int8   `json:"status"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

// ClashNodeResp 订阅节点预览。
type ClashNodeResp struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Server string `json:"server"`
	Port   int    `json:"port"`
}

// ProxySubscriptionPreviewResp 订阅预览响应。
type ProxySubscriptionPreviewResp struct {
	NodeCount int             `json:"node_count"`
	Tunnel    int             `json:"tunnel"`
	Direct    int             `json:"direct"`
	Nodes     []ClashNodeResp `json:"nodes"`
}

// ProxySubscriptionSyncResp 同步结果。
type ProxySubscriptionSyncResp struct {
	NodeCount int `json:"node_count"`
	Tunnel    int `json:"tunnel"`
	Direct    int `json:"direct"`
	Created   int `json:"created"`
}
