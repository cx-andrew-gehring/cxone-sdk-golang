// Package tenant implements the Checkmarx One tenant configuration endpoint.
package tenant

import (
	"context"
	"net/http"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the relative path to the tenant configuration endpoint.
const Path = "api/configuration/tenant"

// GetConfiguration returns all tenant-level configuration entries visible to
// the caller. GET /api/configuration/tenant → 200.
func GetConfiguration(ctx context.Context, e *transport.Executor, baseURL string) ([]*models.TenantConfigurationEntry, error) {
	var out []*models.TenantConfigurationEntry
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
