package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/redact"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
)

// tokenResponse is the subset of the OIDC token response we care about. The
// full Keycloak/Checkmarx response carries more fields (refresh_token,
// expires_in, etc.) — we ignore them and let new fields land without breaking
// (CLAUDE.md §2 forward compatibility).
type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

// errorResponse mirrors the Keycloak token-endpoint error envelope.
type errorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// postForm POSTs the provided form values to cfg.TokenURL using the same
// retry rules as the application request funnel (CLAUDE.md §6.3). Form bodies
// are NEVER returned in errors — only the response status and the upstream
// "error" field are surfaced.
//
// On exhaustion postForm returns an [*cxerrors.AuthError]. Non-retryable
// failures (4xx other than 401, malformed JSON) also return AuthError.
//
// Note: 401 from the token endpoint is fatal here — there is no token to
// refresh in the auth flow itself. The funnel-level 401 → refresh → retry
// logic only applies to API requests.
func postForm(ctx context.Context, cfg Config, form url.Values) (string, error) {
	if cfg.HTTPClient == nil {
		return "", &cxerrors.AuthError{Reason: "auth: HTTPClient is nil"}
	}
	if cfg.TokenURL == "" {
		return "", &cxerrors.AuthError{Reason: "auth: TokenURL is empty"}
	}

	encoded := form.Encode()

	policy := cfg.RetryPolicy
	if policy.MaxAttempts == 0 {
		policy = retry.DefaultPolicy()
	}

	var (
		token       string
		lastHTTPErr error
	)

	_, err := retry.Do(ctx, policy, func(attempt int) (retry.Outcome, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(encoded))
		if err != nil {
			// Construction errors are not retryable.
			return retry.OutcomeFatal, &cxerrors.AuthError{
				Reason: "build token request: " + redact.String(err.Error()),
				Cause:  err,
			}
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		if cfg.UserAgent != "" {
			req.Header.Set("User-Agent", cfg.UserAgent)
		}

		resp, err := cfg.HTTPClient.Do(req)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return retry.OutcomeFatal, err
			}
			if retry.IsTransientNetworkError(err) {
				lastHTTPErr = err
				return retry.OutcomeRetry, err
			}
			return retry.OutcomeFatal, &cxerrors.AuthError{
				Reason: "transport: " + redact.String(err.Error()),
				Cause:  err,
			}
		}
		defer resp.Body.Close()

		// 5xx — retry per §6.3 / §4.4.
		if retry.ClassifyStatus(resp.StatusCode) == retry.OutcomeRetry {
			lastHTTPErr = &cxerrors.AuthError{
				Reason:     statusReason(resp),
				StatusCode: resp.StatusCode,
			}
			return retry.OutcomeRetry, lastHTTPErr
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return retry.OutcomeFatal, &cxerrors.AuthError{
				Reason:     statusReason(resp),
				StatusCode: resp.StatusCode,
			}
		}

		// 2xx — parse the body.
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return retry.OutcomeFatal, &cxerrors.AuthError{
				Reason:     "read token response body: " + redact.String(readErr.Error()),
				StatusCode: resp.StatusCode,
				Cause:      readErr,
			}
		}
		var tr tokenResponse
		if err := json.Unmarshal(body, &tr); err != nil {
			return retry.OutcomeFatal, &cxerrors.AuthError{
				Reason:     "decode token response: " + redact.String(err.Error()),
				StatusCode: resp.StatusCode,
				Cause:      err,
			}
		}
		if tr.AccessToken == "" {
			return retry.OutcomeFatal, &cxerrors.AuthError{
				Reason:     "token response missing access_token",
				StatusCode: resp.StatusCode,
			}
		}
		token = tr.AccessToken
		return retry.OutcomeSuccess, nil
	})
	if err != nil {
		// If retry.Do returned the last retry-able error verbatim, wrap it
		// as AuthError so callers always see the catalog type.
		var aerr *cxerrors.AuthError
		if errors.As(err, &aerr) {
			return "", err
		}
		if lastHTTPErr != nil {
			return "", lastHTTPErr
		}
		return "", &cxerrors.AuthError{Reason: "token fetch failed: " + redact.String(err.Error()), Cause: err}
	}
	return token, nil
}

// statusReason summarizes a non-2xx token response without leaking secrets.
// It reads up to 4 KiB of the body and tries to decode the standard Keycloak
// error envelope; on failure it falls back to the HTTP status text.
func statusReason(resp *http.Response) string {
	if resp.Body == nil {
		return resp.Status
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil || len(body) == 0 {
		return resp.Status
	}
	var er errorResponse
	if err := json.Unmarshal(body, &er); err == nil && er.Error != "" {
		if er.ErrorDescription != "" {
			return er.Error + ": " + er.ErrorDescription
		}
		return er.Error
	}
	// Fallback to a sanitized truncation of the body.
	return resp.Status + ": " + redact.String(strings.TrimSpace(string(body)))
}
