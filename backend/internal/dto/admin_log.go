package dto

type AdminGenerationLogListReq struct {
	Keyword  string `form:"keyword" binding:"omitempty,max=128"`
	Kind     string `form:"kind" binding:"omitempty,oneof=image video chat text"`
	Status   *int   `form:"status" binding:"omitempty,oneof=0 1 2 3 4"`
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=200"`
}

type AdminGenerationLogResp struct {
	TaskID                    string `json:"task_id"`
	CreatedAt                 int64  `json:"created_at"`
	UserID                    uint64 `json:"user_id"`
	UserLabel                 string `json:"user_label"`
	APIKeyID                  uint64 `json:"api_key_id,omitempty"`
	KeyLabel                  string `json:"key_label,omitempty"`
	Kind                      string `json:"kind"`
	ModelCode                 string `json:"model_code"`
	Prompt                    string `json:"prompt"`
	Status                    int8   `json:"status"`
	DurationMs                int64  `json:"duration_ms,omitempty"`
	CostPoints                int64  `json:"cost_points"`
	PreviewURL                string `json:"preview_url,omitempty"`
	Error                     string `json:"error,omitempty"`
	ModelGatewayRouteSnapshot any    `json:"model_gateway_route_snapshot,omitempty"`
	PricingSnapshot           any    `json:"pricing_snapshot,omitempty"`
}

type AdminGenerationLogPurgeReq struct {
	Days int `json:"days" binding:"required,min=1,max=3650"`
}

type AdminGenerationLogPurgeResp struct {
	Deleted int64 `json:"deleted"`
}

type AdminUpstreamFailureListReq struct {
	Keyword    string  `form:"keyword" binding:"omitempty,max=128"`
	Provider   string  `form:"provider" binding:"omitempty,oneof=gpt grok api_channel"`
	AccountID  *uint64 `form:"account_id" binding:"omitempty,min=1"`
	Stage      string  `form:"stage" binding:"omitempty,max=64"`
	StatusCode *int    `form:"status_code" binding:"omitempty,min=0,max=599"`
	SinceHours int     `form:"since_hours" binding:"omitempty,min=1,max=8760"`
	Page       int     `form:"page" binding:"omitempty,min=1"`
	PageSize   int     `form:"page_size" binding:"omitempty,min=1,max=200"`
}

type AdminGenerationUpstreamLogResp struct {
	ID              uint64  `json:"id"`
	TaskID          string  `json:"task_id"`
	Provider        string  `json:"provider"`
	AccountID       *uint64 `json:"account_id,omitempty"`
	Kind            string  `json:"kind,omitempty"`
	ModelCode       string  `json:"model_code,omitempty"`
	Stage           string  `json:"stage"`
	Method          string  `json:"method,omitempty"`
	URL             string  `json:"url,omitempty"`
	StatusCode      int     `json:"status_code"`
	DurationMs      int64   `json:"duration_ms"`
	RequestExcerpt  string  `json:"request_excerpt,omitempty"`
	ResponseExcerpt string  `json:"response_excerpt,omitempty"`
	Error           string  `json:"error,omitempty"`
	Meta            string  `json:"meta,omitempty"`
	CreatedAt       int64   `json:"created_at"`
}

type AdminGenerationBillingProofResp struct {
	TaskID     string                              `json:"task_id"`
	Consume    *AdminGenerationConsumeRecordResp   `json:"consume_record,omitempty"`
	WalletLogs []*AdminGenerationBillingWalletResp `json:"wallet_logs"`
	Refunds    []*AdminGenerationRefundRecordResp  `json:"refund_records"`
	Summary    AdminGenerationBillingSummaryResp   `json:"summary"`
}

type AdminGenerationConsumeRecordResp struct {
	ID          uint64  `json:"id"`
	TaskID      string  `json:"task_id"`
	UserID      uint64  `json:"user_id"`
	Kind        string  `json:"kind"`
	ModelCode   string  `json:"model_code"`
	Count       int     `json:"count"`
	UnitPoints  int64   `json:"unit_points"`
	TotalPoints int64   `json:"total_points"`
	Status      int8    `json:"status"`
	AccountID   *uint64 `json:"account_id,omitempty"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
}

type AdminGenerationBillingWalletResp struct {
	ID           uint64 `json:"id"`
	UserID       uint64 `json:"user_id"`
	Direction    int8   `json:"direction"`
	BizType      string `json:"biz_type"`
	BizID        string `json:"biz_id"`
	Points       int64  `json:"points"`
	PointsBefore int64  `json:"points_before"`
	PointsAfter  int64  `json:"points_after"`
	Remark       string `json:"remark,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

type AdminGenerationRefundRecordResp struct {
	ID        uint64 `json:"id"`
	TaskID    string `json:"task_id"`
	UserID    uint64 `json:"user_id"`
	Points    int64  `json:"points"`
	Reason    string `json:"reason"`
	Operator  string `json:"operator"`
	CreatedAt int64  `json:"created_at"`
}

type AdminGenerationBillingSummaryResp struct {
	ConsumeRecordFound bool  `json:"consume_record_found"`
	ConsumeStatus      int8  `json:"consume_status,omitempty"`
	ConsumeTotalPoints int64 `json:"consume_total_points,omitempty"`
	WalletLogCount     int   `json:"wallet_log_count"`
	RefundRecordCount  int   `json:"refund_record_count"`
	WalletNetPoints    int64 `json:"wallet_net_points"`
	WalletSpendPoints  int64 `json:"wallet_spend_points"`
	WalletRefundPoints int64 `json:"wallet_refund_points"`
	WalletExtraPoints  int64 `json:"wallet_extra_points"`
}
