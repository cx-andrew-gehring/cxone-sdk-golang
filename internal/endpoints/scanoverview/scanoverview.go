// Package scanoverview implements the Checkmarx One SCS (source-code
// security) scan-overview endpoint.
package scanoverview

import (
	"context"
	"net/http"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// PathTemplate is the relative path with the {scanId} placeholder.
const PathTemplate = "api/micro-engines/read/scans/%s/scan-overview"

// Get returns the SCS overview for a single scan.
// GET /api/micro-engines/read/scans/{scanId}/scan-overview → 200.
func Get(ctx context.Context, e *transport.Executor, baseURL, scanID string) (*models.SCSOverview, error) {
	path := "api/micro-engines/read/scans/" + scanID + "/scan-overview"
	var out models.SCSOverview
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, path),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
