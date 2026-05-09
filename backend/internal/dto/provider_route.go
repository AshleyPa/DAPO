package dto

type ProviderHealthSummaryResp struct {
	RefreshedAt int64                        `json:"refreshed_at"`
	Providers   []ProviderHealthProviderResp `json:"providers"`
}

type ProviderHealthProviderResp struct {
	Provider        string                    `json:"provider"`
	Total           int64                     `json:"total"`
	Enabled         int64                     `json:"enabled"`
	Disabled        int64                     `json:"disabled"`
	Broken          int64                     `json:"broken"`
	Banned          int64                     `json:"banned"`
	Available       int64                     `json:"available"`
	CooldownActive  int64                     `json:"cooldown_active"`
	TokenExpired    int64                     `json:"token_expired"`
	LastTestOK      int64                     `json:"last_test_ok"`
	LastTestFail    int64                     `json:"last_test_fail"`
	LastTestUnknown int64                     `json:"last_test_unknown"`
	QuotaZero       int64                     `json:"quota_zero"`
	SuccessCount    int64                     `json:"success_count"`
	ErrorCount      int64                     `json:"error_count"`
	AuthTypes       []ProviderHealthAuthResp  `json:"auth_types"`
	RecentErrors    []ProviderHealthErrorResp `json:"recent_errors"`
}

type ProviderHealthAuthResp struct {
	AuthType       string `json:"auth_type"`
	Total          int64  `json:"total"`
	Available      int64  `json:"available"`
	CooldownActive int64  `json:"cooldown_active"`
	LastTestOK     int64  `json:"last_test_ok"`
	LastTestFail   int64  `json:"last_test_fail"`
}

type ProviderHealthErrorResp struct {
	AccountID            uint64 `json:"account_id"`
	Name                 string `json:"name"`
	AuthType             string `json:"auth_type"`
	Status               int8   `json:"status"`
	ErrorCount           int    `json:"error_count"`
	LastError            string `json:"last_error,omitempty"`
	LastTestError        string `json:"last_test_error,omitempty"`
	LastTestAt           int64  `json:"last_test_at,omitempty"`
	CooldownUntil        int64  `json:"cooldown_until,omitempty"`
	AccessTokenExpiresAt int64  `json:"access_token_expires_at,omitempty"`
	UpdatedAt            int64  `json:"updated_at"`
}

// ProviderRouteTestReq asks the admin API to dry-run model routing.
type ProviderRouteTestReq struct {
	Kind             string `json:"kind" binding:"required,oneof=image text video chat"`
	ModelCode        string `json:"model_code" binding:"required,min=1,max=128"`
	FallbackProvider string `json:"fallback_provider" binding:"omitempty,oneof=gpt grok"`
}

// ProviderRouteTestResp explains the effective provider route without
// touching upstream services or account credentials.
type ProviderRouteTestResp struct {
	Kind              string                       `json:"kind"`
	ModelCode         string                       `json:"model_code"`
	FallbackProvider  string                       `json:"fallback_provider"`
	Provider          string                       `json:"provider"`
	UpstreamModel     string                       `json:"upstream_model"`
	AuthType          string                       `json:"auth_type,omitempty"`
	Strategy          string                       `json:"strategy"`
	MatchedConfig     bool                         `json:"matched_config"`
	MatchedKind       string                       `json:"matched_kind,omitempty"`
	MatchedModelCode  string                       `json:"matched_model_code,omitempty"`
	FallbackReason    string                       `json:"fallback_reason,omitempty"`
	CandidateAccounts int                          `json:"candidate_accounts"`
	AvailableAccounts int                          `json:"available_accounts"`
	Warning           string                       `json:"warning,omitempty"`
	Candidates        []ProviderRouteCandidateResp `json:"candidates,omitempty"`
}

type ProviderRouteCandidateResp struct {
	Index             int    `json:"index"`
	Provider          string `json:"provider"`
	UpstreamModel     string `json:"upstream_model"`
	AuthType          string `json:"auth_type,omitempty"`
	Strategy          string `json:"strategy"`
	CandidateAccounts int    `json:"candidate_accounts"`
	AvailableAccounts int    `json:"available_accounts"`
	Warning           string `json:"warning,omitempty"`
}
