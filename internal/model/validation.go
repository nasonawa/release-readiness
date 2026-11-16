package model

// Validation is a required-job denominator, independent of observed runs.
type Validation struct {
	PlanKnown   bool            `json:"plan_known"`
	Revision    string          `json:"revision"`
	SourceFresh bool            `json:"source_fresh"`
	Ready       bool            `json:"ready"`
	Total       int             `json:"total"`
	Passed      int             `json:"passed"`
	Failed      int             `json:"failed"`
	Running     int             `json:"running"`
	Missing     int             `json:"missing"`
	Rows        []ValidationRow `json:"rows,omitempty"`
}

type ValidationRow struct {
	Unconfigured bool   `json:"unconfigured,omitempty"`
	Removed      bool   `json:"removed,omitempty"`
	Job          string `json:"job"`
	OCP          string `json:"ocp"`
	Cloud        string `json:"cloud"`
	Storage      string `json:"storage"`
	Architecture string `json:"architecture"`
	Mode         string `json:"mode"`
	SourceURL    string `json:"source_url"`
	State        string `json:"state"`
	RunURL       string `json:"run_url,omitempty"`
	Evidence     string `json:"evidence,omitempty"`
}

// PortfolioFacts is an additive overview projection, not a replacement for
// the FBC preparation verdict. Ready requires BOTH preparation and validation.
type PortfolioFacts struct {
	Stage      string     `json:"stage"`
	Verdict    Verdict    `json:"verdict"`
	Validation Validation `json:"validation"`
	Error      string     `json:"error,omitempty"`
}
