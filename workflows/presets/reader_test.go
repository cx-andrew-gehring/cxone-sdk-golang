package presets_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/presets"
)

type stubAuth struct{}

func (stubAuth) Token(context.Context) (string, uint64, error)           { return "tok", 1, nil }
func (stubAuth) Refresh(context.Context, uint64) (string, uint64, error) { return "tok", 1, nil }

func newReader(t *testing.T, h http.HandlerFunc) *presets.PresetReader {
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
	return presets.NewReader(&presets.Backend{Executor: exec, BaseURL: srv.URL + "/"})
}

func TestPresetReaderListLazyLoadsLevel1(t *testing.T) {
	var hits int32
	r := newReader(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/presets" {
			t.Errorf("path = %q", r.URL.Path)
		}
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `{"totalCount":2,"presets":[{"id":"1","name":"Bravo"},{"id":"2","name":"Alpha"}]}`)
	})
	all, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %v", all)
	}
	// Sorted by name → Alpha first.
	if all[0].Name() != "Alpha" || all[1].Name() != "Bravo" {
		t.Errorf("not sorted by name: %v", []string{all[0].Name(), all[1].Name()})
	}
	// Level-1 cache: second List call should not hit the server.
	_, _ = r.List(context.Background())
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("level-1 cache miss: hits=%d", got)
	}
}

func TestPresetReaderByNameAndByID(t *testing.T) {
	r := newReader(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"presets":[{"id":"id-1","name":"ASA Premium"},{"id":"id-2","name":"Default"}]}`)
	})
	byName, err := r.ByName(context.Background(), "ASA Premium")
	if err != nil {
		t.Fatal(err)
	}
	if byName == nil || byName.ID() != "id-1" {
		t.Errorf("byName = %+v", byName)
	}
	byID, err := r.ByID(context.Background(), "id-2")
	if err != nil {
		t.Fatal(err)
	}
	if byID == nil || byID.Name() != "Default" {
		t.Errorf("byID = %+v", byID)
	}
	// Missing name → nil, no error.
	missing, err := r.ByName(context.Background(), "doesnotexist")
	if err != nil || missing != nil {
		t.Errorf("missing: %+v err=%v", missing, err)
	}
}

func TestPresetDetailLazyLoadsLevel2(t *testing.T) {
	var detailHits int32
	r := newReader(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/presets":
			_, _ = io.WriteString(w, `{"presets":[{"id":"id-1","name":"P1"}]}`)
		case strings.HasPrefix(r.URL.Path, "/api/presets/"):
			atomic.AddInt32(&detailHits, 1)
			_, _ = io.WriteString(w, `{"id":"id-1","name":"P1","queryIds":["q1","q2","q3"]}`)
		}
	})
	preset, err := r.ByName(context.Background(), "P1")
	if err != nil {
		t.Fatal(err)
	}
	if preset == nil {
		t.Fatal("preset not found")
	}
	// Detail not loaded yet.
	if got := atomic.LoadInt32(&detailHits); got != 0 {
		t.Errorf("detail loaded prematurely: hits=%d", got)
	}
	// First Detail call: loads.
	d, err := preset.Detail(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.QueryIDs) != 3 {
		t.Errorf("queryIds = %v", d.QueryIDs)
	}
	// Second Detail call: cached.
	_, _ = preset.Detail(context.Background())
	if got := atomic.LoadInt32(&detailHits); got != 1 {
		t.Errorf("level-2 cache miss: hits=%d", got)
	}
}

func TestPresetDetailConcurrentCallersShareOneFetch(t *testing.T) {
	var detailHits int32
	gate := make(chan struct{})
	defer func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}()
	r := newReader(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/presets":
			_, _ = io.WriteString(w, `{"presets":[{"id":"id-1","name":"P1"}]}`)
		case strings.HasPrefix(r.URL.Path, "/api/presets/"):
			atomic.AddInt32(&detailHits, 1)
			<-gate
			_, _ = io.WriteString(w, `{"id":"id-1","name":"P1"}`)
		}
	})
	preset, err := r.ByName(context.Background(), "P1")
	if err != nil {
		t.Fatal(err)
	}
	const N = 8
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if _, err := preset.Detail(context.Background()); err != nil {
				t.Errorf("Detail: %v", err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	if got := atomic.LoadInt32(&detailHits); got != 1 {
		t.Errorf("expected 1 detail fetch, got %d", got)
	}
}

func TestPresetReaderLoadAllDetailsFanOut(t *testing.T) {
	var (
		listHits   int32
		detailHits int32
	)
	r := newReader(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/presets":
			atomic.AddInt32(&listHits, 1)
			items := make([]string, 0, 10)
			for i := 0; i < 10; i++ {
				items = append(items, fmt.Sprintf(`{"id":"%d","name":"P%d"}`, i, i))
			}
			_, _ = io.WriteString(w, `{"presets":[`+strings.Join(items, ",")+`]}`)
		case strings.HasPrefix(r.URL.Path, "/api/presets/"):
			atomic.AddInt32(&detailHits, 1)
			id := strings.TrimPrefix(r.URL.Path, "/api/presets/")
			fmt.Fprintf(w, `{"id":"%s","name":"P%s"}`, id, id)
		}
	})
	if err := r.LoadAllDetails(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&listHits); got != 1 {
		t.Errorf("list hits = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&detailHits); got != 10 {
		t.Errorf("detail hits = %d, want 10", got)
	}
}

func TestPresetReaderListPropagatesError(t *testing.T) {
	r := newReader(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"nope"}`)
	})
	_, err := r.List(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}
