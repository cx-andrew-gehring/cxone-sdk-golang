package models

import (
	"fmt"
	"log/slog"
)

// SCM provider identifiers accepted by the repos-manager project-conversion
// endpoint's scmType field.
const (
	SCMTypeGitHub    = "github"
	SCMTypeGitLab    = "gitlab"
	SCMTypeAzure     = "azure"
	SCMTypeBitbucket = "bitbucket"
	SCMTypeGitHubApp = "githubApp"
)

// SCMScanner identifiers accepted by the repos-manager project-conversion
// endpoint's types field. These select which engines run when a converted
// project is scanned.
const (
	SCMScannerSAST   = "sast"
	SCMScannerSCA    = "sca"
	SCMScannerKICS   = "kics"
	SCMScannerAPISec = "apisec"
)

// SCMIntegration is one tenant SCM integration entry returned by the
// repos-manager v2 listing.
type SCMIntegration struct {
	ID          uint64 `json:"id,omitempty"`
	Type        string `json:"type,omitempty"`
	RepoBaseURL string `json:"repoBaseUrl,omitempty"`
	AuthBaseURL string `json:"authBaseUrl,omitempty"`
	APIBaseURL  string `json:"apiBaseUrl,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	ClientID    string `json:"clientId,omitempty"`
	Scope       string `json:"scope,omitempty"`
	OnPrem      bool   `json:"onPrem,omitempty"`
	RepoCount   int    `json:"repoCount,omitempty"`
}

// SCMRepository is the per-repository configuration entry returned by the
// repos-manager repo lookup. The Branches and Scm sub-shapes are kept loose
// (any-typed) for forward-compatibility — the wire format has been evolving.
type SCMRepository struct {
	ID                             string                `json:"id,omitempty"`
	URL                            string                `json:"url,omitempty"`
	WebhookID                      string                `json:"webhookId,omitempty"`
	WebhookEnabled                 bool                  `json:"webhookEnabled,omitempty"`
	PrDecorationEnabled            bool                  `json:"prDecorationEnabled,omitempty"`
	IsRepoAdmin                    bool                  `json:"isRepoAdmin,omitempty"`
	SastIncrementalScan            bool                  `json:"sastIncrementalScan,omitempty"`
	SastScannerEnabled             bool                  `json:"sastScannerEnabled,omitempty"`
	ScaScannerEnabled              bool                  `json:"scaScannerEnabled,omitempty"`
	KicsScannerEnabled             bool                  `json:"kicsScannerEnabled,omitempty"`
	APISecScannerEnabled           bool                  `json:"apiSecScannerEnabled,omitempty"`
	ContainerScannerEnabled        bool                  `json:"containerScannerEnabled,omitempty"`
	OssfScoreCardScannerEnabled    bool                  `json:"ossfScoreCardScannerEnabled,omitempty"`
	SecretsDetectionScannerEnabled bool                  `json:"secretsDetectionScannerEnabled,omitempty"`
	PrivatePackage                 bool                  `json:"privatePackage,omitempty"`
	Criticality                    int                   `json:"criticality,omitempty"`
	Branches                       []SCMRepositoryBranch `json:"branches,omitempty"`
	RemediationSeverities          []any                 `json:"remediationSeverities,omitempty"`
	SSHRepoURL                     string                `json:"sshRepoUrl,omitempty"`
	ScmID                          int                   `json:"scmId,omitempty"`
}

// SCMRepositoryBranch is one branch entry on an SCM repository.
type SCMRepositoryBranch struct {
	Pattern         string         `json:"pattern,omitempty"`
	IsDefaultBranch bool           `json:"isDefaultBranch,omitempty"`
	Tags            map[string]any `json:"tags,omitempty"`
}

// SCMConversionRequest is the body of the repos-manager project-conversion
// endpoint. It converts one or more existing manual Checkmarx One projects
// into Code Repository projects.
//
// A single conversion process can only connect to repos within a single
// organization, identified by SCMType plus OrgIdentity, and only one
// conversion process may run per organization at a time.
//
// The struct's process-level booleans deliberately omit the `omitempty` tag
// option used elsewhere in this file: omitempty drops false, which would make
// it impossible to explicitly request "no webhook" or "do not auto-scan". The
// API's own documented example sends autoScanCxProjectAfterConversion:false
// explicitly.
type SCMConversionRequest struct {
	// SCMType is the provider identifier. Required. One of the SCMType*
	// constants in this package.
	SCMType string `json:"scmType"`

	// SCMOnPremURL is the URL of a self-hosted SCM environment. Only used
	// for the self-hosted flow, in which case it must exactly match the URL
	// configured for the code repository instance in the Checkmarx One UI.
	SCMOnPremURL string `json:"scmOnPremUrl,omitempty"`

	// OrgIdentity is the name of the SCM organization holding the repos.
	// Required. For Bitbucket on-prem repos this is the project-key rather
	// than an organization name.
	OrgIdentity string `json:"orgIdentity,omitempty"`

	// Token is the SCM access token used to connect to the organization.
	//
	// SECRET. Never log this field directly — SCMConversionRequest
	// implements fmt.Stringer and slog.LogValuer to redact it.
	//
	// Optional: if a Code Repository project already exists for any repo in
	// this organization the service reuses that project's token. It is also
	// not relevant for githubApp integrations. Some Azure self-hosted
	// instances have a gateway that verifies user-token correlation; those
	// require the "username:PAT" syntax.
	Token string `json:"token,omitempty"`

	// Types selects which scanners run when the converted projects are
	// scanned. Values come from the SCMScanner* constants. Individual
	// projects may override this via SCMConversionProject.Types.
	Types []string `json:"types,omitempty"`

	// WebhookEnabled creates and enables a webhook for each converted
	// project.
	WebhookEnabled bool `json:"webhookEnabled"`

	// AutoScanCxProjectAfterConversion runs an initial scan on each
	// converted project. When true, every project entry must set
	// BranchToScanUponCreation.
	AutoScanCxProjectAfterConversion bool `json:"autoScanCxProjectAfterConversion"`

	// SCAAutoPREnabled lets Checkmarx automatically raise pull requests that
	// remediate SCA vulnerabilities by bumping to fixed package versions.
	SCAAutoPREnabled bool `json:"scaAutoPrEnabled"`

	// DecoratePullRequests decorates pull requests with a summary of the
	// Checkmarx One scan results.
	DecoratePullRequests bool `json:"decoratePullRequests"`

	// Projects lists the projects being converted. Required, non-empty.
	Projects []SCMConversionProject `json:"projects"`
}

// String implements fmt.Stringer with the Token field redacted, so that a
// SCMConversionRequest accidentally interpolated into an error or log line
// cannot leak the caller's SCM access token (CLAUDE.md §7).
func (r SCMConversionRequest) String() string {
	token := ""
	if r.Token != "" {
		token = "REDACTED"
	}
	return fmt.Sprintf("SCMConversionRequest{SCMType:%s OrgIdentity:%s Token:%s Projects:%d}",
		r.SCMType, r.OrgIdentity, token, len(r.Projects))
}

// LogValue implements slog.LogValuer so structured logging also redacts the
// token rather than reflecting over the struct fields.
func (r SCMConversionRequest) LogValue() slog.Value {
	token := ""
	if r.Token != "" {
		token = "REDACTED"
	}
	return slog.GroupValue(
		slog.String("scmType", r.SCMType),
		slog.String("orgIdentity", r.OrgIdentity),
		slog.String("token", token),
		slog.Int("projects", len(r.Projects)),
	)
}

// SCMConversionProject describes one project taking part in a conversion.
//
// The pointer-typed booleans are deliberate: the API documents them as
// per-project overrides of the process-level values on SCMConversionRequest,
// so nil ("inherit the process-level value") must be distinguishable from
// false ("explicitly off for this project").
type SCMConversionProject struct {
	// CxProjectID is the id of the existing manual Checkmarx One project.
	CxProjectID string `json:"cxProjectId,omitempty"`

	// SCMRepositoryURL is the repo being connected to the project.
	// Case-sensitive, and must not include a trailing ".git".
	SCMRepositoryURL string `json:"scmRepositoryUrl,omitempty"`

	// ProtectedBranches are the branches Checkmarx One treats as protected.
	// Required. Wildcard patterns are allowed; if none of the listed
	// branches exist in the SCM and no wildcard is used, conversion fails
	// for this project.
	//
	// This endpoint assigns protected branches but cannot tag them; use the
	// dedicated add-protected-branches endpoint for tags.
	ProtectedBranches []string `json:"protectedBranches"`

	// BranchToScanUponCreation is the branch scanned when the project is
	// created. Required when the process sets
	// AutoScanCxProjectAfterConversion.
	BranchToScanUponCreation string `json:"branchToScanUponCreation,omitempty"`

	// Types overrides SCMConversionRequest.Types for this project.
	Types []string `json:"types,omitempty"`

	// WebhookEnabled overrides SCMConversionRequest.WebhookEnabled.
	WebhookEnabled *bool `json:"webhookEnabled,omitempty"`

	// SCAAutoPREnabled overrides SCMConversionRequest.SCAAutoPREnabled.
	SCAAutoPREnabled *bool `json:"scaAutoPrEnabled,omitempty"`

	// DecoratePullRequests overrides
	// SCMConversionRequest.DecoratePullRequests.
	DecoratePullRequests *bool `json:"decoratePullRequests,omitempty"`
}

// SCMConversionResponse is returned when a conversion process is accepted.
// ProcessID is the handle used to poll the process to completion.
type SCMConversionResponse struct {
	ProcessID string `json:"processId,omitempty"`
	Message   string `json:"message,omitempty"`
}

// SCMConversionStatus is the current state of a conversion process.
//
// Note the legacy "migrationStatus" wire name on what is otherwise a
// conversion API; the Go field is named Status.
//
// The service retains conversion process data for 7 days only. Polling a
// process id older than that fails rather than returning a terminal state.
type SCMConversionStatus struct {
	Status                 string   `json:"migrationStatus,omitempty"`
	Summary                string   `json:"summary,omitempty"`
	TotalProjects          int      `json:"totalProjects,omitempty"`
	MigratedProjects       int      `json:"migratedProjects,omitempty"`
	SuccessfulProjectsList []string `json:"successfulProjectsList,omitempty"`
	FailedProjectList      []string `json:"failedProjectList,omitempty"`
}
