package cxone

import (
	"net/url"
	"strings"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
)

// Region identifies a Checkmarx One deployment by its IAM (auth) host and API
// host. Each multi-tenant region is exposed as a typed value (RegionUS,
// RegionEU, etc.); single-tenant deployments use [NewCustomRegion].
//
// The catalog below is canonical and must match every other Checkmarx One SDK
// — see CLAUDE.md §13.
type Region struct {
	// Name is a short label used in logs and trace attributes (e.g. "US",
	// "EU2", "custom").
	Name string

	// AuthHost is the IAM hostname (no scheme, no path).
	// Example: "iam.checkmarx.net".
	AuthHost string

	// APIHost is the API hostname (no scheme, no path).
	// Example: "ast.checkmarx.net".
	APIHost string

	// Scheme is the URL scheme used for both hosts. Defaults to "https".
	// "http" is permitted only as an explicit override (test environments).
	Scheme string
}

// The nine well-known multi-tenant regions, in the order documented in
// CLAUDE.md §13. Do NOT renumber or re-spell these — the host strings are
// part of the cross-language SDK contract.
var (
	RegionUS = Region{
		Name:     "US",
		AuthHost: "iam.checkmarx.net",
		APIHost:  "ast.checkmarx.net",
		Scheme:   "https",
	}
	RegionUS2 = Region{
		Name:     "US2",
		AuthHost: "us.iam.checkmarx.net",
		APIHost:  "us.ast.checkmarx.net",
		Scheme:   "https",
	}
	RegionEU = Region{
		Name:     "EU",
		AuthHost: "eu.iam.checkmarx.net",
		APIHost:  "eu.ast.checkmarx.net",
		Scheme:   "https",
	}
	RegionEU2 = Region{
		Name:     "EU2",
		AuthHost: "eu-2.iam.checkmarx.net",
		APIHost:  "eu-2.ast.checkmarx.net",
		Scheme:   "https",
	}
	RegionDEU = Region{
		Name:     "DEU",
		AuthHost: "deu.iam.checkmarx.net",
		APIHost:  "deu.ast.checkmarx.net",
		Scheme:   "https",
	}
	RegionANZ = Region{
		Name:     "ANZ",
		AuthHost: "anz.iam.checkmarx.net",
		APIHost:  "anz.ast.checkmarx.net",
		Scheme:   "https",
	}
	RegionIndia = Region{
		Name:     "India",
		AuthHost: "ind.iam.checkmarx.net",
		APIHost:  "ind.ast.checkmarx.net",
		Scheme:   "https",
	}
	RegionSingapore = Region{
		Name:     "Singapore",
		AuthHost: "sng.iam.checkmarx.net",
		APIHost:  "sng.ast.checkmarx.net",
		Scheme:   "https",
	}
	RegionUAE = Region{
		Name:     "UAE",
		AuthHost: "mea.iam.checkmarx.net",
		APIHost:  "mea.ast.checkmarx.net",
		Scheme:   "https",
	}
)

// AllRegions returns the nine well-known multi-tenant regions in catalog order.
// Useful for tests, region-pickers in CLI tooling, and validation.
func AllRegions() []Region {
	return []Region{
		RegionUS, RegionUS2, RegionEU, RegionEU2, RegionDEU,
		RegionANZ, RegionIndia, RegionSingapore, RegionUAE,
	}
}

// NewCustomRegion constructs a Region for a single-tenant or self-hosted
// Checkmarx One deployment.
//
// Both hosts must be non-empty bare hostnames ("example.com" or
// "example.com:8443"), not URLs. The scheme defaults to https; pass "http"
// only when targeting an explicit test environment.
//
// The constructed region is validated immediately; on failure an
// [EndpointError] is returned.
func NewCustomRegion(authHost, apiHost string) (Region, error) {
	r := Region{
		Name:     "custom",
		AuthHost: authHost,
		APIHost:  apiHost,
		Scheme:   "https",
	}
	if err := r.Validate(); err != nil {
		return Region{}, err
	}
	return r, nil
}

// WithScheme returns a copy of r with the scheme replaced. "http" is the only
// override accepted; any other value yields an [EndpointError]. The returned
// region is re-validated.
func (r Region) WithScheme(scheme string) (Region, error) {
	scheme = strings.ToLower(scheme)
	if scheme != "http" && scheme != "https" {
		return Region{}, &cxerrors.EndpointError{
			Reason: "scheme must be http or https, got " + scheme,
		}
	}
	r.Scheme = scheme
	if err := r.Validate(); err != nil {
		return Region{}, err
	}
	return r, nil
}

// Validate checks that the region's hosts and scheme are well-formed and
// returns an [EndpointError] if not. It is called automatically by the client
// builder; callers using [NewCustomRegion] do not need to call it explicitly.
func (r Region) Validate() error {
	if r.AuthHost == "" {
		return &cxerrors.EndpointError{Reason: "region: AuthHost is required"}
	}
	if r.APIHost == "" {
		return &cxerrors.EndpointError{Reason: "region: APIHost is required"}
	}
	if r.Scheme != "" && r.Scheme != "http" && r.Scheme != "https" {
		return &cxerrors.EndpointError{
			Reason: "region: Scheme must be http or https, got " + r.Scheme,
		}
	}
	for _, h := range []struct{ field, value string }{
		{"AuthHost", r.AuthHost},
		{"APIHost", r.APIHost},
	} {
		if strings.ContainsAny(h.value, " \t\r\n") {
			return &cxerrors.EndpointError{
				Reason: "region: " + h.field + " contains whitespace: " + h.value,
			}
		}
		if strings.Contains(h.value, "://") {
			return &cxerrors.EndpointError{
				Reason: "region: " + h.field + " must be a bare hostname (no scheme), got " + h.value,
			}
		}
		if strings.ContainsAny(h.value, "/?#") {
			return &cxerrors.EndpointError{
				Reason: "region: " + h.field + " must not contain a path or query, got " + h.value,
			}
		}
	}
	return nil
}

// scheme returns the effective URL scheme, defaulting to https when unset.
func (r Region) scheme() string {
	if r.Scheme == "" {
		return "https"
	}
	return r.Scheme
}

// TokenURL returns the OAuth/OIDC token endpoint for the given tenant:
//
//	{scheme}://{authHost}/auth/realms/{tenant}/protocol/openid-connect/token
func (r Region) TokenURL(tenant string) (string, error) {
	if tenant == "" {
		return "", &cxerrors.EndpointError{Reason: "tenant is required"}
	}
	u := &url.URL{
		Scheme: r.scheme(),
		Host:   r.AuthHost,
		Path:   "/auth/realms/" + tenant + "/protocol/openid-connect/token",
	}
	return u.String(), nil
}

// AuthAdminURL returns the IAM admin base URL for the given tenant:
//
//	{scheme}://{authHost}/auth/admin/realms/{tenant}/
func (r Region) AuthAdminURL(tenant string) (string, error) {
	if tenant == "" {
		return "", &cxerrors.EndpointError{Reason: "tenant is required"}
	}
	u := &url.URL{
		Scheme: r.scheme(),
		Host:   r.AuthHost,
		Path:   "/auth/admin/realms/" + tenant + "/",
	}
	return u.String(), nil
}

// APIBaseURL returns the API root used to construct endpoint URLs:
//
//	{scheme}://{apiHost}/api/
func (r Region) APIBaseURL() string {
	u := &url.URL{
		Scheme: r.scheme(),
		Host:   r.APIHost,
		Path:   "/api/",
	}
	return u.String()
}

// DisplayRootURL returns the host root used for building UI deep links:
//
//	{scheme}://{apiHost}/
func (r Region) DisplayRootURL() string {
	u := &url.URL{
		Scheme: r.scheme(),
		Host:   r.APIHost,
		Path:   "/",
	}
	return u.String()
}
