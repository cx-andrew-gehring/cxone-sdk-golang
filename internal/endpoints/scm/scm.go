// Package scm implements the Checkmarx One repos-manager endpoints for SCM
// integrations and repository configuration.
//
// Part of the SDK's low-level endpoint layer (CLAUDE.md §8).
package scm

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
)

// Path constants for the repos-manager endpoints.
const (
	IntegrationsPath = "api/repos-manager/v2/scms"
	RepoPath         = "api/repos-manager/repo"
	ProjectsPath     = "api/repos-manager/projects"
	// ProjectConversionPath serves both the POST that starts a conversion
	// and the GET that polls one.
	ProjectConversionPath = "api/repos-manager/project-conversion"
)

// validSCMTypes is the set accepted by the project-conversion endpoint's
// scmType field. Validated client-side because a typo here would otherwise
// only surface after a caller has already disconnected a project.
var validSCMTypes = map[string]struct{}{
	models.SCMTypeGitHub:    {},
	models.SCMTypeGitLab:    {},
	models.SCMTypeAzure:     {},
	models.SCMTypeBitbucket: {},
	models.SCMTypeGitHubApp: {},
}

// ListIntegrations returns every SCM integration configured for the tenant.
// The fields=repoCount query parameter is included so the response includes
// per-integration repository counts.
// GET /api/repos-manager/v2/scms?fields=repoCount → 200.
func ListIntegrations(ctx context.Context, e *transport.Executor, baseURL string) ([]models.SCMIntegration, error) {
	q := url.Values{"fields": {"repoCount"}}
	var out []models.SCMIntegration
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, IntegrationsPath),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRepository returns the per-repository configuration for the supplied
// repo id.
// GET /api/repos-manager/repo/{id} → 200.
func GetRepository(ctx context.Context, e *transport.Executor, baseURL string, repositoryID uint64) (*models.SCMRepository, error) {
	if repositoryID == 0 {
		return nil, &cxerrors.ConfigurationError{Field: "repositoryID", Reason: "must be > 0"}
	}
	var out models.SCMRepository
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, RepoPath+"/"+strconv.FormatUint(repositoryID, 10)),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DisconnectProject removes a project's code repository integration,
// reverting it to a manual project in place. Scan history is retained.
//
// The request carries no body.
// POST /api/repos-manager/projects/{projectId}/disconnect → 200/202/204.
func DisconnectProject(ctx context.Context, e *transport.Executor, baseURL, projectID string) error {
	if projectID == "" {
		return &cxerrors.ConfigurationError{Field: "projectID", Reason: "is required"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, ProjectsPath+"/"+url.PathEscape(projectID)+"/disconnect"),
		nil, nil, []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent}, nil)
}

// ConvertProjects connects one or more existing manual projects to repos,
// turning them into Code Repository projects. The call is asynchronous: it
// returns a process id that must be polled via [GetConversionStatus].
//
// Every project in one request must live under the same organization
// (req.SCMType plus req.OrgIdentity), and the service permits only one
// conversion process per organization at a time — a second concurrent request
// for the same organization is rejected by the server.
// POST /api/repos-manager/project-conversion → 200/201/202.
func ConvertProjects(ctx context.Context, e *transport.Executor, baseURL string, req *models.SCMConversionRequest) (*models.SCMConversionResponse, error) {
	if req == nil {
		return nil, &cxerrors.ConfigurationError{Field: "request", Reason: "is required"}
	}
	if req.SCMType == "" {
		return nil, &cxerrors.ConfigurationError{Field: "scmType", Reason: "is required"}
	}
	if _, ok := validSCMTypes[req.SCMType]; !ok {
		return nil, &cxerrors.ConfigurationError{
			Field:  "scmType",
			Reason: "must be one of github, gitlab, azure, bitbucket, githubApp",
		}
	}
	if req.OrgIdentity == "" {
		return nil, &cxerrors.ConfigurationError{Field: "orgIdentity", Reason: "is required"}
	}
	if len(req.Projects) == 0 {
		return nil, &cxerrors.ConfigurationError{Field: "projects", Reason: "must not be empty"}
	}
	for i := range req.Projects {
		if len(req.Projects[i].ProtectedBranches) == 0 {
			return nil, &cxerrors.ConfigurationError{
				Field:  "projects[" + strconv.Itoa(i) + "].protectedBranches",
				Reason: "must not be empty",
			}
		}
		// The API rejects the combination server-side, but only after the
		// caller may already have disconnected the project — catch it here.
		if req.AutoScanCxProjectAfterConversion && req.Projects[i].BranchToScanUponCreation == "" {
			return nil, &cxerrors.ConfigurationError{
				Field:  "projects[" + strconv.Itoa(i) + "].branchToScanUponCreation",
				Reason: "is required when autoScanCxProjectAfterConversion is true",
			}
		}
	}

	var out models.SCMConversionResponse
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, ProjectConversionPath),
		nil, req, []int{http.StatusOK, http.StatusCreated, http.StatusAccepted}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetConversionStatus returns the current state of a conversion process.
//
// The service retains process data for 7 days only; polling an older process
// id fails rather than returning a terminal state.
// GET /api/repos-manager/project-conversion?processId={id} → 200.
func GetConversionStatus(ctx context.Context, e *transport.Executor, baseURL, processID string) (*models.SCMConversionStatus, error) {
	if processID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "processID", Reason: "is required"}
	}
	q := url.Values{"processId": {processID}}
	var out models.SCMConversionStatus
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, ProjectConversionPath),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
