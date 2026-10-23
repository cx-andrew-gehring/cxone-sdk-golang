// Package auth implements the two Checkmarx One IAM authentication flows
// (OAuth client_credentials and API key) plus a serialized token cache.
//
// Applications normally do not import this package directly; the cxone client
// builder accepts a cxone.Authenticator (which is an alias of [Authenticator])
// and constructs the appropriate implementation from credentials passed to
// the builder. Importing this package directly is only necessary when:
//
//   - You need to share a single token cache across multiple cxone.Client
//     instances (rare), or
//   - You want to plug in a custom Authenticator implementation that fetches
//     tokens from a vault, broker, or sidecar.
//
// CLAUDE.md §4.5 + §6 govern the behavior of every implementation in this
// package: lazy first auth, 401 → refresh → retry, serialized refresh, no
// secrets in errors or logs.
package auth

import (
	"context"
	"net/http"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
)

// Authenticator supplies bearer access tokens to the SDK request funnel.
//
// Token returns the currently cached token if one exists, or fetches a new
// one if not. Refresh discards any cached token whose generation matches the
// stale generation passed in and fetches a new one. Concurrent calls
// serialize on a single in-flight fetch — when many goroutines hit a 401
// simultaneously, only ONE token fetch is sent to IAM and all callers reuse
// the result.
//
// The generation counter lets the funnel reason about staleness without
// holding any lock itself: a goroutine that received generation N and then
// observed a 401 can call Refresh(N); if another goroutine has already
// refreshed the cache to generation N+1 the call returns the new token
// without re-fetching.
type Authenticator interface {
	// Token returns the current bearer access token, fetching one if the
	// cache is empty. The returned generation can be passed back to Refresh
	// to drive the §4.5 refresh-on-401 logic without races.
	Token(ctx context.Context) (token string, generation uint64, err error)

	// Refresh forces a new token fetch when the caller's stale generation
	// equals the cached generation. If the cache has already advanced past
	// staleGen (because another goroutine refreshed first), Refresh is a
	// no-op and returns the cached token.
	Refresh(ctx context.Context, staleGen uint64) (token string, generation uint64, err error)
}

// Config carries the dependencies every concrete authenticator in this package
// needs. The cxone client builder fills it in.
type Config struct {
	// HTTPClient is used for the IAM token request. It MUST be a long-lived,
	// pooled client (the SDK reuses the same instance across all auth calls
	// and request calls). Required.
	HTTPClient *http.Client

	// TokenURL is the fully-qualified IAM token endpoint, e.g.
	//   https://iam.checkmarx.net/auth/realms/acme/protocol/openid-connect/token
	// (Use cxone.Region.TokenURL to construct it.) Required.
	TokenURL string

	// UserAgent is the value to send on the User-Agent header of the auth
	// request. The funnel sets the same header on application requests.
	UserAgent string

	// RetryPolicy controls how token-endpoint failures are retried
	// (CLAUDE.md §6.3: same rules as the request funnel). When zero,
	// [retry.DefaultPolicy] is used. Tests typically override this with a
	// zero-delay policy to avoid sleeping during retries.
	RetryPolicy retry.Policy
}
