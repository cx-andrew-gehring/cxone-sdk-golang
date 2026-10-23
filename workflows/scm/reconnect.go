package scm

import (
	"context"
	"strings"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scm"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// ReconnectRequest describes the repository a project should be pointed at.
//
// It is the single-project projection of [models.SCMConversionRequest]: the
// conversion API is a batch API, but re-pointing one project is the common
// case and does not need the caller to assemble a slice.
type ReconnectRequest struct {
	// SCMType is the target provider. Required. One of the
	// models.SCMType* constants.
	SCMType string

	// SCMOnPremURL is the self-hosted SCM URL, when applicable. It must
	// exactly match the URL configured for the code repository instance in
	// the Checkmarx One UI.
	SCMOnPremURL string

	// OrgIdentity is the target SCM organization. Required. For Bitbucket
	// on-prem this is the project-key.
	OrgIdentity string

	// Token is the SCM access token. SECRET.
	//
	// Optional: the service reuses an existing project's token when one
	// already exists for this organization, and it is not used at all for
	// githubApp integrations.
	Token string

	// RepoURL is the repository to connect. Required. Case-sensitive, and
	// must not carry a trailing ".git" — the API matches the URL literally
	// and silently fails to find the repo otherwise.
	RepoURL string

	// ProtectedBranches are the branches Checkmarx One treats as protected.
	// Required. Wildcards are allowed; without one, conversion fails if none
	// of the named branches exist.
	ProtectedBranches []string

	// BranchToScanUponCreation is the branch scanned on creation. Required
	// when AutoScan is set.
	BranchToScanUponCreation string

	// Types selects the scanners to run. Values from the
	// models.SCMScanner* constants. Empty leaves the service default.
	Types []string

	// WebhookEnabled creates and enables a webhook for the project.
	WebhookEnabled bool

	// AutoScan runs an initial scan once the project is converted.
	AutoScan bool

	// SCAAutoPREnabled enables automatic SCA remediation pull requests.
	SCAAutoPREnabled bool

	// DecoratePullRequests decorates pull requests with scan results.
	DecoratePullRequests bool

	// SkipDisconnect suppresses the disconnect call.
	//
	// Set this when the project is already a manual project. Callers that
	// have the project model in hand can decide by checking whether
	// ScmRepoID and RepoURL are empty; the workflow deliberately does not
	// fetch the project itself, so that Reconnect costs exactly the two
	// calls it advertises.
	SkipDisconnect bool
}

// Validate checks the request without issuing any HTTP call.
func (r ReconnectRequest) Validate() error {
	if r.SCMType == "" {
		return &cxerrors.ConfigurationError{Field: "SCMType", Reason: "is required"}
	}
	if r.OrgIdentity == "" {
		return &cxerrors.ConfigurationError{Field: "OrgIdentity", Reason: "is required"}
	}
	if r.RepoURL == "" {
		return &cxerrors.ConfigurationError{Field: "RepoURL", Reason: "is required"}
	}
	if strings.HasSuffix(r.RepoURL, ".git") {
		return &cxerrors.ConfigurationError{
			Field:  "RepoURL",
			Reason: "must not end in .git",
		}
	}
	if len(r.ProtectedBranches) == 0 {
		return &cxerrors.ConfigurationError{Field: "ProtectedBranches", Reason: "must not be empty"}
	}
	if r.AutoScan && r.BranchToScanUponCreation == "" {
		return &cxerrors.ConfigurationError{
			Field:  "BranchToScanUponCreation",
			Reason: "is required when AutoScan is true",
		}
	}
	return nil
}

// Disconnect reverts a code repository project to a manual project, in place.
// The project and its scan history are retained; only the repository
// integration is removed.
func Disconnect(ctx context.Context, backend *Backend, projectID string) error {
	if backend == nil || backend.Executor == nil {
		return &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if projectID == "" {
		return &cxerrors.ConfigurationError{Field: "projectID", Reason: "is required"}
	}
	return endpoints.DisconnectProject(ctx, backend.Executor, backend.BaseURL, projectID)
}

// Reconnect points an existing Checkmarx One project at a different
// repository, preserving the project and its scan history.
//
// It disconnects the project from its current code repository integration
// (unless req.SkipDisconnect is set), then submits a single-project conversion
// against the target repo. The returned inspector tracks the resulting
// asynchronous process; pass it to [WaitUntilComplete] to poll it to a
// terminal state.
//
// The two calls are NOT atomic. If the conversion is rejected after the
// disconnect succeeds, the project is left as a manual project — recoverable
// by calling Reconnect again with SkipDisconnect set, but callers should
// surface that state rather than swallowing it.
//
// The service allows only one conversion process per organization at a time,
// so a concurrent Reconnect against the same SCMType and OrgIdentity is
// rejected by the server. Do not retry into that rejection.
//
// Reconnect does not poll, so it never blocks beyond its two HTTP calls.
func Reconnect(ctx context.Context, backend *Backend, projectID string, req ReconnectRequest) (*ConversionInspector, error) {
	if backend == nil || backend.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if projectID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "projectID", Reason: "is required"}
	}
	// Validate everything before the destructive step, so a malformed
	// request cannot strand the project in the manual state.
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if !req.SkipDisconnect {
		if err := endpoints.DisconnectProject(ctx, backend.Executor, backend.BaseURL, projectID); err != nil {
			return nil, err
		}
	}

	conv := &models.SCMConversionRequest{
		SCMType:                          req.SCMType,
		SCMOnPremURL:                     req.SCMOnPremURL,
		OrgIdentity:                      req.OrgIdentity,
		Token:                            req.Token,
		Types:                            req.Types,
		WebhookEnabled:                   req.WebhookEnabled,
		AutoScanCxProjectAfterConversion: req.AutoScan,
		SCAAutoPREnabled:                 req.SCAAutoPREnabled,
		DecoratePullRequests:             req.DecoratePullRequests,
		Projects: []models.SCMConversionProject{{
			CxProjectID:              projectID,
			SCMRepositoryURL:         req.RepoURL,
			ProtectedBranches:        req.ProtectedBranches,
			BranchToScanUponCreation: req.BranchToScanUponCreation,
		}},
	}

	resp, err := endpoints.ConvertProjects(ctx, backend.Executor, backend.BaseURL, conv)
	if err != nil {
		return nil, err
	}
	if resp.ProcessID == "" {
		return nil, &cxerrors.ConfigurationError{
			Field:  "processId",
			Reason: "conversion accepted but returned no process id",
		}
	}

	// Return an unfetched inspector rather than calling FromProcessID: the
	// process may not be queryable in the instant after the POST, and the
	// waiter sleeps before its first Refresh anyway. Status() is "" until
	// then, which classifies as executing.
	return &ConversionInspector{backend: backend, processID: resp.ProcessID}, nil
}
