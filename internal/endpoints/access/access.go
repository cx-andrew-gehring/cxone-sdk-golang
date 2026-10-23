// Package access implements the Checkmarx One Access Management API.
package access

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the relative path to the access-management endpoint.
const Path = "api/access-management"

// CreateAssignment grants an entity a role on a resource.
// POST /api/access-management/ → 201.
func CreateAssignment(ctx context.Context, e *transport.Executor, baseURL string, in *models.AssignmentPayload) error {
	return transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/"),
		nil, in, []int{http.StatusCreated}, nil)
}

// EntitiesFor lists the entities (users, groups) that have access to a
// given resource.
// GET /api/access-management/entities-for?resource-id={id}&resource-type={type} → 200.
func EntitiesFor(ctx context.Context, e *transport.Executor, baseURL, resourceID, resourceType string) ([]*models.AssignmentResponse, error) {
	q := url.Values{
		"resource-id":   {resourceID},
		"resource-type": {resourceType},
	}
	var out []*models.AssignmentResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/entities-for"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAssignment returns the single assignment between an entity and a
// resource.
// GET /api/access-management/?entity-id={eid}&resource-id={rid} → 200.
func GetAssignment(ctx context.Context, e *transport.Executor, baseURL, entityID, resourceID string) (*models.AssignmentResponse, error) {
	if entityID == "" || resourceID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "entityID/resourceID", Reason: "both are required"}
	}
	q := url.Values{
		"entity-id":   {entityID},
		"resource-id": {resourceID},
	}
	var out models.AssignmentResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAssignment removes the assignment between an entity and a resource.
// DELETE /api/access-management?entity-id={eid}&resource-id={rid} → 204.
func DeleteAssignment(ctx context.Context, e *transport.Executor, baseURL, entityID, resourceID string) error {
	if entityID == "" || resourceID == "" {
		return &cxerrors.ConfigurationError{Field: "entityID/resourceID", Reason: "both are required"}
	}
	q := url.Values{
		"entity-id":   {entityID},
		"resource-id": {resourceID},
	}
	return transport.DoJSON(ctx, e, http.MethodDelete,
		transport.JoinURL(baseURL, Path),
		q, nil, []int{http.StatusNoContent, http.StatusOK}, nil)
}

// ResourcesFor returns the resources an entity has direct access to.
// GET /api/access-management/resources-for?entity-id={eid}&entity-type={et}&resource-types={types} → 200.
func ResourcesFor(ctx context.Context, e *transport.Executor, baseURL, entityID, entityType string, resourceTypes []string) ([]*models.AssignmentResponse, error) {
	if entityID == "" || entityType == "" {
		return nil, &cxerrors.ConfigurationError{Field: "entityID/entityType", Reason: "both are required"}
	}
	q := url.Values{
		"entity-id":      {entityID},
		"entity-type":    {entityType},
		"resource-types": {strings.Join(resourceTypes, ",")},
	}
	var out []*models.AssignmentResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/resources-for"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// HasAccess checks whether the current caller has permission to perform
// action on the named resource.
// GET /api/access-management/has-access?resource-id=&resource-type=&action= → 200.
func HasAccess(ctx context.Context, e *transport.Executor, baseURL, resourceID, resourceType, action string) (bool, error) {
	q := url.Values{
		"resource-id":   {resourceID},
		"resource-type": {resourceType},
		"action":        {action},
	}
	var out models.HasAccessResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/has-access"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return false, err
	}
	return out.AccessGranted, nil
}

// AccessibleResources returns the resources the current caller can perform
// action on, grouped by resource type.
// GET /api/access-management/get-resources?resource-types=&action= → 200.
func AccessibleResources(ctx context.Context, e *transport.Executor, baseURL string, resourceTypes []string, action string) (*models.AccessibleResourcesResponse, error) {
	q := url.Values{
		"resource-types": {strings.Join(resourceTypes, ",")},
		"action":         {action},
	}
	var out models.AccessibleResourcesResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/get-resources"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
