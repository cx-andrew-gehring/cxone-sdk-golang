package scm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scm"
)

// recorder captures the sequence of request paths so tests can assert the
// disconnect-then-convert ordering.
type recorder struct {
	mu     sync.Mutex
	paths  []string
	bodies []string
}

func (r *recorder) record(path, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, path)
	r.bodies = append(r.bodies, body)
}

func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...)
}

func validReconnect() scm.ReconnectRequest {
	return scm.ReconnectRequest{
		SCMType:           models.SCMTypeGitHub,
		OrgIdentity:       "acme",
		RepoURL:           "https://github.com/acme/new-repo",
		ProtectedBranches: []string{"main"},
	}
}

// routingBackend serves the disconnect and conversion endpoints, recording
// each call. convertStatus lets a test make the conversion fail.
func routingBackend(t *testing.T, rec *recorder, convertStatus int) *scm.Backend {
	t.Helper()
	return newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.record(r.URL.Path, string(b))

		switch {
		case strings.HasSuffix(r.URL.Path, "/disconnect"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/project-conversion"):
			if convertStatus != http.StatusOK {
				w.WriteHeader(convertStatus)
				_, _ = io.WriteString(w, `{"message":"a conversion is already running for this organization"}`)
				return
			}
			_, _ = io.WriteString(w, `{"processId":"p1","message":"Started to convert projects."}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestReconnectDisconnectsThenConverts(t *testing.T) {
	rec := &recorder{}
	b := routingBackend(t, rec, http.StatusOK)

	insp, err := scm.Reconnect(context.Background(), b, "proj-1", validReconnect())
	if err != nil {
		t.Fatal(err)
	}
	if insp.ID() != "p1" {
		t.Errorf("processID = %q", insp.ID())
	}

	paths := rec.seen()
	if len(paths) != 2 {
		t.Fatalf("expected 2 calls, got %v", paths)
	}
	if !strings.HasSuffix(paths[0], "/projects/proj-1/disconnect") {
		t.Errorf("first call should be the disconnect, got %q", paths[0])
	}
	if !strings.HasSuffix(paths[1], "/project-conversion") {
		t.Errorf("second call should be the conversion, got %q", paths[1])
	}

	// The conversion body must carry the project id and target repo.
	var sent models.SCMConversionRequest
	if err := json.Unmarshal([]byte(rec.bodies[1]), &sent); err != nil {
		t.Fatalf("decode conversion body: %v", err)
	}
	if len(sent.Projects) != 1 {
		t.Fatalf("expected exactly 1 project, got %d", len(sent.Projects))
	}
	if sent.Projects[0].CxProjectID != "proj-1" {
		t.Errorf("cxProjectId = %q", sent.Projects[0].CxProjectID)
	}
	if sent.Projects[0].SCMRepositoryURL != "https://github.com/acme/new-repo" {
		t.Errorf("scmRepositoryUrl = %q", sent.Projects[0].SCMRepositoryURL)
	}
}

// The returned inspector must not have been populated by an extra GET: the
// process may not be queryable immediately after the POST.
func TestReconnectDoesNotPollImmediately(t *testing.T) {
	rec := &recorder{}
	b := routingBackend(t, rec, http.StatusOK)

	insp, err := scm.Reconnect(context.Background(), b, "proj-1", validReconnect())
	if err != nil {
		t.Fatal(err)
	}
	if insp.Data() != nil {
		t.Error("expected an unfetched inspector")
	}
	if insp.Terminal() {
		t.Error("an unfetched inspector must not be terminal")
	}
	if len(rec.seen()) != 2 {
		t.Errorf("expected exactly 2 calls, got %v", rec.seen())
	}
}

func TestReconnectSkipDisconnect(t *testing.T) {
	rec := &recorder{}
	b := routingBackend(t, rec, http.StatusOK)

	req := validReconnect()
	req.SkipDisconnect = true
	if _, err := scm.Reconnect(context.Background(), b, "proj-1", req); err != nil {
		t.Fatal(err)
	}

	paths := rec.seen()
	if len(paths) != 1 {
		t.Fatalf("expected only the conversion call, got %v", paths)
	}
	if !strings.HasSuffix(paths[0], "/project-conversion") {
		t.Errorf("expected the conversion call, got %q", paths[0])
	}
}

// A failing conversion must surface the error; the disconnect has already
// happened at that point, which is the non-atomic case callers must handle.
func TestReconnectConversionFailureSurfaces(t *testing.T) {
	rec := &recorder{}
	b := routingBackend(t, rec, http.StatusConflict)

	_, err := scm.Reconnect(context.Background(), b, "proj-1", validReconnect())
	if err == nil {
		t.Fatal("expected error when the conversion is rejected")
	}
	if len(rec.seen()) != 2 {
		t.Errorf("disconnect should still have run: %v", rec.seen())
	}
}

// Validation must short-circuit before ANY call — a malformed request must
// never leave a project disconnected.
func TestReconnectValidationRunsBeforeDisconnect(t *testing.T) {
	noRepo := validReconnect()
	noRepo.RepoURL = ""

	dotGit := validReconnect()
	dotGit.RepoURL = "https://github.com/acme/new-repo.git"

	noBranches := validReconnect()
	noBranches.ProtectedBranches = nil

	autoScan := validReconnect()
	autoScan.AutoScan = true

	noOrg := validReconnect()
	noOrg.OrgIdentity = ""

	noType := validReconnect()
	noType.SCMType = ""

	cases := []struct {
		name string
		req  scm.ReconnectRequest
	}{
		{"missing repo url", noRepo},
		{"repo url with .git suffix", dotGit},
		{"no protected branches", noBranches},
		{"auto scan without branch", autoScan},
		{"missing org", noOrg},
		{"missing scm type", noType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("should not reach server: %s", r.URL.Path)
			})
			if _, err := scm.Reconnect(context.Background(), b, "proj-1", tc.req); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestReconnectRequiresProjectIDAndBackend(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	})
	if _, err := scm.Reconnect(context.Background(), b, "", validReconnect()); err == nil {
		t.Error("expected error for empty projectID")
	}
	if _, err := scm.Reconnect(context.Background(), nil, "proj-1", validReconnect()); err == nil {
		t.Error("expected error for nil backend")
	}
}

func TestDisconnectValidation(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	})
	if err := scm.Disconnect(context.Background(), b, ""); err == nil {
		t.Error("expected error for empty projectID")
	}
	if err := scm.Disconnect(context.Background(), nil, "proj-1"); err == nil {
		t.Error("expected error for nil backend")
	}
}
