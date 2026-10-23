// Package export implements the Checkmarx One SCA export endpoint.
//
// Like reports, this is a three-step dance: POST a request to enqueue the
// export, poll until completion, then download the file.
package export

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the relative path to the SCA export endpoint.
const Path = "api/sca/export"

// Create queues an SCA export. POST /api/sca/export/requests → 202.
func Create(ctx context.Context, e *transport.Executor, baseURL string, in *models.ExportRequestPayload) (*models.ExportResponse, error) {
	var out models.ExportResponse
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path+"/requests"),
		nil, in, []int{http.StatusAccepted}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Poll returns the current status of an export request.
// GET /api/sca/export/requests?return-url=true&export-id={id} → 200.
func Poll(ctx context.Context, e *transport.Executor, baseURL, exportID string) (*models.ExportPollingResponse, error) {
	q := url.Values{
		"return-url": {"true"},
		"export-id":  {exportID},
	}
	var out models.ExportPollingResponse
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/requests"),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Download streams the completed export file body. The caller MUST close the
// returned ReadCloser.
// GET /api/sca/export/requests/{exportId}/download → 200.
func Download(ctx context.Context, e *transport.Executor, baseURL, exportID string) (io.ReadCloser, error) {
	return transport.DoStream(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/requests/"+exportID+"/download"),
		nil, []int{http.StatusOK})
}
