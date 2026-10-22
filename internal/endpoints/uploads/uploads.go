// Package uploads implements the Checkmarx One Uploads API.
//
// Uploading a source archive is a two-step dance:
//
//  1. POST /api/uploads → 200 returns a presigned URL ([GetPresignedURL]).
//     This call goes through the SDK funnel and carries the bearer token
//     so the platform can authorize and audit the request.
//
//  2. PUT the file body to that presigned URL ([PutFileAuth], or [PutFile]
//     when no token source is available). Where the URL points depends on
//     the tenant's storage:
//
//     - External object store (S3/MinIO): the URL embeds short-lived
//     credentials and must NOT carry the SDK's bearer token — a bearer on a
//     presigned S3 URL is rejected and would leak the token to a third
//     party.
//     - Storage fronted by the Checkmarx API host itself (the URL is
//     https://<api host>/storage/uploads...): the API gateway requires the
//     bearer token on the PUT (this mirrors the official ast-cli).
//
//     [PutFileAuth] therefore attaches the bearer token if and only if the
//     presigned URL's origin is exactly the client's API origin, and never
//     otherwise. Both variants bypass the funnel and use a plain
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

	"github.com/checkmarx-open-labs/cxone-sdk-golang/auth"
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
// PutFile never sends an Authorization header: use it for presigned URLs on an
// external object store. For storage fronted by the API host use
// [PutFileAuth]. It uses the supplied *http.Client directly — it does NOT go
// through the SDK funnel because the presigned URL is not an API endpoint and
// the retry/headers/correlation-id machinery does not apply.
//
// Deadline: every upload is bounded. PutFile uses httpClient.Timeout when
// > 0, otherwise a ceiling of two hours (previously a client with Timeout==0
// meant no limit). Use [PutFileWithTimeout] / [PutFileAuthWithTimeout] to
// set a different, e.g. larger, deadline. A deadline hit is returned as a
// *cxerrors.CommunicationError.
//
// On a non-2xx response PutFile returns a *cxerrors.CommunicationError so
// the failure is still surfaceable via the standard catalog discrimination.
//
// httpClient is typically the same *http.Client the cxone client is using
// for its API calls (so connection pools and proxies are shared); the
// uploads workflow layer fetches it from the client.
func PutFile(ctx context.Context, httpClient *http.Client, presignedURL string, body io.Reader, size int64) error {
	return putFile(ctx, httpClient, presignedURL, body, size, nil, 0)
}

// Auth lets [PutFileAuth] authenticate the upload PUT when (and only when)
// the presigned URL is served by the Checkmarx API host.
type Auth struct {
	// Authenticator is the SDK's token source (the same one the request
	// funnel uses). Nil disables authentication entirely.
	Authenticator auth.Authenticator
	// APIBaseURL is the API origin the client talks to (e.g.
	// "https://deu.ast.checkmarx.net/"). Only scheme, host and port are used.
	APIBaseURL string
}

// PutFileAuth is [PutFile] that attaches "Authorization: Bearer <token>" iff
// the presigned URL's origin (scheme + host + port) is exactly the origin of
// a.APIBaseURL. Any other origin gets no Authorization header, so tokens are
// never sent to external object stores. The comparison is fail-closed: URLs
// with userinfo, a trailing-dot or non-ASCII host, a different port or a
// different scheme never match.
//
// When the token is attached:
//
//   - Redirects are not followed (a 3xx is returned as a *CommunicationError
//     with its StatusCode). Go would forward Authorization to subdomains of
//     the original host and replay the body on 307/308; refusing is the only
//     way to guarantee the token stays on the API origin. The caller's
//     *http.Client is not modified (a copy is used).
//   - A 401 triggers one token refresh and one replay, but only if body is
//     an io.Seeker (it is rewound to its starting offset). A non-seekable
//     body cannot be replayed, so the 401 is returned.
//
// Errors never contain the token or the URL's query string.
func PutFileAuth(ctx context.Context, httpClient *http.Client, presignedURL string, body io.Reader, size int64, a *Auth) error {
	return putFile(ctx, httpClient, presignedURL, body, size, a, 0)
}

func putFile(ctx context.Context, httpClient *http.Client, presignedURL string, body io.Reader, size int64, a *Auth, timeout time.Duration) error {
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

	// Explicit deadline on every upload: the caller's explicit timeout, else
	// the client's own Timeout, else the package ceiling. The derived context
	// is attached to each request, so body streaming is bounded too.
	if timeout <= 0 {
		timeout = httpClient.Timeout
	}
	if timeout <= 0 {
		timeout = putCeiling
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	safeURL := sanitizeURL(presignedURL)
	commErr := func(status, attempt int, cause error) error {
		return &cxerrors.CommunicationError{
			Method: http.MethodPut, URL: safeURL, StatusCode: status, Attempt: attempt, Cause: cause,
		}
	}

	// send performs one PUT. token == "" means no Authorization header.
	send := func(c *http.Client, b io.Reader, token string, attempt int) (int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, presignedURL, b)
		if err != nil {
			return 0, commErr(0, attempt, fmt.Errorf("build upload request: %w", scrubCause(err, presignedURL, safeURL)))
		}
		req.ContentLength = size
		req.Header.Set("Content-Type", "application/zip")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := c.Do(req)
		if err != nil {
			return 0, commErr(0, attempt, scrubCause(err, presignedURL, safeURL))
		}
		defer resp.Body.Close()
		// Read a small chunk for diagnostics; never log the body wholesale
		// (presigned URLs may be publicly indexable).
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, nil
	}
	ok := func(status int) bool { return status >= 200 && status < 300 }

	if a == nil || a.Authenticator == nil || !sameOrigin(a.APIBaseURL, presignedURL) {
		status, err := send(httpClient, body, "", 1)
		if err != nil {
			return err
		}
		if !ok(status) {
			return commErr(status, 1, nil)
		}
		return nil
	}

	// Same-origin storage: authenticated PUT. Never follow redirects so the
	// token cannot be forwarded anywhere (see PutFileAuth).
	cp := *httpClient
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	var (
		seeker io.Seeker
		start  int64
	)
	if s, isSeeker := body.(io.Seeker); isSeeker {
		if pos, err := s.Seek(0, io.SeekCurrent); err == nil {
			seeker, start = s, pos
		}
	}

	token, gen, err := a.Authenticator.Token(ctx)
	if err != nil {
		return err
	}
	status, err := send(&cp, body, token, 1)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized && seeker != nil {
		// The cached token may have gone stale; refresh once and replay.
		if _, err := seeker.Seek(start, io.SeekStart); err != nil {
			return commErr(status, 1, nil)
		}
		token, _, err = a.Authenticator.Refresh(ctx, gen)
		if err != nil {
			return err
		}
		status, err = send(&cp, body, token, 2)
		if err != nil {
			return err
		}
		if !ok(status) {
			return commErr(status, 2, nil)
		}
		return nil
	}
	if !ok(status) {
		return commErr(status, 1, nil)
	}
	return nil
}

// putCeiling is the deadline PutFile/PutFileAuth apply when neither an
// explicit timeout nor httpClient.Timeout is set. Variable only so tests can
// shorten it.
var putCeiling = 2 * time.Hour

// sameOrigin reports whether presigned has exactly the same scheme, host and
// effective port as apiBase. It fails closed: any URL it cannot confidently
// classify (parse errors, userinfo, opaque, trailing-dot or non-ASCII hosts,
// a scheme other than http/https) is NOT the same origin.
func sameOrigin(apiBase, presigned string) bool {
	as, ah, ap, aok := originOf(apiBase)
	ps, ph, pp, pok := originOf(presigned)
	return aok && pok && as == ps && ah == ph && ap == pp
}

func originOf(raw string) (scheme, host, port string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" {
		return "", "", "", false
	}
	scheme = strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", "", "", false
	}
	host = strings.ToLower(u.Hostname())
	if host == "" || strings.HasSuffix(host, ".") {
		return "", "", "", false
	}
	for i := 0; i < len(host); i++ {
		if c := host[i]; c >= 0x80 || c <= ' ' || c == '\\' || c == '@' {
			return "", "", "", false
		}
	}
	port = u.Port()
	if port == "" {
		port = "80"
		if scheme == "https" {
			port = "443"
		}
	}
	return scheme, host, port, true
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
	return PutFileAuthWithTimeout(ctx, httpClient, presignedURL, body, size, nil, d)
}

// PutFileAuthWithTimeout is [PutFileAuth] with the deadline semantics of
// [PutFileWithTimeout]. a may be nil (no authentication).
func PutFileAuthWithTimeout(ctx context.Context, httpClient *http.Client, presignedURL string, body io.Reader, size int64, a *Auth, d time.Duration) error {
	if httpClient == nil {
		return &cxerrors.ConfigurationError{Field: "httpClient", Reason: "is required"}
	}
	if d <= 0 {
		return &cxerrors.ConfigurationError{Field: "UploadTimeout", Reason: "must be > 0"}
	}
	cp := *httpClient
	cp.Timeout = 0
	return putFile(ctx, &cp, presignedURL, body, size, a, d)
}
