package dto

// ProviderRouteTestReq asks the admin API to dry-run model routing.
type ProviderRouteTestReq struct {
	Kind             string `json:"kind" binding:"required,oneof=image text video chat"`
	ModelCode        string `json:"model_code" binding:"required,min=1,max=128"`
	FallbackProvider string `json:"fallback_provider" binding:"omitempty,oneof=gpt grok"`
}

// ProviderRouteTestResp explains the effective provider route without
// touching upstream services or account credentials.
type ProviderRouteTestResp struct {
	Kind              string `json:"kind"`
	ModelCode         string `json:"model_code"`
	FallbackProvider  string `json:"fallback_provider"`
	Provider          string `json:"provider"`
	UpstreamModel     string `json:"upstream_model"`
	AuthType          string `json:"auth_type,omitempty"`
	Strategy          string `json:"strategy"`
	MatchedConfig     bool   `json:"matched_config"`
	MatchedKind       string `json:"matched_kind,omitempty"`
	MatchedModelCode  string `json:"matched_model_code,omitempty"`
	FallbackReason    string `json:"fallback_reason,omitempty"`
	CandidateAccounts int    `json:"candidate_accounts"`
	AvailableAccounts int    `json:"available_accounts"`
	Warning           string `json:"warning,omitempty"`
}
