package models

import "time"

// ApplicationScore is the per-application risk score for one result.
type ApplicationScore struct {
	ApplicationID string  `json:"applicationID"`
	Score         float64 `json:"score"`
}

// RiskManagementApplication is one application name + score row in the
// risk-management response.
type RiskManagementApplication struct {
	ApplicationID   string  `json:"applicationID"`
	ApplicationName string  `json:"applicationName"`
	Score           float64 `json:"score"`
}

// RiskManagementResult is one result with its risk score and enrichment data.
type RiskManagementResult struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	Hash               string             `json:"hash"`
	Type               string             `json:"type"`
	State              string             `json:"state"`
	Engine             string             `json:"engine"`
	Severity           string             `json:"severity"`
	RiskScore          float64            `json:"riskScore"`
	EnrichmentSources  map[string]string  `json:"enrichmentSources"`
	Traits             map[string]string  `json:"traits"`
	CreatedAt          time.Time          `json:"createdAt"`
	ApplicationsScores []ApplicationScore `json:"applicationsScores"`
}

// ASPMResult is the response of GET /api/risk-management/projects/{projectId}/results.
type ASPMResult struct {
	ProjectID            string                      `json:"projectID"`
	ScanID               string                      `json:"scanID"`
	ApplicationNameIDMap []RiskManagementApplication `json:"applicationNameIDMap"`
	Results              []RiskManagementResult      `json:"results"`
}

// APISecResult is the response of GET /api/apisec/static/api/scan/{scanId}/risks-overview.
type APISecResult struct {
	APICount         int                     `json:"api_count,omitempty"`
	TotalRisksCount  int                     `json:"total_risks_count,omitempty"`
	Risks            []int                   `json:"risks,omitempty"`
	RiskDistribution []RiskDistributionEntry `json:"risk_distribution,omitempty"`
}

// RiskDistributionEntry is one (origin, total) pair in the API security
// risk-distribution breakdown.
type RiskDistributionEntry struct {
	Origin string `json:"origin,omitempty"`
	Total  int    `json:"total,omitempty"`
}

// APISecRiskEntry is one paginated API security risk entry.
type APISecRiskEntry struct {
	RiskID        string `json:"risk_id,omitempty"`
	APIID         string `json:"api_id,omitempty"`
	Severity      string `json:"severity,omitempty"`
	Name          string `json:"name,omitempty"`
	Status        string `json:"status,omitempty"`
	HTTPMethod    string `json:"http_method,omitempty"`
	URL           string `json:"url,omitempty"`
	Origin        string `json:"origin,omitempty"`
	Documented    bool   `json:"documented,omitempty"`
	Authenticated *bool  `json:"authenticated,omitempty"`
	DiscoveryDate string `json:"discovery_date,omitempty"`
	ScanID        string `json:"scan_id,omitempty"`
	SastRiskID    string `json:"sast_risk_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	State         string `json:"state,omitempty"`
}

// APISecPaginatedResult is the paginated API security risks response.
type APISecPaginatedResult struct {
	Entries            []APISecRiskEntry `json:"entries,omitempty"`
	TotalRecords       string            `json:"total_records,omitempty"`
	TotalPages         string            `json:"total_pages,omitempty"`
	HasPrevious        bool              `json:"has_previous,omitempty"`
	HasNext            bool              `json:"has_next,omitempty"`
	NextPageNumber     int               `json:"next_page_number,omitempty"`
	PreviousPageNumber *int              `json:"previous_page_number,omitempty"`
}

// FilterParam is one filter clause supported by the risks-overview filtering
// query parameter (column + operator + values).
type FilterParam struct {
	Column   string   `json:"column"`
	Values   []string `json:"values"`
	Operator string   `json:"operator"`
}

// Risk is one entry of GET /api/risks (RiskAPIModel, legacy risks endpoint).
type Risk struct {
	ID                 string `json:"id"`
	RiskName           string `json:"riskName,omitempty"`
	Status             string `json:"status,omitempty"`
	State              string `json:"state,omitempty"`
	Severity           string `json:"severity,omitempty"`
	FirstDetectionDate string `json:"firstDetectionDate,omitempty"`
	Origin             string `json:"origin,omitempty"`
	Source             string `json:"source,omitempty"`
	AssetName          string `json:"assetName,omitempty"`
	SubAssetName       string `json:"subAssetName,omitempty"`
	ProjectID          string `json:"projectId,omitempty"`
	ScanID             string `json:"scanId,omitempty"`
	Engine             string `json:"engine,omitempty"`
	GroupID            string `json:"groupId,omitempty"`
	AssetType          string `json:"assetType,omitempty"`
	StateChangedBy     string `json:"stateChangedBy,omitempty"`
}

// RisksMetaData is the paging metadata of GET /api/risks.
type RisksMetaData struct {
	TotalResults    int `json:"totalResults"`
	FilteredResults int `json:"filteredResults"`
	Page            int `json:"page"`
	PageSize        int `json:"pageSize"`
}

// RisksPage is the response of GET /api/risks.
type RisksPage struct {
	MetaData RisksMetaData `json:"metaData"`
	Risks    []Risk        `json:"risks"`
}
