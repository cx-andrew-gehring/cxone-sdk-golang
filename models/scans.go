package models

import (
	"encoding/json"
	"time"
)

// Scan-state strings returned by the Checkmarx One API. They are constants —
// the high-level workflow layer's ScanInspector compares Status against
// these to derive Executing/Successful/Failed buckets per CLAUDE.md §9.4.
const (
	ScanQueued    = "Queued"
	ScanRunning   = "Running"
	ScanCompleted = "Completed"
	ScanFailed    = "Failed"
	ScanCanceled  = "Canceled"
	ScanPartial   = "Partial"
)

// ScanStatus is a type-safe wrapper around the wire-side scan status string.
// Compare against the Scan* constants above; do not invent new values.
type ScanStatus string

// CancelScanModel is the body posted to PATCH /api/scans/{id} to cancel a
// running scan.
type CancelScanModel struct {
	Status ScanStatus `json:"status"`
}

// ScanTaskResponseModel is one entry in the scan workflow timeline returned
// by GET /api/scans/{id}/workflow.
type ScanTaskResponseModel struct {
	Source    string `json:"source"`
	Timestamp string `json:"timestamp"`
	Info      string `json:"info"`
}

// Config is a single key/value entry in the scan metadata configs list.
// The protobuf tags are vestigial — they come from the Checkmarx One scan
// service's gRPC origin and are kept here only because the JSON layout
// inherits the same field shapes.
type Config struct {
	Type  string                 `json:"type,omitempty"`
	Value map[string]interface{} `json:"value,omitempty"`
}

// ScanHandler is the upload-mode handler payload for a Scan request.
//
// RepoURL and Credentials are omitted from the wire when empty (the spec's
// upload handler carries only uploadUrl, optional branch and uploadFormat);
// UploadFormat is "single" for an SBOM upload.
type ScanHandler struct {
	Branch       string         `json:"branch,omitempty"`
	RepoURL      string         `json:"repoUrl,omitempty"`
	UploadURL    string         `json:"uploadUrl"`
	UploadFormat string         `json:"uploadFormat,omitempty"`
	Credentials  GitCredentials `json:"credentials"`
}

// MarshalJSON omits the zero-valued Credentials struct (encoding/json's
// omitempty cannot do this for a non-pointer struct) while keeping the
// exported field type unchanged.
func (h ScanHandler) MarshalJSON() ([]byte, error) {
	type wire struct {
		Branch       string          `json:"branch,omitempty"`
		RepoURL      string          `json:"repoUrl,omitempty"`
		UploadURL    string          `json:"uploadUrl"`
		UploadFormat string          `json:"uploadFormat,omitempty"`
		Credentials  *GitCredentials `json:"credentials,omitempty"`
	}
	w := wire{Branch: h.Branch, RepoURL: h.RepoURL, UploadURL: h.UploadURL, UploadFormat: h.UploadFormat}
	if h.Credentials != (GitCredentials{}) {
		c := h.Credentials
		w.Credentials = &c
	}
	return json.Marshal(w)
}

// GitProjectHandler is the git-mode handler payload for a Scan request.
type GitProjectHandler struct {
	RepoURL     string         `json:"repoUrl"`
	Branch      string         `json:"branch,omitempty"`
	Commit      string         `json:"commit,omitempty"`
	Tag         string         `json:"tag,omitempty"`
	Credentials GitCredentials `json:"credentials"`
}

// GitCredentials carries credentials for accessing a git repository. Type is
// one of: "apiKey", "password", "ssh", "JWT".
type GitCredentials struct {
	Username string `json:"username,omitempty"`
	Type     string `json:"type"`
	Value    string `json:"value,omitempty"`
}

// StatusInfo is one engine-level status entry inside a [ScanResponseModel].
// The high-level inspector walks these when ScanResponseModel.Status is
// "Partial" to derive the executing/successful/failed bucket per engine.
type StatusInfo struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Details   string `json:"details,omitempty"`
	ErrorCode int    `json:"errorCode,omitempty"`
}

// ScanResponseModel is the server-returned representation of a single scan.
type ScanResponseModel struct {
	ID              string                    `json:"id"`
	Status          ScanStatus                `json:"status"`
	PositionInQueue *uint                     `json:"positionInQueue,omitempty"`
	StatusDetails   []StatusInfo              `json:"statusDetails,omitempty"`
	Branch          string                    `json:"branch"`
	CreatedAt       time.Time                 `json:"createdAt"`
	UpdatedAt       time.Time                 `json:"updatedAt"`
	ProjectID       string                    `json:"projectId"`
	ProjectName     string                    `json:"projectName"`
	UserAgent       string                    `json:"userAgent"`
	Initiator       string                    `json:"initiator"`
	Tags            map[string]string         `json:"tags"`
	Metadata        ScanResponseModelMetadata `json:"metadata"`
	Engines         []string                  `json:"engines"`
	SourceType      string                    `json:"sourceType"`
	SourceOrigin    string                    `json:"sourceOrigin"`
	SastIncremental string                    `json:"sastIncremental"`
	Timeout         string                    `json:"timeout"`
}

// ScanResponseModelMetadata wraps the engine-config snapshot returned with a
// scan response.
type ScanResponseModelMetadata struct {
	Configs []Config `json:"configs"`
}

// ScansCollectionResponseModel is the paginated list-scans response.
type ScansCollectionResponseModel struct {
	TotalCount         uint                `json:"totalCount"`
	FilteredTotalCount uint                `json:"filteredTotalCount"`
	Scans              []ScanResponseModel `json:"scans"`
}

// ScanProject is the project-handle nested inside a Scan request.
//
// Tags is omitted when nil/empty: per the spec, project.tags on a scan
// request OVERWRITES the project's tags, so it is only sent on purpose.
type ScanProject struct {
	ID   string            `json:"id"`
	Tags map[string]string `json:"tags,omitempty"`
}

// Scan is the create-scan request body.
//
// Type is "git" or "upload". Handler is a raw JSON message that, depending
// on Type, decodes as either [GitProjectHandler] or [ScanHandler]. Use one
// of the helper constructors below for type-safe construction.
type Scan struct {
	Type    string            `json:"type"`
	Handler json.RawMessage   `json:"handler"`
	Project ScanProject       `json:"project,omitempty"`
	Config  []Config          `json:"config,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
}

// SastConfig holds the SAST engine config for a scan request. Booleans are
// strings on the wire because the Checkmarx One scan service uses
// stringified flags throughout.
type SastConfig struct {
	Incremental           string `json:"incremental,omitempty"`
	Filter                string `json:"filter,omitempty"`
	EngineVerbose         string `json:"engineVerbose,omitempty"`
	LanguageMode          string `json:"languageMode,omitempty"`
	PresetName            string `json:"presetName,omitempty"`
	FastScanMode          string `json:"fastScanMode,omitempty"`
	LightQueries          string `json:"lightQueries,omitempty"`
	RecommendedExclusions string `json:"recommendedExclusions,omitempty"`
}

// AiConfig holds the "ai" scan config entry (Checkmarx Fusion). Values are
// strings on the wire. Model names are not validated here; Checkmarx One
// does that.
type AiConfig struct {
	FusionEnabled string `json:"fusionEnabled,omitempty"`
	LLMModel      string `json:"llmModel,omitempty"`
}

// Config converts the AiConfig to a config[] entry of type "ai", omitting
// empty fields.
func (c AiConfig) Config() Config {
	v := map[string]any{}
	if c.FusionEnabled != "" {
		v["fusionEnabled"] = c.FusionEnabled
	}
	if c.LLMModel != "" {
		v["llmModel"] = c.LLMModel
	}
	return Config{Type: "ai", Value: v}
}

// KicsConfig holds the KICS (IaC) engine config.
type KicsConfig struct {
	Filter    string `json:"filter,omitempty"`
	Platforms string `json:"platforms,omitempty"`
	PresetID  string `json:"presetId,omitempty"`
}

// ScaConfig holds the SCA engine config.
type ScaConfig struct {
	Filter                string `json:"filter,omitempty"`
	ExploitablePath       string `json:"ExploitablePath,omitempty"`
	LastSastScanTime      string `json:"LastSastScanTime,omitempty"`
	PrivatePackageVersion string `json:"privatePackageVersion,omitempty"`
	EnableContainersScan  bool   `json:"enableContainersScan,omitempty"`
	SBom                  string `json:"sbom,omitempty"`
}

// ContainerConfig holds the container-scanning engine config.
type ContainerConfig struct {
	FilesFilter          string `json:"filesFilter,omitempty"`
	ImagesFilter         string `json:"imagesFilter,omitempty"`
	PackagesFilter       string `json:"packagesFilter,omitempty"`
	NonFinalStagesFilter string `json:"nonFinalStagesFilter,omitempty"`
	UserCustomImages     string `json:"userCustomImages,omitempty"`
}

// APISecConfig holds the API security engine config.
type APISecConfig struct {
	SwaggerFilter string `json:"swaggerFilter,omitempty"`
}

// SCSConfig holds the source-code security (secrets / scorecard / 2ms) config.
type SCSConfig struct {
	Twoms            string `json:"2ms,omitempty"`
	Scorecard        string `json:"scorecard,omitempty"`
	RepoURL          string `json:"repoUrl,omitempty"`
	RepoToken        string `json:"repoToken,omitempty"`
	GitCommitHistory string `json:"gitCommitHistory,omitempty"`
}
