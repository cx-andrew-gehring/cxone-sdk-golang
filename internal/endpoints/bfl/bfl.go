// Package bfl implements the Checkmarx One Best Fix Location endpoint.
package bfl

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the relative path to the BFL endpoint.
const Path = "api/bfl"

// Get returns the best-fix-location tree for a SAST query in a given scan.
// GET /api/bfl?scan-id={id}&query-id={qid} → 200.
func Get(ctx context.Context, e *transport.Executor, baseURL, scanID, queryID string) (*models.BFLResponseModel, error) {
	q := url.Values{
		"scan-id":  {scanID},
		"query-id": {queryID},
	}
	var out models.BFLResponseModel
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path),
		q, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
