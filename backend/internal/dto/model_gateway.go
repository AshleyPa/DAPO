package dto

type ModelCatalogCreateReq struct {
	ModelCode            string   `json:"model_code" binding:"required,min=1,max=128"`
	DisplayName          string   `json:"display_name" binding:"required,min=1,max=128"`
	EntryKind            string   `json:"entry_kind" binding:"required,min=1,max=16"`
	ProviderHint         string   `json:"provider_hint" binding:"omitempty,max=64"`
	UpstreamDefaultModel string   `json:"upstream_default_model" binding:"omitempty,max=128"`
	Capabilities         []string `json:"capabilities" binding:"omitempty,max=30,dive,max=32"`
	ParametersSchema     any      `json:"parameters_schema"`
	PricingMode          string   `json:"pricing_mode" binding:"omitempty,max=32"`
	UnitPoints           int64    `json:"unit_points" binding:"omitempty,min=0"`
	InputUnitPoints      int64    `json:"input_unit_points" binding:"omitempty,min=0"`
	OutputUnitPoints     int64    `json:"output_unit_points" binding:"omitempty,min=0"`
	PriceRules           any      `json:"price_rules"`
	MinPlan              string   `json:"min_plan" binding:"omitempty,max=32"`
	Tags                 []string `json:"tags" binding:"omitempty,max=30,dive,max=32"`
	Description          string   `json:"description" binding:"omitempty,max=2000"`
	SortOrder            int      `json:"sort_order" binding:"omitempty,min=0,max=100000"`
	Visible              *int8    `json:"visible" binding:"omitempty,oneof=0 1"`
	Status               *int8    `json:"status" binding:"omitempty,oneof=0 1"`
}

type ModelCatalogUpdateReq struct {
	ModelCode             *string  `json:"model_code" binding:"omitempty,min=1,max=128"`
	DisplayName           *string  `json:"display_name" binding:"omitempty,min=1,max=128"`
	EntryKind             *string  `json:"entry_kind" binding:"omitempty,min=1,max=16"`
	ProviderHint          *string  `json:"provider_hint" binding:"omitempty,max=64"`
	UpstreamDefaultModel  *string  `json:"upstream_default_model" binding:"omitempty,max=128"`
	Capabilities          []string `json:"capabilities" binding:"omitempty,max=30,dive,max=32"`
	ParametersSchema      any      `json:"parameters_schema"`
	ClearParametersSchema *bool    `json:"clear_parameters_schema"`
	PricingMode           *string  `json:"pricing_mode" binding:"omitempty,max=32"`
	UnitPoints            *int64   `json:"unit_points" binding:"omitempty,min=0"`
	InputUnitPoints       *int64   `json:"input_unit_points" binding:"omitempty,min=0"`
	OutputUnitPoints      *int64   `json:"output_unit_points" binding:"omitempty,min=0"`
	PriceRules            any      `json:"price_rules"`
	ClearPriceRules       *bool    `json:"clear_price_rules"`
	MinPlan               *string  `json:"min_plan" binding:"omitempty,max=32"`
	Tags                  []string `json:"tags" binding:"omitempty,max=30,dive,max=32"`
	Description           *string  `json:"description" binding:"omitempty,max=2000"`
	SortOrder             *int     `json:"sort_order" binding:"omitempty,min=0,max=100000"`
	Visible               *int8    `json:"visible" binding:"omitempty,oneof=0 1"`
	Status                *int8    `json:"status" binding:"omitempty,oneof=0 1"`
}

type ModelCatalogListReq struct {
	EntryKind string `form:"entry_kind" binding:"omitempty,max=16"`
	Status    *int8  `form:"status"`
	Visible   *int8  `form:"visible"`
	Keyword   string `form:"keyword" binding:"omitempty,max=128"`
	Page      int    `form:"page" binding:"omitempty,min=1"`
	PageSize  int    `form:"page_size" binding:"omitempty,min=1,max=200"`
}

type ModelCatalogResp struct {
	ID                   uint64   `json:"id"`
	ModelCode            string   `json:"model_code"`
	DisplayName          string   `json:"display_name"`
	EntryKind            string   `json:"entry_kind"`
	ProviderHint         string   `json:"provider_hint"`
	UpstreamDefaultModel string   `json:"upstream_default_model"`
	Capabilities         []string `json:"capabilities"`
	ParametersSchema     any      `json:"parameters_schema,omitempty"`
	PricingMode          string   `json:"pricing_mode"`
	UnitPoints           int64    `json:"unit_points"`
	InputUnitPoints      int64    `json:"input_unit_points"`
	OutputUnitPoints     int64    `json:"output_unit_points"`
	PriceRules           any      `json:"price_rules,omitempty"`
	MinPlan              string   `json:"min_plan"`
	Tags                 []string `json:"tags"`
	Description          string   `json:"description,omitempty"`
	SortOrder            int      `json:"sort_order"`
	Visible              int8     `json:"visible"`
	Status               int8     `json:"status"`
	CreatedAt            int64    `json:"created_at"`
	UpdatedAt            int64    `json:"updated_at"`
}

type ModelSourceCreateReq struct {
	ModelCode     string `json:"model_code" binding:"required,min=1,max=128"`
	SourceType    string `json:"source_type" binding:"required,min=1,max=32"`
	SourceCode    string `json:"source_code" binding:"required,min=1,max=128"`
	UpstreamModel string `json:"upstream_model" binding:"omitempty,max=128"`
	Adapter       string `json:"adapter" binding:"omitempty,max=64"`
	AuthType      string `json:"auth_type" binding:"omitempty,max=32"`
	ImageAPIMode  string `json:"image_api_mode" binding:"omitempty,max=32"`
	Strategy      string `json:"strategy" binding:"omitempty,max=32"`
	Priority      int    `json:"priority" binding:"omitempty,min=0,max=10000"`
	Weight        int    `json:"weight" binding:"omitempty,min=1,max=10000"`
	Status        *int8  `json:"status" binding:"omitempty,oneof=0 1"`
	Remark        string `json:"remark" binding:"omitempty,max=512"`
}

type ModelSourceUpdateReq struct {
	ModelCode     *string `json:"model_code" binding:"omitempty,min=1,max=128"`
	SourceType    *string `json:"source_type" binding:"omitempty,min=1,max=32"`
	SourceCode    *string `json:"source_code" binding:"omitempty,min=1,max=128"`
	UpstreamModel *string `json:"upstream_model" binding:"omitempty,max=128"`
	Adapter       *string `json:"adapter" binding:"omitempty,max=64"`
	AuthType      *string `json:"auth_type" binding:"omitempty,max=32"`
	ImageAPIMode  *string `json:"image_api_mode" binding:"omitempty,max=32"`
	Strategy      *string `json:"strategy" binding:"omitempty,max=32"`
	Priority      *int    `json:"priority" binding:"omitempty,min=0,max=10000"`
	Weight        *int    `json:"weight" binding:"omitempty,min=1,max=10000"`
	Status        *int8   `json:"status" binding:"omitempty,oneof=0 1"`
	Remark        *string `json:"remark" binding:"omitempty,max=512"`
}

type ModelSourceListReq struct {
	ModelCode  string `form:"model_code" binding:"omitempty,max=128"`
	SourceType string `form:"source_type" binding:"omitempty,max=32"`
	Status     *int8  `form:"status"`
	Page       int    `form:"page" binding:"omitempty,min=1"`
	PageSize   int    `form:"page_size" binding:"omitempty,min=1,max=500"`
}

type ModelSourceResp struct {
	ID            uint64 `json:"id"`
	ModelCode     string `json:"model_code"`
	SourceType    string `json:"source_type"`
	SourceCode    string `json:"source_code"`
	UpstreamModel string `json:"upstream_model"`
	Adapter       string `json:"adapter,omitempty"`
	AuthType      string `json:"auth_type,omitempty"`
	ImageAPIMode  string `json:"image_api_mode,omitempty"`
	Strategy      string `json:"strategy"`
	Priority      int    `json:"priority"`
	Weight        int    `json:"weight"`
	Status        int8   `json:"status"`
	Remark        string `json:"remark,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

type ModelSourceConflictResp struct {
	ID            uint64 `json:"id"`
	ModelCode     string `json:"model_code"`
	SourceType    string `json:"source_type"`
	SourceCode    string `json:"source_code"`
	UpstreamModel string `json:"upstream_model"`
	Status        int8   `json:"status"`
	Reason        string `json:"reason"`
}

type ModelGatewayDryRunReq struct {
	ModelCode string `json:"model_code" binding:"required,min=1,max=128"`
	EntryKind string `json:"entry_kind" binding:"omitempty,max=16"`
}

type ModelGatewayDryRunResp struct {
	ModelCode      string                        `json:"model_code"`
	DisplayName    string                        `json:"display_name"`
	EntryKind      string                        `json:"entry_kind"`
	MatchedModel   bool                          `json:"matched_model"`
	SelectedIndex  int                           `json:"selected_index"`
	CandidateCount int                           `json:"candidate_count"`
	AvailableCount int                           `json:"available_count"`
	Warning        string                        `json:"warning,omitempty"`
	Candidates     []ModelGatewayDryRunCandidate `json:"candidates"`
}

type ModelGatewayDryRunCandidate struct {
	Index             int    `json:"index"`
	SourceType        string `json:"source_type"`
	SourceCode        string `json:"source_code"`
	SourceName        string `json:"source_name,omitempty"`
	UpstreamModel     string `json:"upstream_model"`
	Adapter           string `json:"adapter,omitempty"`
	AuthType          string `json:"auth_type,omitempty"`
	ImageAPIMode      string `json:"image_api_mode,omitempty"`
	Strategy          string `json:"strategy"`
	Priority          int    `json:"priority"`
	Weight            int    `json:"weight"`
	Status            int8   `json:"status"`
	Available         bool   `json:"available"`
	SkipReason        string `json:"skip_reason,omitempty"`
	CandidateAccounts int    `json:"candidate_accounts,omitempty"`
	AvailableAccounts int    `json:"available_accounts,omitempty"`
}

type ModelGatewayAuditListReq struct {
	Keyword       string `form:"keyword" binding:"omitempty,max=128"`
	Kind          string `form:"kind" binding:"omitempty,oneof=image video chat text"`
	ModelCode     string `form:"model_code" binding:"omitempty,max=128"`
	SourceCode    string `form:"source_code" binding:"omitempty,max=128"`
	SkipReason    string `form:"skip_reason" binding:"omitempty,max=256"`
	PricingSource string `form:"pricing_source" binding:"omitempty,max=64"`
	Settlement    string `form:"settlement" binding:"omitempty,max=64"`
	AuditType     string `form:"audit_type" binding:"omitempty,oneof=all route pricing output output_missing video video_missing"`
	Status        *int   `form:"status" binding:"omitempty,oneof=0 1 2 3 4"`
	Page          int    `form:"page" binding:"omitempty,min=1"`
	PageSize      int    `form:"page_size" binding:"omitempty,min=1,max=200"`
}

type ModelGatewayAuditResp struct {
	TaskID                    string   `json:"task_id"`
	CreatedAt                 int64    `json:"created_at"`
	UserID                    uint64   `json:"user_id"`
	UserLabel                 string   `json:"user_label"`
	Kind                      string   `json:"kind"`
	ModelCode                 string   `json:"model_code"`
	Status                    int8     `json:"status"`
	DurationMs                int64    `json:"duration_ms,omitempty"`
	CostPoints                int64    `json:"cost_points"`
	PreviewURL                string   `json:"preview_url,omitempty"`
	SelectedSourceType        string   `json:"selected_source_type,omitempty"`
	SelectedSourceCode        string   `json:"selected_source_code,omitempty"`
	SelectedSourceName        string   `json:"selected_source_name,omitempty"`
	SelectedProvider          string   `json:"selected_provider,omitempty"`
	SelectedAdapter           string   `json:"selected_adapter,omitempty"`
	SelectedUpstreamModel     string   `json:"selected_upstream_model,omitempty"`
	SelectedIndex             *int     `json:"selected_index,omitempty"`
	CandidateCount            int      `json:"candidate_count,omitempty"`
	SkippedCount              int      `json:"skipped_count,omitempty"`
	SkipReasons               []string `json:"skip_reasons,omitempty"`
	PricingSource             string   `json:"pricing_source,omitempty"`
	PricingMode               string   `json:"pricing_mode,omitempty"`
	Settlement                string   `json:"settlement,omitempty"`
	PreDeductPoints           int64    `json:"pre_deduct_points,omitempty"`
	ActualPoints              int64    `json:"actual_points,omitempty"`
	RefundPoints              int64    `json:"refund_points,omitempty"`
	ExtraPoints               int64    `json:"extra_points,omitempty"`
	ModelGatewayRouteSnapshot any      `json:"model_gateway_route_snapshot,omitempty"`
	PricingSnapshot           any      `json:"pricing_snapshot,omitempty"`
	OutputSnapshot            any      `json:"output_snapshot,omitempty"`
	VideoJobSnapshot          any      `json:"video_job_snapshot,omitempty"`
}
