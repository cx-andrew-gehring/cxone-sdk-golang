package cxone_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func clientAgainst(t *testing.T, api http.HandlerFunc) *cxone.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/realms/acme/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"tok-1","token_type":"Bearer","expires_in":300}`)
	})
	mux.HandleFunc("/api/", api)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	region, err := cxone.NewCustomRegion(host, host)
	if err != nil {
		t.Fatal(err)
	}
	region, _ = region.WithScheme("http")
	c, err := cxone.NewClient().Region(region).Tenant("acme").AgentName("t").APIKey("k").HTTPClient(srv.Client()).Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestRisksGroupIDsPagesAndDedupes(t *testing.T) {
	const total = 450 // > 2 pages of 200; groupId = g<i/3> so each repeats 3x
	var engines []string
	calls := 0
	c := clientAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if q.Get("projectId") != "p1" {
			t.Errorf("projectId=%q", q.Get("projectId"))
		}
		engines = q["engine"]
		off, _ := strconv.Atoi(q.Get("offset"))
		lim, _ := strconv.Atoi(q.Get("limit"))
		if lim > 200 {
			t.Errorf("limit %d > 200", lim)
		}
		var parts []string
		for i := off; i < off+lim && i < total; i++ {
			parts = append(parts, fmt.Sprintf(`{"id":"r%d","groupId":"g%d"}`, i, i/3))
		}
		_, _ = io.WriteString(w, `{"risks":[`+strings.Join(parts, ",")+`]}`)
	})
	ids, err := c.Risks().GroupIDs(context.Background(), "p1", "SAST", "SCA")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 150 || ids[0] != "g0" || ids[149] != "g149" {
		t.Errorf("got %d ids", len(ids))
	}
	if !reflect.DeepEqual(engines, []string{"SAST", "SCA"}) {
		t.Errorf("engines %v", engines)
	}
	if calls < 3 {
		t.Errorf("expected paging, calls=%d", calls)
	}
}

func TestRisksGroupIDsEmpty(t *testing.T) {
	c := clientAgainst(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"risks":[]}`) })
	ids, err := c.Risks().GroupIDs(context.Background(), "p1")
	if err != nil || len(ids) != 0 {
		t.Errorf("got %v %v", ids, err)
	}
}

func TestRisksGroupIDsRequiresProject(t *testing.T) {
	c := clientAgainst(t, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := c.Risks().GroupIDs(context.Background(), ""); err == nil {
		t.Error("want error")
	}
}

func TestAITriageHandleSubmitAndResult(t *testing.T) {
	c := clientAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/ai-triage/triage":
			w.WriteHeader(202)
			_, _ = io.WriteString(w, `{"scanID":"s","status":"accepted","triageID":"t","published":false}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/ai-triage/triage/p/g":
			_, _ = io.WriteString(w, `{"projectID":"p","groupID":"g","jobStatus":"IN_PROGRESS"}`)
		default:
			w.WriteHeader(404)
		}
	})
	acc, err := c.AITriage().Submit(context.Background(), "s", models.TriageBucket{ScannerType: "sast", ResultIDs: []string{"a"}})
	if err != nil || acc.Published {
		t.Fatalf("%+v %v", acc, err)
	}
	res, err := c.AITriage().Result(context.Background(), "p", "g")
	if err != nil || res.Done() || res.Failed() || res.Detail() != nil {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestAITriageHandle402IsTyped(t *testing.T) {
	c := clientAgainst(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(402) })
	_, err := c.AITriage().Submit(context.Background(), "s", models.TriageBucket{ScannerType: "sast"})
	var te *cxerrors.TriageError
	if !errors.As(err, &te) || te.Kind != cxerrors.TriageInsufficientCredits {
		t.Errorf("got %v", err)
	}
}

func TestRisksGroupIDsOffsetIgnoringServerTerminates(t *testing.T) {
	calls := 0
	c := clientAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 50 {
			http.Error(w, "runaway", 500)
			return
		}
		_, _ = io.WriteString(w, `{"metaData":{"totalResults":999,"filteredResults":999},"risks":[{"id":"a","groupId":"g1"},{"id":"b","groupId":"g1"},{"id":"c","groupId":"g2"}]}`)
	})
	ids, err := c.Risks().GroupIDs(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"g1", "g2"}) || calls > 3 {
		t.Errorf("ids=%v calls=%d", ids, calls)
	}
}
