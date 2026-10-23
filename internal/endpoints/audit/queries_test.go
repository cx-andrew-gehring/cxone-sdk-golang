package audit_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/audit"
)

func TestGetSASTQueries(t *testing.T) {
	var gotPath, gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"queryLanguages":[{"name":"Go","queryGroups":[]}]}`)
	})
	coll, err := audit.GetSASTQueries(context.Background(), exec, base, "sess-1", "project", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(coll.QueryLanguages) != 1 {
		t.Errorf("languages = %+v", coll.QueryLanguages)
	}
	if !strings.Contains(gotPath, "sessions/sess-1/queries") {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "projectId=p1") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestGetIACQueries(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"platforms":[{"name":"Terraform","queryGroups":[]}]}`)
	})
	coll, err := audit.GetIACQueries(context.Background(), exec, base, "sess-1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(coll.Platforms) != 1 {
		t.Errorf("platforms = %+v", coll.Platforms)
	}
}

func TestGetQueryTree(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"isLeaf":false,"title":"SQL Injection","key":"k1","children":[{"isLeaf":true,"title":"Query1","key":"k2"}]}]`)
	})
	tree, err := audit.GetQueryTree(context.Background(), exec, base, "sess-1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].Title != "SQL Injection" {
		t.Errorf("tree = %+v", tree)
	}
	if len(tree[0].Children) != 1 || !tree[0].Children[0].IsLeaf {
		t.Errorf("children = %+v", tree[0].Children)
	}
}

func TestDeleteQueryOverride(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := audit.DeleteQueryOverride(context.Background(), exec, base, "sess-1", "query-key-123")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotPath, "/queries/query-key-123") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestCreateQueryOverride(t *testing.T) {
	var gotMethod, gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
	})
	body := map[string]string{"name": "MyQuery", "source": "result = base.Find(...)"}
	err := audit.CreateQueryOverride(context.Background(), exec, base, "sess-1", body)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotBody, `"name":"MyQuery"`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestUpdateQuerySource(t *testing.T) {
	var gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `[]`)
	})
	failures, err := audit.UpdateQuerySource(context.Background(), exec, base, "sess-1", "qkey", map[string]string{"source": "new code"})
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Errorf("failures = %+v", failures)
	}
	if !strings.Contains(gotPath, "/queries/qkey/source") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestUpdateQueryMetadata(t *testing.T) {
	var gotMethod, gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := audit.UpdateQueryMetadata(context.Background(), exec, base, "sess-1", "qkey", map[string]string{"severity": "High"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotPath, "/queries/qkey/metadata") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestValidateQuerySource(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/queries/validate") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"query_id":"q1","error":[{"line":5,"message":"syntax error"}]}]`)
	})
	failures, err := audit.ValidateQuerySource(context.Background(), exec, base, "sess-1", map[string]string{"source": "bad code"})
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].QueryID != "q1" {
		t.Errorf("failures = %+v", failures)
	}
	if len(failures[0].Errors) != 1 || failures[0].Errors[0].Line != 5 {
		t.Errorf("errors = %+v", failures[0].Errors)
	}
}

func TestRunQuery(t *testing.T) {
	var gotPath string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"query_id":"q1","error":[]}`)
	})
	result, err := audit.RunQuery(context.Background(), exec, base, "sess-1", map[string]string{"key": "qkey", "source": "code"})
	if err != nil {
		t.Fatal(err)
	}
	if result.QueryID != "q1" {
		t.Errorf("queryID = %q", result.QueryID)
	}
	if !strings.HasSuffix(gotPath, "/queries/run") {
		t.Errorf("path = %q", gotPath)
	}
}
