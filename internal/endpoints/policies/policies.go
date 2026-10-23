// Package policies implements the Checkmarx One Policies API (low-level layer).
package policies

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the relative path to the policy evaluation endpoint.
const Path = "api/policy_management_service_uri/evaluation"

// Service-routing prefix used by all policy-management endpoints.
const servicePrefix = "api/policy_management_service_uri"

// Evaluate fetches the policy evaluation result for a given scan.
// GET /api/policy_management_service_uri/evaluation?scan-id={id} → 200.
//
// The path includes a service-routing prefix that the platform uses to
// dispatch the request to the policy management service.
func Evaluate(ctx context.Context, e *transport.Executor, baseURL, scanID string) (*models.PolicyResponseModel, error) {
	q := url.Values{"scan-id": {scanID}}
	var out models.PolicyResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns one page of policies. The caller supplies Limit and Page.
// GET /api/policy_management_service_uri/policies/v2 → 200.
func List(ctx context.Context, e *transport.Executor, baseURL string, filter models.PolicyFilter) (*models.PolicyListResponse, error) {
	q := url.Values{}
	if filter.Limit > 0 {
		q.Set("limit", strconv.FormatUint(filter.Limit, 10))
	}
	if filter.Page > 0 {
		q.Set("page", strconv.FormatUint(filter.Page, 10))
	}
	var out models.PolicyListResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, servicePrefix+"/policies/v2"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CountFiltered returns the total number of policies matching filter.
func CountFiltered(ctx context.Context, e *transport.Executor, baseURL string, filter models.PolicyFilter) (uint64, error) {
	filter.Limit = 1
	resp, err := List(ctx, e, baseURL, filter)
	if err != nil {
		return 0, err
	}
	return resp.FilteredTotalCount, nil
}

// ListViolations returns one page of policy violations ("incidents").
// GET /api/policy_management_service_uri/incidents/filters → 200.
func ListViolations(ctx context.Context, e *transport.Executor, baseURL string, filter models.PolicyViolationFilter) (*models.PolicyViolationListResponse, error) {
	q := url.Values{}
	if filter.Limit > 0 {
		q.Set("limit", strconv.FormatUint(filter.Limit, 10))
	}
	if filter.Page > 0 {
		q.Set("page", strconv.FormatUint(filter.Page, 10))
	}
	if filter.ProjectID != "" {
		q.Set("astProjectId", filter.ProjectID)
	}
	if filter.ScanID != "" {
		q.Set("scanId", filter.ScanID)
	}
	if filter.PolicyID != "" {
		q.Set("policyId", filter.PolicyID)
	}
	var out models.PolicyViolationListResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, servicePrefix+"/incidents/filters"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CountViolationsFiltered returns the total violation count matching filter.
func CountViolationsFiltered(ctx context.Context, e *transport.Executor, baseURL string, filter models.PolicyViolationFilter) (uint64, error) {
	filter.Limit = 1
	resp, err := ListViolations(ctx, e, baseURL, filter)
	if err != nil {
		return 0, err
	}
	return resp.FilteredTotalCount, nil
}

// GetViolationDetails returns the per-scan violation details.
// GET /api/policy_management_service_uri/evaluation?astProjectId=&scanId= → 200.
func GetViolationDetails(ctx context.Context, e *transport.Executor, baseURL, projectID, scanID string) (*models.PolicyViolationDetails, error) {
	if projectID == "" || scanID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "projectID/scanID", Reason: "both are required"}
	}
	q := url.Values{}
	q.Set("astProjectId", projectID)
	q.Set("scanId", scanID)
	var out models.PolicyViolationDetails
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, servicePrefix+"/evaluation"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AssignToProjects attaches a policy to one or more projects.
// POST /api/policy_management_service_uri/policies/projects/{policyID} → 200.
//
// Each supplied project must have a Name and ID populated (the Checkmarx One
// service validates against both).
func AssignToProjects(ctx context.Context, e *transport.Executor, baseURL, policyID string, projects []models.ProjectResponseModel) error {
	if policyID == "" {
		return &cxerrors.ConfigurationError{Field: "policyID", Reason: "is required"}
	}
	type policyProject struct {
		Name         string `json:"Name"`
		AstProjectID string `json:"AstProjectId"`
	}
	body := struct {
		ID       string          `json:"id"`
		Projects []policyProject `json:"projects"`
	}{ID: policyID}
	for _, p := range projects {
		body.Projects = append(body.Projects, policyProject{Name: p.Name, AstProjectID: p.ID})
	}
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, servicePrefix+"/policies/projects/"+policyID),
		nil, body, []int{http.StatusOK, http.StatusCreated, http.StatusNoContent}, nil)
}
