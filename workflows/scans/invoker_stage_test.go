package scans_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/projects"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

// A failure before the scan-create request is sent (presign or PUT) is an
// *UploadStageError; a failure of the scan-create call itself is not.
func TestInvokerUploadStageError(t *testing.T) {
	cases := []struct {
		name               string
		presign, put, make int
		want               bool
	}{
		{"presign 400", 400, 0, 0, true},
		{"put 500", 0, 500, 0, true},
		{"create 500", 0, 0, 500, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/uploads":
					if c.presign != 0 {
						w.WriteHeader(c.presign)
						return
					}
					_, _ = io.WriteString(w, `{"url":"http://`+r.Host+`/put/abc"}`)
				case r.Method == http.MethodPut:
					_, _ = io.Copy(io.Discard, r.Body)
					if c.put != 0 {
						w.WriteHeader(c.put)
						return
					}
				case r.URL.Path == "/api/scans":
					if c.make != 0 {
						w.WriteHeader(c.make)
						return
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{"id":"s","status":"Queued"}`)
				}
			}))
			defer srv.Close()
			exec := transport.NewExecutor(transport.Config{HTTPClient: srv.Client(), Authenticator: stubAuth{}, UserAgent: "t", RetryPolicy: retry.Policy{MaxAttempts: 1}})
			sb := &scans.Backend{Executor: exec, BaseURL: srv.URL + "/"}
			repo, _ := projects.FromProjectModel(&projects.Backend{Executor: exec, BaseURL: srv.URL + "/"}, &models.ProjectResponseModel{ID: "p1", RepoURL: "g", MainBranch: "main"})
			_, err := scans.NewInvoker(sb, repo, srv.Client()).WithEngines("sast").ForBranch("b").
				WithUploadReader(bytes.NewReader([]byte("zip")), 3).Start(context.Background())
			if err == nil {
				t.Fatal("want error")
			}
			var ue *scans.UploadStageError
			if got := errors.As(err, &ue); got != c.want {
				t.Errorf("UploadStageError=%v want %v (%v)", got, c.want, err)
			}
		})
	}
}
