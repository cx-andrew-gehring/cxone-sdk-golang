package presetmanager_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/presetmanager"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newExec(t *testing.T, h http.HandlerFunc) (*transport.Executor, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	exec := transport.NewExecutor(transport.Config{
		HTTPClient:    srv.Client(),
		Authenticator: stubAuth{},
		UserAgent:     "test",
		CorrelationID: "corr",
		RetryPolicy:   retry.Policy{MaxAttempts: 1, MaxDelay: 0},
	})
	return exec, srv.URL + "/"
}

func TestListPresets(t *testing.T) {
	var gotPath, gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"presets":[{"id":"p1","name":"Default","custom":false}],"totalCount":1}`)
	})
	resp, err := presetmanager.ListPresets(context.Background(), exec, base, "sast", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Presets) != 1 || resp.Presets[0].Name != "Default" {
		t.Errorf("presets = %+v", resp.Presets)
	}
	if !strings.Contains(gotPath, "/preset-manager/sast/presets") {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "limit=100") || !strings.Contains(gotQuery, "include_details=true") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestGetPreset(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/presets/p1") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"p1","name":"Default","queries":[{"familyName":"SQL Injection","totalCount":5}]}`)
	})
	p, err := presetmanager.GetPreset(context.Background(), exec, base, "sast", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Default" {
		t.Errorf("name = %q", p.Name)
	}
	if len(p.QueryFamilies) != 1 {
		t.Errorf("families = %+v", p.QueryFamilies)
	}
}

func TestSearchPreset(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"presets":[{"id":"p1","name":"Checkmarx Default"}],"totalCount":1}`)
	})
	resp, err := presetmanager.SearchPreset(context.Background(), exec, base, "sast", "Checkmarx Default", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Presets) != 1 {
		t.Errorf("presets = %+v", resp.Presets)
	}
	if !strings.Contains(gotQuery, "search-term=Checkmarx") {
		t.Errorf("query = %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "exact-match=true") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestCreatePreset(t *testing.T) {
	var gotMethod, gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"p-new","name":"Custom","custom":true}`)
	})
	p, err := presetmanager.CreatePreset(context.Background(), exec, base, "sast", &models.PresetCreateRequest{
		Name:     "Custom",
		QueryIDs: []string{"q1", "q2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if p.ID != "p-new" {
		t.Errorf("id = %q", p.ID)
	}
	if !strings.Contains(gotBody, `"name":"Custom"`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestUpdatePreset(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := presetmanager.UpdatePreset(context.Background(), exec, base, "sast", "p1", &models.PresetCreateRequest{Name: "Updated"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/presets/p1") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestDeletePreset(t *testing.T) {
	var gotMethod string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := presetmanager.DeletePreset(context.Background(), exec, base, "iac", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestListQueryFamilies(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/query-families") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `["SQL Injection","XSS","Buffer Overflow"]`)
	})
	families, err := presetmanager.ListQueryFamilies(context.Background(), exec, base, "sast")
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 3 {
		t.Errorf("families = %v", families)
	}
}

func TestGetSASTQueryFamilyContents(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"queryLanguages":[{"name":"CSharp","queryGroups":[{"name":"SQL Injection","queries":[{"queryID":"1","queryName":"SQL_Injection"}]}]}]}`)
	})
	coll, err := presetmanager.GetSASTQueryFamilyContents(context.Background(), exec, base, "SQL Injection")
	if err != nil {
		t.Fatal(err)
	}
	if len(coll.QueryLanguages) != 1 {
		t.Errorf("languages = %+v", coll.QueryLanguages)
	}
}

func TestGetIACQueryFamilyContents(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"platforms":[{"name":"Terraform","queryGroups":[{"name":"Encryption","queries":[{"queryId":"q1","name":"UnencryptedBucket"}]}]}]}`)
	})
	coll, err := presetmanager.GetIACQueryFamilyContents(context.Background(), exec, base, "Encryption")
	if err != nil {
		t.Fatal(err)
	}
	if len(coll.Platforms) != 1 || coll.Platforms[0].Name != "Terraform" {
		t.Errorf("platforms = %+v", coll.Platforms)
	}
}

func TestValidation(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	})
	_, err := presetmanager.ListPresets(context.Background(), exec, base, "", 10)
	if err == nil {
		t.Error("expected error for empty engine")
	}
	_, err = presetmanager.GetPreset(context.Background(), exec, base, "sast", "")
	if err == nil {
		t.Error("expected error for empty presetID")
	}
}
