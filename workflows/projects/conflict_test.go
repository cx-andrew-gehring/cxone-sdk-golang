package projects_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/projects"
)

// Body recorded on the canary (S5, 002-create-duplicate): HTTP 400, code 208.
const duplicateBody = `{"code":208,"message":"Failed to create a project, project name 'acme' already exists","type":"ERROR"}`

func init() {
	projects.SetConflictLookupBackoffForTest([]time.Duration{time.Millisecond, time.Millisecond, time.Millisecond})
}

// The winner becomes visible only on the 3rd lookup after the duplicate 400
// (eventual consistency): the bounded re-lookup finds it.
func TestGetOrCreateByNameDuplicateRetriesLookup(t *testing.T) {
	var lists int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if atomic.AddInt32(&lists, 1) < 4 { // initial lookup + 2 misses
				_, _ = io.WriteString(w, `{"totalCount":0,"filteredTotalCount":0,"projects":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"totalCount":1,"filteredTotalCount":1,"projects":[{"id":"winner","name":"acme"}]}`)
		case http.MethodPost:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, duplicateBody)
		}
	})
	got, err := projects.GetOrCreateByName(context.Background(), b, &models.Project{Name: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "winner" || atomic.LoadInt32(&lists) != 4 {
		t.Fatalf("id=%s lists=%d", got.ID(), lists)
	}
}

// Still invisible after the bounded re-lookups: a typed ConflictError that
// keeps the 400 ResponseError reachable.
func TestGetOrCreateByNameDuplicateNeverVisibleIsConflictError(t *testing.T) {
	var lists int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			atomic.AddInt32(&lists, 1)
			_, _ = io.WriteString(w, `{"totalCount":0,"filteredTotalCount":0,"projects":[]}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, duplicateBody)
	})
	_, err := projects.GetOrCreateByName(context.Background(), b, &models.Project{Name: "acme"})
	var ce *cxerrors.ConflictError
	if !errors.As(err, &ce) || ce.Name != "acme" {
		t.Fatalf("err=%v (%T), want *cxerrors.ConflictError", err, err)
	}
	var re *cxerrors.ResponseError
	if !errors.As(err, &re) || re.StatusCode != 400 {
		t.Fatalf("ResponseError not reachable: %v", err)
	}
	if n := atomic.LoadInt32(&lists); n != 4 { // 1 initial + 3 bounded re-lookups
		t.Fatalf("lists=%d want 4", n)
	}
}

// A 400 that is not a duplicate stays the plain ResponseError (no ConflictError).
func TestGetOrCreateByNameOther400NotConflict(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"totalCount":0,"filteredTotalCount":0,"projects":[]}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":101,"message":"invalid tag"}`)
	})
	_, err := projects.GetOrCreateByName(context.Background(), b, &models.Project{Name: "acme"})
	var ce *cxerrors.ConflictError
	if err == nil || errors.As(err, &ce) {
		t.Fatalf("err=%v", err)
	}
}

// The re-lookup backoff honours ctx.
func TestGetOrCreateByNameDuplicateHonoursContext(t *testing.T) {
	projects.SetConflictLookupBackoffForTest([]time.Duration{time.Hour})
	defer projects.SetConflictLookupBackoffForTest([]time.Duration{time.Millisecond, time.Millisecond, time.Millisecond})
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"totalCount":0,"filteredTotalCount":0,"projects":[]}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, duplicateBody)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := projects.GetOrCreateByName(ctx, b, &models.Project{Name: "acme"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v elapsed=%v", err, time.Since(start))
	}
}
