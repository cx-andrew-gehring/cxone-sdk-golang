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
)

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

// DisconnectProject detaches a Code Repository Integration project from its
// SCM repository, converting it to a manual project. History is preserved and
// any configured webhook is removed.
// POST /api/repos-manager/projects/{projectId}/disconnect → 200.
func DisconnectProject(ctx context.Context, e *transport.Executor, baseURL, projectID string) error {
	if projectID == "" {
		return &cxerrors.ConfigurationError{Field: "projectID", Reason: "must not be empty"}
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, ProjectsPath+"/"+projectID+"/disconnect"),
		nil, nil, []int{http.StatusOK}, nil)
}