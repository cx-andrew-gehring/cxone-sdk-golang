package uploads_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/uploads"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
)

type stubAuth struct{}

func (stubAuth) Token(ctx context.Context) (string, uint64, error)         { return "tok", 1, nil }
func (stubAuth) Refresh(ctx context.Context, _ uint64) (string, uint64, error) { return "tok", 1, nil }

func newTestExecutor(t *testing.T, h http.HandlerFunc) (*transport.Executor, *http.Client, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	exec := transport.NewExecutor(transport.Config{
		HTTPClient:    srv.Client(),
		Authenticator: stubAuth{},
		UserAgent:     "test/(CxOne GoSDK/0.0.0-test)",
		CorrelationID: "corr",
		RetryPolicy:   retry.Policy{MaxAttempts: 1, MaxDelay: 0},
	})
	return exec, srv.Client(), srv.URL + "/"
}

func TestGetPresignedURLSuccess(t *testing.T) {
	exec, _, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/uploads" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"url":"https://store.example.com/u/abc?sig=123"}`)
	})
	url, err := uploads.GetPresignedURL(context.Background(), exec, base)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://store.example.com/u/abc?sig=123" {
		t.Errorf("url = %q", url)
	}
}

func TestGetPresignedURLEmptyURLIsResponseError(t *testing.T) {
	exec, _, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	})
	_, err := uploads.GetPresignedURL(context.Background(), exec, base)
	var rerr *cxerrors.ResponseError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *ResponseError, got %T: %v", err, err)
	}
}

func TestPutFileSendsBodyAndContentLength(t *testing.T) {
	var (
		gotMethod   string
		gotCT       string
		gotLen      int64
		gotBody     []byte
		gotAuthHdr  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotLen = r.ContentLength
		gotAuthHdr = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := []byte("PK\x03\x04zipcontent")
	err := uploads.PutFile(context.Background(), srv.Client(), srv.URL+"/u/abc?sig=1", bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q", gotMethod)
	}
	if gotCT != "application/zip" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotLen != int64(len(body)) {
		t.Errorf("content-length = %d, want %d", gotLen, len(body))
	}
	if !strings.HasPrefix(string(gotBody), "PK") {
		t.Errorf("body = %q", string(gotBody))
	}
	if gotAuthHdr != "" {
		t.Errorf("Authorization header should NOT be sent to presigned URL, got %q", gotAuthHdr)
	}
}

func TestPutFileNon2xxIsCommunicationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<Error>SignatureDoesNotMatch</Error>`)
	}))
	defer srv.Close()

	body := []byte("hello")
	err := uploads.PutFile(context.Background(), srv.Client(), srv.URL+"/u", bytes.NewReader(body), int64(len(body)))
	var commErr *cxerrors.CommunicationError
	if !errors.As(err, &commErr) {
		t.Fatalf("expected *CommunicationError, got %T: %v", err, err)
	}
	if commErr.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d", commErr.StatusCode)
	}
}

func TestPutFileValidatesArgs(t *testing.T) {
	cases := []struct {
		name string
		fn   func() error
	}{
		{
			"nil client",
			func() error {
				return uploads.PutFile(context.Background(), nil, "http://x", bytes.NewReader([]byte("a")), 1)
			},
		},
		{
			"empty url",
			func() error {
				return uploads.PutFile(context.Background(), http.DefaultClient, "", bytes.NewReader([]byte("a")), 1)
			},
		},
		{
			"nil body",
			func() error {
				return uploads.PutFile(context.Background(), http.DefaultClient, "http://x", nil, 1)
			},
		},
		{
			"zero size",
			func() error {
				return uploads.PutFile(context.Background(), http.DefaultClient, "http://x", bytes.NewReader([]byte("a")), 0)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			var ce *cxerrors.ConfigurationError
			if !errors.As(err, &ce) {
				t.Fatalf("expected *ConfigurationError, got %T: %v", err, err)
			}
		})
	}
}

const secretSig = "SECRETSIG123"

func assertNoSecret(t *testing.T, err error) {
	t.Helper()
	for _, s := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), fmt.Sprintf("%v", errors.Unwrap(err))} {
		if strings.Contains(s, secretSig) {
			t.Errorf("presigned signature leaked: %s", s)
		}
	}
	var ce *cxerrors.CommunicationError
	if errors.As(err, &ce) {
		if strings.Contains(ce.URL, secretSig) || strings.Contains(ce.URL, "?") {
			t.Errorf("CommunicationError.URL not sanitized: %q", ce.URL)
		}
		if ce.Cause != nil {
			if s := fmt.Sprintf("%v %+v %#v", ce.Cause, ce.Cause, ce.Cause); strings.Contains(s, secretSig) {
				t.Errorf("cause leaks signature: %s", s)
			}
		}
	}
}

func TestPutFile403DoesNotLeakSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	err := uploads.PutFile(context.Background(), srv.Client(), srv.URL+"/b/obj?X-Amz-Signature="+secretSig, bytes.NewReader([]byte("x")), 1)
	if err == nil {
		t.Fatal("want error")
	}
	assertNoSecret(t, err)
}

func TestPutFileTransportErrorDoesNotLeakSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	u := srv.URL + "/b/obj?X-Amz-Signature=" + secretSig
	client := srv.Client()
	srv.Close() // connection refused
	err := uploads.PutFile(context.Background(), client, u, bytes.NewReader([]byte("x")), 1)
	if err == nil {
		t.Fatal("want error")
	}
	assertNoSecret(t, err)
	if !errors.Is(err, cxerrors.ErrCommunication) {
		t.Errorf("not a CommunicationError: %v", err)
	}
}

func TestPutFileBuildRequestErrorDoesNotLeakSignature(t *testing.T) {
	err := uploads.PutFile(context.Background(), http.DefaultClient, "http://user:pw"+secretSig+"@exa mple.com/x?sig="+secretSig, bytes.NewReader([]byte("x")), 1)
	if err == nil {
		t.Fatal("want error")
	}
	assertNoSecret(t, err)
	if strings.Contains(err.Error(), "pw"+secretSig) {
		t.Errorf("userinfo leaked: %v", err)
	}
}
