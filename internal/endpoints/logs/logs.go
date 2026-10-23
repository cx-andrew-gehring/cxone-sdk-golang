// Package logs implements the Checkmarx One scan-logs endpoint.
//
// The endpoint streams plain text (engine output) rather than JSON, so it
// uses the [transport.DoText] helper.
package logs

import (
	"context"
	"net/http"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
)

// Path is the relative path to the logs endpoint.
const Path = "api/logs"

// Get returns the engine output log for one (scanID, scanType) pair as a
// plain string. GET /api/logs/{scanId}/{scanType} → 200.
//
// scanType is one of: "sast", "kics", "sca", "container", "apisec".
func Get(ctx context.Context, e *transport.Executor, baseURL, scanID, scanType string) (string, error) {
	return transport.DoText(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+scanID+"/"+scanType),
		nil, []int{http.StatusOK})
}
