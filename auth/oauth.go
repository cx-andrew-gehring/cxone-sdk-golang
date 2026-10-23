package auth

import (
	"context"
	"net/url"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
)

// OAuthCredentials carries the inputs for the OAuth client_credentials flow
// (CLAUDE.md §6.1).
type OAuthCredentials struct {
	ClientID     string
	ClientSecret string
}

// NewOAuthClientCredentials returns an [Authenticator] that fetches access
// tokens via the OAuth 2.0 client_credentials grant. Tokens are cached and
// concurrent refresh is serialized. The returned authenticator is safe for
// concurrent use.
//
// The supplied Config.HTTPClient should be the same long-lived client the
// SDK uses for application requests so connection pools are shared.
func NewOAuthClientCredentials(cfg Config, creds OAuthCredentials) (Authenticator, error) {
	if creds.ClientID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "OAuth.ClientID", Reason: "is required"}
	}
	if creds.ClientSecret == "" {
		return nil, &cxerrors.ConfigurationError{Field: "OAuth.ClientSecret", Reason: "is required"}
	}
	if cfg.HTTPClient == nil {
		return nil, &cxerrors.ConfigurationError{Field: "auth.HTTPClient", Reason: "is required"}
	}
	if cfg.TokenURL == "" {
		return nil, &cxerrors.ConfigurationError{Field: "auth.TokenURL", Reason: "is required"}
	}
	return newCached(&oauthFetcher{cfg: cfg, creds: creds}), nil
}

type oauthFetcher struct {
	cfg   Config
	creds OAuthCredentials
}

func (f *oauthFetcher) fetch(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {f.creds.ClientID},
		"client_secret": {f.creds.ClientSecret},
	}
	return postForm(ctx, f.cfg, form)
}

func (f *oauthFetcher) close() {}
