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

	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/risks"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/retry"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/pagination"
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
