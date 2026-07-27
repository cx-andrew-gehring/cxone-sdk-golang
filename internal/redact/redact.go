// Package redact provides helpers that strip Checkmarx One bearer tokens and
// auth-flow form-body secrets from arbitrary strings before they are emitted
// to errors, logs, or trace attributes.
//
// CLAUDE.md §7 makes redaction mandatory: any string of the form "Bearer …",
// or any client_secret / refresh_token / api_key form field, must be replaced
// with the literal "REDACTED" before crossing the SDK's public boundary.
package redact

import (
	"net/url"
	"regexp"
	"strings"
)

const placeholder = "REDACTED"

// bearerRe matches "Bearer <token>" case-insensitively. The token may contain
// any non-whitespace characters; we stop at whitespace, comma, or end-of-string.
var bearerRe = regexp.MustCompile(`(?i)Bearer\s+[^\s,"']+`)

// formSecretRe matches form-encoded secret fields. The capture group keeps
// the field name so the replacement can preserve it.
var formSecretRe = regexp.MustCompile(`(?i)(client_secret|refresh_token|api_key)=[^&\s"']*`)

// jsonSecretRe matches JSON-encoded secret fields.
// e.g. "client_secret":"abc"  →  "client_secret":"REDACTED"
//
// The bare "token" alternative covers the repos-manager project-conversion
// request body, which carries the caller's SCM access token under that name.
// It cannot collide with refresh_token / access_token: the pattern anchors on
// the opening quote, so those only match their own alternatives.
var jsonSecretRe = regexp.MustCompile(`(?i)"(client_secret|refresh_token|api_key|access_token|password|token)"\s*:\s*"[^"]*"`)

// String strips bearer tokens and known secret fields from s.
//
// It is safe to call on arbitrary user-supplied strings. The function performs
// only string substitution; it never panics and never allocates if no match
// is found.
func String(s string) string {
	if s == "" {
		return s
	}
	if bearerRe.MatchString(s) {
		s = bearerRe.ReplaceAllString(s, "Bearer "+placeholder)
	}
	if formSecretRe.MatchString(s) {
		s = formSecretRe.ReplaceAllString(s, "${1}="+placeholder)
	}
	if jsonSecretRe.MatchString(s) {
		s = jsonSecretRe.ReplaceAllString(s, `"${1}":"`+placeholder+`"`)
	}
	return s
}

// URL returns a copy of u with sensitive query parameters and userinfo
// stripped. Returns the input unchanged if it cannot be parsed.
func URL(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return String(raw)
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), placeholder)
	}
	if q := u.Query(); len(q) > 0 {
		changed := false
		for _, k := range []string{"client_secret", "refresh_token", "api_key", "access_token", "token"} {
			if q.Has(k) {
				q.Set(k, placeholder)
				changed = true
			}
		}
		if changed {
			u.RawQuery = q.Encode()
		}
	}
	return u.String()
}

// Header returns a copy of headers safe for logging.
//
// The Authorization, Cookie, Set-Cookie and Proxy-Authorization headers are
// fully replaced with the placeholder. Other headers are passed through
// String() so any inline secrets are stripped.
func Header(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return headers
	}
	out := make(map[string][]string, len(headers))
	for k, v := range headers {
		switch strings.ToLower(k) {
		case "authorization", "cookie", "set-cookie", "proxy-authorization":
			out[k] = []string{placeholder}
		default:
			cleaned := make([]string, len(v))
			for i, s := range v {
				cleaned[i] = String(s)
			}
			out[k] = cleaned
		}
	}
	return out
}
