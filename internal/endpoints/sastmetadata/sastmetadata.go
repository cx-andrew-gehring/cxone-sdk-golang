// Package sastmetadata implements the Checkmarx One SAST metadata endpoint.
package sastmetadata

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the relative path to the sast-metadata endpoint.
const Path = "api/sast-metadata"

// List returns SAST metadata (line counts, file counts, incremental status,
// query preset) for one or more scans.
// GET /api/sast-metadata?scan-ids={id}&scan-ids={id}... → 200.
func List(ctx context.Context, e *transport.Executor, baseURL string, scanIDs ...string) (*models.SastMetadataModel, error) {
	q := url.Values{}
	for _, id := range scanIDs {
		q.Add("scan-ids", id)
	}
	var out models.SastMetadataModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
