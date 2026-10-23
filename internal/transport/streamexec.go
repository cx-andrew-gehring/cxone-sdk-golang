package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
)

// DoStream is the binary-download counterpart to [DoJSON].
//
// It sends a GET request through the funnel and, on a status in expected,
// returns the response body unread so the caller can stream it (e.g. write
// it to a file). The caller MUST close the returned ReadCloser.
//
// On a non-expected status DoStream reads a small chunk of the body to try
// to extract a [models.ErrorModel] envelope, then returns a *cxerrors.ResponseError.
// On transport-level failure DoStream returns whatever the funnel returned
// (typically *cxerrors.CommunicationError).
func DoStream(
	ctx context.Context,
	e *Executor,
	method, urlStr string,
	query url.Values,
	expected []int,
) (io.ReadCloser, error) {
	finalURL, err := appendQuery(urlStr, query)
	if err != nil {
		return nil, &cxerrors.ConfigurationError{Field: "url", Reason: err.Error()}
	}
	resp, err := e.Do(ctx, &Request{Method: method, URL: finalURL})
	if err != nil {
		return nil, err
	}
	if statusIn(resp.StatusCode, expected) {
		return resp.Body, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	reason := fmt.Sprintf("unexpected status %d", resp.StatusCode)
	var em models.ErrorModel
	if len(body) > 0 && json.Unmarshal(body, &em) == nil && em.Message != "" {
		reason = fmt.Sprintf("status %d: %s", resp.StatusCode, em.Message)
	}
	return nil, &cxerrors.ResponseError{
		Method:        method,
		URL:           finalURL,
		StatusCode:    resp.StatusCode,
		CorrelationID: resp.CorrelationID,
		Reason:        reason,
	}
}

// DoText is the plain-text counterpart to [DoJSON].
//
// It sends a request through the funnel and, on a status in expected, reads
// the entire response body as a string and returns it. Used by the
// /api/logs/{scanId}/{scanType} endpoint which streams plain text rather
// than JSON.
//
// The body is bounded at 16 MiB.
func DoText(
	ctx context.Context,
	e *Executor,
	method, urlStr string,
	query url.Values,
	expected []int,
) (string, error) {
	finalURL, err := appendQuery(urlStr, query)
	if err != nil {
		return "", &cxerrors.ConfigurationError{Field: "url", Reason: err.Error()}
	}
	resp, err := e.Do(ctx, &Request{Method: method, URL: finalURL})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if readErr != nil {
		return "", &cxerrors.ResponseError{
			Method: method, URL: finalURL,
			StatusCode: resp.StatusCode, CorrelationID: resp.CorrelationID,
			Reason: "read response body: " + readErr.Error(),
			Cause:  readErr,
		}
	}
	if statusIn(resp.StatusCode, expected) {
		return string(body), nil
	}
	reason := fmt.Sprintf("unexpected status %d", resp.StatusCode)
	if len(body) > 0 {
		reason = fmt.Sprintf("status %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	return "", &cxerrors.ResponseError{
		Method:        method,
		URL:           finalURL,
		StatusCode:    resp.StatusCode,
		CorrelationID: resp.CorrelationID,
		Reason:        reason,
		RetryAfter:    retryAfter(resp.Header),
	}
}

// MethodGet is exported only so endpoint packages can use it without
// importing net/http for one constant; it is purely a convenience.
const MethodGet = http.MethodGet

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
