package projects_test

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/projects"
)

// A concurrent creator wins between our lookup and create: the create gets a
// 4xx conflict; GetOrCreateByName must re-run the exact-name lookup once and
// return the winner instead of failing.
func TestGetOrCreateByNameRecoversFromCreateRace(t *testing.T) {
	var lists, creates int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			if atomic.AddInt32(&lists, 1) == 1 {
				_, _ = io.WriteString(w, `{"totalCount":0,"filteredTotalCount":0,"projects":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"totalCount":1,"filteredTotalCount":1,"projects":[{"id":"winner","name":"acme"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects":
			atomic.AddInt32(&creates, 1)
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"code":409,"message":"project name already exists"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	got, err := projects.GetOrCreateByName(context.Background(), b, &models.Project{Name: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "winner" || atomic.LoadInt32(&lists) != 2 || atomic.LoadInt32(&creates) != 1 {
		t.Errorf("id=%s lists=%d creates=%d", got.ID(), lists, creates)
	}
}

func TestGetOrCreateByNameConflictWithoutWinnerReturnsError(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"totalCount":0,"filteredTotalCount":0,"projects":[]}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"bad"}`)
	})
	if _, err := projects.GetOrCreateByName(context.Background(), b, &models.Project{Name: "acme"}); err == nil {
		t.Fatal("want error")
	}
}
