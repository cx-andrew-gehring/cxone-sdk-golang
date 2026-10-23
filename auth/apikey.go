package auth

import (
	"context"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
)

// astAppClientID is the hardcoded client_id used by the Checkmarx One IAM
// refresh-token grant. It is the literal string "ast-app" — see CLAUDE.md
// §6.2. Do not change.
const astAppClientID = "ast-app"

// NewAPIKey returns an [Authenticator] that fetches access tokens via the
// Checkmarx One "API key" flow. Despite the name, the supplied apiKey is
// actually a long-lived OAuth refresh token issued by the Checkmarx One UI.
//
// The returned authenticator caches access tokens, serializes concurrent
// refreshes, and is safe for concurrent use.
func NewAPIKey(cfg Config, apiKey string) (Authenticator, error) {
	if apiKey == "" {
		return nil, &cxerrors.ConfigurationError{Field: "APIKey", Reason: "is required"}
	}
	if cfg.HTTPClient == nil {
		return nil, &cxerrors.ConfigurationError{Field: "auth.HTTPClient", Reason: "is required"}
	}
	if cfg.TokenURL == "" {
		return nil, &cxerrors.ConfigurationError{Field: "auth.TokenURL", Reason: "is required"}
	}
	return newCached(&apiKeyFetcher{cfg: cfg, apiKey: apiKey}), nil
}

type apiKeyFetcher struct {
	cfg    Config
	apiKey string
}

func (f *apiKeyFetcher) fetch(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {astAppClientID},
		"refresh_token": {f.apiKey},
	}
	return postForm(ctx, f.cfg, form)
}

func (f *apiKeyFetcher) close() {}
