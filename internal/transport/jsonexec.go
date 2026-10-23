package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// DoJSON is the workhorse used by every endpoint function in
// internal/endpoints/...
//
// It marshals body (if non-nil) as JSON, sends the request through Executor.Do,
// and on success decodes the response body into out (if non-nil). The set of
// HTTP status codes considered successful is supplied by the caller; any
// other status causes the body to be decoded as [models.ErrorModel] and
// surfaced as a *cxerrors.ResponseError.
//
// Parameters:
//
//   - urlStr      — fully-qualified URL.
//   - query       — optional query parameters; nil for none.
//   - body        — request body; nil for GET-style requests. Anything
//                   marshalable by encoding/json is accepted.
//   - expected    — non-empty list of HTTP status codes considered success.
//   - out         — pointer to a struct that should receive the decoded
//                   response body, or nil if the caller doesn't care about
//                   the body (e.g. DELETE / 204 No Content).
//
// The response body is always drained and closed before DoJSON returns.
func DoJSON(
	ctx context.Context,
	e *Executor,
	method, urlStr string,
	query url.Values,
	body any,
	expected []int,
	out any,
) error {
	finalURL, err := appendQuery(urlStr, query)
	if err != nil {
		return &cxerrors.ConfigurationError{Field: "url", Reason: err.Error()}
	}

	req := &Request{
		Method: method,
		URL:    finalURL,
	}
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return &cxerrors.ConfigurationError{Field: "body", Reason: "json marshal: " + err.Error()}
		}
		req.Body = bytes.NewReader(buf)
		req.ContentType = "application/json"
	}

	resp, err := e.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Read up to a generous bound; Checkmarx One responses for these
	// endpoint shapes are well under 16 MiB. Reports use a streaming path
	// (added in M5) and don't go through DoJSON.
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if readErr != nil {
		return &cxerrors.ResponseError{
			Method:        method,
			URL:           finalURL,
			StatusCode:    resp.StatusCode,
			CorrelationID: resp.CorrelationID,
			Reason:        "read response body: " + readErr.Error(),
			Cause:         readErr,
		}
	}

	if statusIn(resp.StatusCode, expected) {
		if out == nil || len(bodyBytes) == 0 {
			return nil
		}
		if err := json.Unmarshal(bodyBytes, out); err != nil {
			return &cxerrors.ResponseError{
				Method:        method,
				URL:           finalURL,
				StatusCode:    resp.StatusCode,
				CorrelationID: resp.CorrelationID,
				Reason:        "decode response body: " + err.Error(),
				Cause:         err,
			}
		}
		return nil
	}

	// Unexpected status — try to decode an error envelope to enrich the
	// returned error. We tolerate decode failures: an unparseable body just
	// means the upstream returned something unusual; the status code is
	// still authoritative.
	reason := fmt.Sprintf("unexpected status %d", resp.StatusCode)
	var em models.ErrorModel
	if len(bodyBytes) > 0 && json.Unmarshal(bodyBytes, &em) == nil && em.Message != "" {
		reason = fmt.Sprintf("status %d: %s", resp.StatusCode, em.Message)
	}
	return &cxerrors.ResponseError{
		Method:        method,
		URL:           finalURL,
		StatusCode:    resp.StatusCode,
		CorrelationID: resp.CorrelationID,
		Reason:        reason,
		RetryAfter:    retryAfter(resp.Header),
	}
}

// retryAfter returns the parsed Retry-After of h, 0 when absent or malformed.
func retryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	d, _ := retry.ParseRetryAfter(h.Get("Retry-After"), time.Now())
	return d
}

// statusIn reports whether code is contained in expected.
func statusIn(code int, expected []int) bool {
	for _, c := range expected {
		if c == code {
			return true
		}
	}
	return false
}

// appendQuery merges query into u's existing query string and returns the
// resulting URL. If query is empty u is returned unchanged.
func appendQuery(u string, query url.Values) (string, error) {
	if len(query) == 0 {
		return u, nil
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	existing := parsed.Query()
	for k, vs := range query {
		for _, v := range vs {
			existing.Add(k, v)
		}
	}
	parsed.RawQuery = existing.Encode()
	return parsed.String(), nil
}

// JoinURL joins an API base URL ("https://host/api/") with a relative path
// ("projects" or "projects/123/branches"). Leading slashes on path are
// stripped to keep the result well-formed.
func JoinURL(base, path string) string {
	if path == "" {
		return base
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + strings.TrimLeft(path, "/")
}
