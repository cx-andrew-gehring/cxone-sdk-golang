// Package uploads implements the Checkmarx One Uploads API.
//
// Uploading a source archive is a two-step dance:
//
//  1. POST /api/uploads → 200 returns a presigned URL ([GetPresignedURL]).
//     This call goes through the SDK funnel and carries the bearer token
//     so the platform can authorize and audit the request.
//
//  2. PUT the file body to that presigned URL ([PutFile]). The URL points
//     at the platform's object store, NOT the Checkmarx API host. It
//     embeds short-lived credentials and must NOT be sent with the SDK's
//     bearer token — doing so confuses the object store and may be
//     rejected. PutFile therefore bypasses the funnel and uses a plain
//     [*http.Client].
//
// The high-level workflow layer (cxone/workflows/scans) hides this two-step
// dance behind a single ScanInvoker.WithUpload(reader, size) call.
package uploads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
)

// Path is the relative path to the uploads endpoint, joined with the API
// base URL.
const Path = "api/uploads"

// GetPresignedURL requests a presigned URL where the caller can PUT a source
// archive. POST /api/uploads → 200.
func GetPresignedURL(ctx context.Context, e *transport.Executor, baseURL string) (string, error) {
	var out models.UploadModel
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", &cxerrors.ResponseError{
			Method:     http.MethodPost,
			URL:        transport.JoinURL(baseURL, Path),
			StatusCode: http.StatusOK,
			Reason:     "presigned URL response missing 'url' field",
		}
	}
	return out.URL, nil
}

// PutFile uploads body (of length size, in bytes) to a presigned URL
// previously obtained from [GetPresignedURL].
//
// PutFile uses the supplied *http.Client directly — it does NOT go through
// the SDK funnel because:
//
//   - The presigned URL is on the object store, not the Checkmarx API host.
//   - The SDK's bearer token must not be sent (it would be rejected).
//   - The retry/headers/correlation-id machinery does not apply.
//
// On a non-2xx response PutFile returns a *cxerrors.CommunicationError so
// the failure is still surfaceable via the standard catalog discrimination.
//
// httpClient is typically the same *http.Client the cxone client is using
// for its API calls (so connection pools and proxies are shared); the
// uploads workflow layer fetches it from the client.
func PutFile(ctx context.Context, httpClient *http.Client, presignedURL string, body io.Reader, size int64) error {
	if httpClient == nil {
		return &cxerrors.ConfigurationError{Field: "httpClient", Reason: "is required"}
	}
	if presignedURL == "" {
		return &cxerrors.ConfigurationError{Field: "presignedURL", Reason: "is required"}
	}
	if body == nil {
		return &cxerrors.ConfigurationError{Field: "body", Reason: "is required"}
	}
	if size <= 0 {
		return &cxerrors.ConfigurationError{Field: "size", Reason: "must be > 0 (object stores require Content-Length)"}
	}

	safeURL := sanitizeURL(presignedURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, presignedURL, body)
	if err != nil {
		return &cxerrors.CommunicationError{
			Method: http.MethodPut, URL: safeURL, Attempt: 1,
			Cause: fmt.Errorf("build upload request: %w", scrubCause(err, presignedURL, safeURL)),
		}
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/zip")

	resp, err := httpClient.Do(req)
	if err != nil {
		return &cxerrors.CommunicationError{
			Method: http.MethodPut, URL: safeURL, Attempt: 1,
			Cause: scrubCause(err, presignedURL, safeURL),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read a small chunk for diagnostics; never log the body wholesale
		// (presigned URLs may be publicly indexable).
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return &cxerrors.CommunicationError{
			Method:     http.MethodPut,
			URL:        safeURL,
			StatusCode: resp.StatusCode,
			Attempt:    1,
		}
	}
	return nil
}

// sanitizeURL strips the query string, fragment and userinfo from a presigned
// URL: they carry the signature/credentials and must never reach an error
// message or log. An unparseable URL is replaced by a placeholder.
func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid presigned url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// scrubCause unwraps a *url.Error (whose Error() embeds the full request URL,
// signature included) to its inner cause and replaces any remaining
// occurrence of the raw URL in the message with the sanitized one.
func scrubCause(err error, raw, safe string) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if err == nil {
		return nil
	}
	if msg := err.Error(); strings.Contains(msg, raw) {
		return errors.New(strings.ReplaceAll(msg, raw, safe))
	}
	return err
}

// PutFileWithTimeout is [PutFile] with its own deadline d that is NOT capped
// by httpClient.Timeout: large archives can take far longer than the API
// client's per-request timeout. It copies httpClient with Timeout=0 (sharing
// its Transport, so pools and proxies still apply) and bounds the request by
// a context deadline of d instead. d must be positive.
func PutFileWithTimeout(ctx context.Context, httpClient *http.Client, presignedURL string, body io.Reader, size int64, d time.Duration) error {
	if httpClient == nil {
		return &cxerrors.ConfigurationError{Field: "httpClient", Reason: "is required"}
	}
	if d <= 0 {
		return &cxerrors.ConfigurationError{Field: "UploadTimeout", Reason: "must be > 0"}
	}
	cp := *httpClient
	cp.Timeout = 0
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return PutFile(ctx, &cp, presignedURL, body, size)
}
