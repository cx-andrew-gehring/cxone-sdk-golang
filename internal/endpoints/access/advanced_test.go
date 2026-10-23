package access_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/access"
)

func TestGetMyGroups(t *testing.T) {
	var gotPath, gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":"g1","name":"my-group"}]`)
	})
	groups, err := access.GetMyGroups(context.Background(), exec, base, "test", true, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Name != "my-group" {
		t.Errorf("groups = %+v", groups)
	}
	if !strings.HasSuffix(gotPath, "/my-groups") {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "search=test") || !strings.Contains(gotQuery, "subgroups=true") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestGetAvailableGroups(t *testing.T) {
	var gotQuery string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":"g1","name":"available-group"}]`)
	})
	groups, err := access.GetAvailableGroups(context.Background(), exec, base, "p1", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Errorf("groups = %+v", groups)
	}
	if !strings.Contains(gotQuery, "project-id=p1") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestListAMGroups(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/groups") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"g1","name":"am-group"}]`)
	})
	groups, err := access.ListAMGroups(context.Background(), exec, base, "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Errorf("groups = %+v", groups)
	}
}

func TestListAMUsers(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"u1","username":"alice"}]`)
	})
	users, err := access.ListAMUsers(context.Background(), exec, base, "alice", 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Username != "alice" {
		t.Errorf("users = %+v", users)
	}
}

func TestListPermissions(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/permissions") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"perm1","name":"manage-scan","category":"scanning"}]`)
	})
	perms, err := access.ListPermissions(context.Background(), exec, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(perms) != 1 || perms[0].Name != "manage-scan" {
		t.Errorf("perms = %+v", perms)
	}
}

func TestListAMRoles(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/roles") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":"r1","name":"Admin","systemRole":true}]`)
	})
	roles, err := access.ListAMRoles(context.Background(), exec, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Name != "Admin" {
		t.Errorf("roles = %+v", roles)
	}
}

func TestListAMClients(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"c1","clientId":"my-client"}]`)
	})
	clients, err := access.ListAMClients(context.Background(), exec, base, "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 {
		t.Errorf("clients = %+v", clients)
	}
}
