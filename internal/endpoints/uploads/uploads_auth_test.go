package uploads_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/uploads"
)

// countingAuth is a fake auth.Authenticator whose tokens change on Refresh.
type countingAuth struct {
	tokens    []string // tokens[gen-1] is the token for generation gen
	tokenCall atomic.Int32
	refreshes atomic.Int32
	tokenErr  error
}

func (a *countingAuth) Token(ctx context.Context) (string, uint64, error) {
	a.tokenCall.Add(1)
	if a.tokenErr != nil {
		return "", 0, a.tokenErr
	}
	return a.tokens[0], 1, nil
}

func (a *countingAuth) Refresh(ctx context.Context, stale uint64) (string, uint64, error) {
	a.refreshes.Add(1)
	if int(stale) >= len(a.tokens) {
		return a.tokens[len(a.tokens)-1], stale, nil
	}
	return a.tokens[stale], stale + 1, nil
}

// recordingTransport answers every request with status without any network
// and records what was sent.
type recordingTransport struct {
	status int
	reqs   []*http.Request
}

func (rt *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.reqs = append(rt.reqs, r)
	_, _ = io.Copy(io.Discard, r.Body)
	return &http.Response{
		StatusCode: rt.status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{},
		Request:    r,
	}, nil
}

const apiBase = "https://api.example.test/"

func TestPutFileAuthOriginMatching(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		wantAuth bool
	}{
		{"same origin", "https://api.example.test/storage/uploads.x/t/o?sig=1", true},
		{"uppercase host", "https://API.Example.TEST/storage/u/o", true},
		{"explicit default port", "https://api.example.test:443/storage/u/o", true},
		{"other host", "https://s3.amazonaws.com/bucket/obj?X-Amz-Signature=abc", false},
		{"suffix lookalike", "https://api.example.test.evil.com/storage/u/o", false},
		{"prefix lookalike", "https://evilapi.example.test/storage/u/o", false},
		{"subdomain", "https://x.api.example.test/storage/u/o", false},
		{"userinfo trick host is evil", "https://api.example.test@evil.com/storage/u/o", false},
		{"userinfo on real host", "https://evil:pw@api.example.test/storage/u/o", false},
		{"different port", "https://api.example.test:444/storage/u/o", false},
		{"http scheme mismatch", "http://api.example.test/storage/u/o", false},
		{"trailing dot fails closed", "https://api.example.test./storage/u/o", false},
		{"unicode lookalike", "https://api.examplе.test/storage/u/o", false},
		{"backslash trick", "https://evil.com\\@api.example.test/storage/u/o", false},
		{"unparseable", "https://api.example.test:99999999/x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &recordingTransport{status: 200}
			a := &countingAuth{tokens: []string{"SECRET-TOKEN"}}
			err := uploads.PutFileAuth(context.Background(), &http.Client{Transport: rt}, tc.url,
				bytes.NewReader([]byte("zip")), 3, &uploads.Auth{Authenticator: a, APIBaseURL: apiBase})
			if len(rt.reqs) == 0 {
				// request could not even be built (unparseable): must not have asked for a token.
				if tc.wantAuth {
					t.Fatalf("no request sent: %v", err)
				}
				return
			}
			got := rt.reqs[0].Header.Values("Authorization")
			if !tc.wantAuth {
				// net/http itself derives a Basic header from URL userinfo;
				// that is the URL's own credential, never the SDK token.
				var kept []string
				for _, g := range got {
					if !strings.HasPrefix(g, "Basic ") {
						kept = append(kept, g)
					}
				}
				got = kept
			}
			if tc.wantAuth {
				if len(got) != 1 || got[0] != "Bearer SECRET-TOKEN" {
					t.Fatalf("Authorization = %q, want exactly one Bearer header", got)
				}
			} else if len(got) != 0 {
				t.Fatalf("Authorization leaked to %s: %q", tc.url, got)
			}
			if !tc.wantAuth && a.tokenCall.Load() != 0 {
				t.Errorf("token fetched for a non-matching origin")
			}
		})
	}
}

func TestPutFileAuthHTTPAPIOriginAllowsHTTP(t *testing.T) {
	var gotAuth, gotCT string
	var gotLen int64
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotLen = r.ContentLength
		gotBody, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()
	a := &countingAuth{tokens: []string{"tok1"}}
	body := []byte("PK\x03\x04zip")
	err := uploads.PutFileAuth(context.Background(), srv.Client(), srv.URL+"/storage/uploads.x/t/o?sig=1",
		bytes.NewReader(body), int64(len(body)), &uploads.Auth{Authenticator: a, APIBaseURL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok1" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotCT != "application/zip" || gotLen != int64(len(body)) || !bytes.Equal(gotBody, body) {
		t.Errorf("ct=%q len=%d body=%q", gotCT, gotLen, gotBody)
	}
}

func TestPutFileAuthNilAuthNeverSendsBearer(t *testing.T) {
	rt := &recordingTransport{status: 200}
	for _, a := range []*uploads.Auth{nil, {}, {APIBaseURL: apiBase}} {
		err := uploads.PutFileAuth(context.Background(), &http.Client{Transport: rt},
			apiBase+"storage/u/o", bytes.NewReader([]byte("x")), 1, a)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range rt.reqs {
		if r.Header.Get("Authorization") != "" {
			t.Fatal("Authorization sent without an authenticator")
		}
	}
}

func TestPutFileAuth401RefreshesOnceWithSeekableBody(t *testing.T) {
	var seen []string
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if r.Header.Get("Authorization") == "Bearer old" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	a := &countingAuth{tokens: []string{"old", "new"}}
	err := uploads.PutFileAuth(context.Background(), srv.Client(), srv.URL+"/storage/u/o",
		bytes.NewReader([]byte("payload")), 7, &uploads.Auth{Authenticator: a, APIBaseURL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "Bearer old" || seen[1] != "Bearer new" {
		t.Fatalf("seen = %q", seen)
	}
	if bodies[0] != "payload" || bodies[1] != "payload" {
		t.Errorf("body not replayed in full: %q", bodies)
	}
	if a.refreshes.Load() != 1 {
		t.Errorf("refreshes = %d", a.refreshes.Load())
	}
}

func TestPutFileAuth401StillFailingIsCommunicationErrorAfterOneRefresh(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	a := &countingAuth{tokens: []string{"old", "new"}}
	err := uploads.PutFileAuth(context.Background(), srv.Client(), srv.URL+"/storage/u/o?sig=SUPERSECRETSIG",
		bytes.NewReader([]byte("p")), 1, &uploads.Auth{Authenticator: a, APIBaseURL: srv.URL + "/"})
	var ce *cxerrors.CommunicationError
	if !errors.As(err, &ce) || ce.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %T %v", err, err)
	}
	if n.Load() != 2 {
		t.Errorf("requests = %d, want 2", n.Load())
	}
	for _, s := range []string{"old", "new", "SUPERSECRETSIG", "Bearer"} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("error text contains %q: %v", s, err)
		}
	}
}

func TestPutFileAuth401NonSeekableBodyIsNotReplayed(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	a := &countingAuth{tokens: []string{"old", "new"}}
	body := io.MultiReader(strings.NewReader("abc"))
	err := uploads.PutFileAuth(context.Background(), srv.Client(), srv.URL+"/storage/u/o",
		body, 3, &uploads.Auth{Authenticator: a, APIBaseURL: srv.URL + "/"})
	var ce *cxerrors.CommunicationError
	if !errors.As(err, &ce) || ce.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %T %v", err, err)
	}
	if n.Load() != 1 || a.refreshes.Load() != 0 {
		t.Errorf("requests=%d refreshes=%d", n.Load(), a.refreshes.Load())
	}
}

func TestPutFileAuth403OnSameOriginIsCommunicationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	a := &countingAuth{tokens: []string{"tok"}}
	err := uploads.PutFileAuth(context.Background(), srv.Client(), srv.URL+"/storage/u/o",
		bytes.NewReader([]byte("p")), 1, &uploads.Auth{Authenticator: a, APIBaseURL: srv.URL + "/"})
	var ce *cxerrors.CommunicationError
	if !errors.As(err, &ce) || ce.StatusCode != http.StatusForbidden {
		t.Fatalf("err = %T %v", err, err)
	}
	if a.refreshes.Load() != 0 {
		t.Error("403 must not trigger a refresh")
	}
}

func TestPutFileAuthTokenErrorIsSurfaced(t *testing.T) {
	rt := &recordingTransport{status: 200}
	want := &cxerrors.AuthError{Reason: "boom"}
	a := &countingAuth{tokens: []string{"x"}, tokenErr: want}
	err := uploads.PutFileAuth(context.Background(), &http.Client{Transport: rt}, apiBase+"storage/u/o",
		bytes.NewReader([]byte("p")), 1, &uploads.Auth{Authenticator: a, APIBaseURL: apiBase})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
	if len(rt.reqs) != 0 {
		t.Error("request sent despite token failure")
	}
}

// A same-origin authed PUT must never follow redirects: Go would forward the
// Authorization header to subdomains of the original host and re-send the
// body on 307/308, so the safe behaviour is to surface the 3xx.
func TestPutFileAuthDoesNotFollowRedirects(t *testing.T) {
	var otherHits atomic.Int32
	var otherAuth atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherHits.Add(1)
		otherAuth.Store(r.Header.Get("Authorization"))
	}))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusTemporaryRedirect)
	}))
	defer api.Close()
	a := &countingAuth{tokens: []string{"SECRET-TOKEN"}}
	client := api.Client()
	err := uploads.PutFileAuth(context.Background(), client, api.URL+"/storage/u/o",
		bytes.NewReader([]byte("p")), 1, &uploads.Auth{Authenticator: a, APIBaseURL: api.URL + "/"})
	var ce *cxerrors.CommunicationError
	if !errors.As(err, &ce) || ce.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("err = %T %v", err, err)
	}
	if otherHits.Load() != 0 {
		t.Fatalf("redirect was followed (%d hits), auth=%v", otherHits.Load(), otherAuth.Load())
	}
	if client.CheckRedirect != nil {
		t.Error("caller's http.Client was mutated")
	}
}

// Without an authenticator (external object store) behaviour is unchanged:
// the client's own redirect policy applies and no Authorization is ever set.
func TestPutFileNoAuthRedirectCarriesNoAuthorization(t *testing.T) {
	var gotAuth atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
	}))
	defer other.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/x", http.StatusTemporaryRedirect)
	}))
	defer first.Close()
	a := &countingAuth{tokens: []string{"SECRET-TOKEN"}}
	// API origin is a different host than the (redirecting) presigned URL.
	err := uploads.PutFileAuth(context.Background(), first.Client(), first.URL+"/o",
		bytes.NewReader([]byte("p")), 1, &uploads.Auth{Authenticator: a, APIBaseURL: "https://api.example.test/"})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := gotAuth.Load().(string); v != "" {
		t.Errorf("Authorization = %q after redirect", v)
	}
}

func TestPutFileAuthWithTimeout(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer srv.Close()
	c := *srv.Client()
	c.Timeout = time.Nanosecond // must be overridden by the explicit deadline
	a := &countingAuth{tokens: []string{"tok"}}
	err := uploads.PutFileAuthWithTimeout(context.Background(), &c, srv.URL+"/storage/u/o",
		bytes.NewReader([]byte("p")), 1, &uploads.Auth{Authenticator: a, APIBaseURL: srv.URL + "/"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
	if err := uploads.PutFileAuthWithTimeout(context.Background(), &c, srv.URL, bytes.NewReader([]byte("p")), 1, nil, 0); err == nil {
		t.Error("zero timeout must be rejected")
	}
}
