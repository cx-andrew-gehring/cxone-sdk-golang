package risks_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/risks"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/pagination"
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

func TestListRisks(t *testing.T) {
	var q url.Values
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/risks" {
			t.Errorf("path %q", r.URL.Path)
		}
		q = r.URL.Query()
		_, _ = io.WriteString(w, `{"metaData":{"totalResults":5,"filteredResults":2,"page":1,"pageSize":2},
"risks":[{"id":"i1","riskName":"SQLi","engine":"SAST","groupId":"g1","projectId":"p","unknown":1}]}`)
	})
	out, err := risks.ListRisks(context.Background(), exec, base, url.Values{"projectId": {"p"}, "engine": {"SAST", "SCA"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Risks) != 1 || out.Risks[0].GroupID != "g1" || out.MetaData.FilteredResults != 2 {
		t.Errorf("got %+v", out)
	}
	if q.Get("projectId") != "p" || len(q["engine"]) != 2 {
		t.Errorf("query %v", q)
	}
}

// pagedServer serves total risks, groupId = "g<i/2>" so every group appears twice.
func pagedServer(t *testing.T, total int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		body := `{"risks":[`
		first := true
		for i := off; i < off+lim && i < total; i++ {
			if !first {
				body += ","
			}
			first = false
			body += fmt.Sprintf(`{"id":"r%d","groupId":"g%d"}`, i, i/2)
		}
		body += `]}`
		_, _ = io.WriteString(w, body)
	}
}

func TestIterPagesThroughAll(t *testing.T) {
	exec, base := newExec(t, pagedServer(t, 7))
	it := risks.Iter(exec, base, url.Values{"projectId": {"p"}}, pagination.Config{PageSize: 3, PageRetryDelay: pagination.NoRetryDelay})
	all, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 7 {
		t.Errorf("got %d risks", len(all))
	}
}

func TestIterEmpty(t *testing.T) {
	exec, base := newExec(t, pagedServer(t, 0))
	it := risks.Iter(exec, base, url.Values{"projectId": {"p"}}, pagination.Config{PageRetryDelay: pagination.NoRetryDelay})
	all, err := pagination.Collect(context.Background(), it)
	if err != nil || len(all) != 0 {
		t.Errorf("got %v %v", all, err)
	}
}

// ignoringServer ignores offset and always returns the same first page.
func ignoringServer(total, page int, reqs *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*reqs++
		if *reqs > 50 {
			http.Error(w, "runaway", 500)
			return
		}
		body := fmt.Sprintf(`{"metaData":{"totalResults":%d,"filteredResults":%d},"risks":[`, total, total)
		for i := 0; i < page; i++ {
			if i > 0 {
				body += ","
			}
			body += fmt.Sprintf(`{"id":"r%d","groupId":"g%d"}`, i, i)
		}
		_, _ = io.WriteString(w, body+"]}")
	}
}

func TestIterOffsetIgnoringServerTerminates(t *testing.T) {
	reqs := 0
	// total says 1000 but the server keeps returning the same 200: stall guard must stop.
	exec, base := newExec(t, ignoringServer(1000, 200, &reqs))
	it := risks.Iter(exec, base, url.Values{"projectId": {"p"}}, pagination.Config{PageRetryDelay: pagination.NoRetryDelay})
	all, err := pagination.Collect(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 200 || reqs != 2 {
		t.Errorf("items=%d reqs=%d", len(all), reqs)
	}
}

func TestIterStopsAtFilteredTotalExactMultiple(t *testing.T) {
	reqs := 0
	h := pagedServer(t, 400)
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		reqs++
		rec := httptest.NewRecorder()
		h(rec, r)
		b := rec.Body.String()
		// inject metadata: filteredResults=400 (totalResults larger, must be ignored)
		b = `{"metaData":{"totalResults":9999,"filteredResults":400},` + b[1:]
		_, _ = io.WriteString(w, b)
	})
	it := risks.Iter(exec, base, url.Values{"projectId": {"p"}}, pagination.Config{PageRetryDelay: pagination.NoRetryDelay})
	all, err := pagination.Collect(context.Background(), it)
	if err != nil || len(all) != 400 {
		t.Fatalf("got %d %v", len(all), err)
	}
	if reqs != 2 {
		t.Errorf("want exactly 2 requests (no empty trailing page), got %d", reqs)
	}
}

func TestIterStopsOnShortPageWithoutMetadata(t *testing.T) {
	reqs := 0
	h := pagedServer(t, 250)
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) { reqs++; h(w, r) })
	it := risks.Iter(exec, base, url.Values{"projectId": {"p"}}, pagination.Config{PageRetryDelay: pagination.NoRetryDelay})
	all, _ := pagination.Collect(context.Background(), it)
	if len(all) != 250 || reqs != 2 {
		t.Errorf("items=%d reqs=%d", len(all), reqs)
	}
}

func TestIterDefaultPageSize200AndClamp(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{0, 200}, {500, 200}, {50, 50}} {
		var lim string
		exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
			lim = r.URL.Query().Get("limit")
			_, _ = io.WriteString(w, `{"risks":[]}`)
		})
		it := risks.Iter(exec, base, url.Values{"projectId": {"p"}}, pagination.Config{PageSize: tc.in, PageRetryDelay: pagination.NoRetryDelay})
		_, _ = pagination.Collect(context.Background(), it)
		if lim != strconv.Itoa(tc.want) {
			t.Errorf("PageSize %d: limit=%q want %d", tc.in, lim, tc.want)
		}
	}
}

func TestIterMaxPagesCap(t *testing.T) {
	// Every page has fresh ids and no metadata, full-size: only the cap stops it.
	reqs := 0
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		reqs++
		_, _ = io.WriteString(w, fmt.Sprintf(`{"risks":[{"id":"a%d"},{"id":"b%d"}]}`, reqs, reqs))
	})
	it := risks.Iter(exec, base, url.Values{"projectId": {"p"}}, pagination.Config{PageSize: 2, PageRetryDelay: pagination.NoRetryDelay})
	_, err := pagination.Collect(context.Background(), it)
	if err == nil {
		t.Fatal("want error at max pages")
	}
	if reqs != risks.MaxPages {
		t.Errorf("reqs=%d want %d", reqs, risks.MaxPages)
	}
}
