package dto

type AdminSystemReadinessResp struct {
	RefreshedAt int64                       `json:"refreshed_at"`
	Overall     string                      `json:"overall"`
	Summary     AdminSystemReadinessSummary `json:"summary"`
	Checks      []AdminSystemReadinessCheck `json:"checks"`
}

type AdminSystemReadinessSummary struct {
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Error int `json:"error"`
}

type AdminSystemReadinessCheck struct {
	Category string `json:"category"`
	Key      string `json:"key"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
	Required bool   `json:"required"`
}
