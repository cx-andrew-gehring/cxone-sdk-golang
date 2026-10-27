package advanced_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

const (
	creditsPage1 = `{"items":[{"name":"user-a","creditsUsed":"1.5","percentOfTotal":6.8,"actionsPerformed":{"triage":1,"haiku-4.5":2},"newField":true}],"totalItems":2,"totalPages":2,"currentPage":1}`
	creditsPage2 = `{"items":[{"name":"user-b","creditsUsed":"20","percentOfTotal":93.2,"actionsPerformed":{"remediation":6}}],"totalItems":2,"totalPages":2,"currentPage":2}`
)

// fastFail makes 5xx fail quickly: one attempt, no backoff.
func fastFail(b *cxone.ClientBuilder) { b.Retries(1).RetryDelaySeconds(0).RandomizeRetryDelay(false) }

func creditsServer(t *testing.T, page2Status int, hits *int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		atomic.AddInt32(hits, 1)
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, creditsPage1)
		case "2":
			if page2Status != 200 {
				w.WriteHeader(page2Status)
				_, _ = io.WriteString(w, `{"code":1,"message":"boom for user-a@example.com"}`)
				return
			}
			_, _ = io.WriteString(w, creditsPage2)
		default:
			t.Errorf("unexpected query %q", r.URL.RawQuery)
		}
	}
}

func TestCreditsConsumptionSinglePage(t *testing.T) {
	var hits int32
	c := newClientAgainst(t, creditsServer(t, 200, &hits))
	p, err := c.Advanced().Credits().Consumption(context.Background(), models.CreditConsumptionQuery{Page: 1, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	it := p.Items[0]
	if it.Name != "user-a" || it.CreditsUsed != "1.5" || it.ActionsPerformed["haiku-4.5"].String() != "2" || p.TotalPages != 2 || p.CurrentPage != 1 {
		t.Errorf("page = %+v", p)
	}
}

func TestCreditsConsumptionAllSumsPages(t *testing.T) {
	var hits int32
	c := newClientAgainst(t, creditsServer(t, 200, &hits))
	items, err := c.Advanced().Credits().ConsumptionAll(context.Background(), models.CreditConsumptionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].CreditsUsed != "1.5" || items[1].CreditsUsed != "20" || hits != 2 {
		t.Errorf("items=%+v hits=%d", items, hits)
	}
}

func TestCreditsConsumptionAllEmpty(t *testing.T) {
	c := newClientAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[],"totalItems":0,"totalPages":0,"currentPage":1}`)
	})
	items, err := c.Advanced().Credits().ConsumptionAll(context.Background(), models.CreditConsumptionQuery{})
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestCreditsConsumptionAllPage2FailsNoPartial(t *testing.T) {
	var hits int32
	c := newClientAgainstWith(t, creditsServer(t, 500, &hits), fastFail)
	items, err := c.Advanced().Credits().ConsumptionAll(context.Background(), models.CreditConsumptionQuery{})
	if err == nil || items != nil {
		t.Fatalf("want error and nil items, got %v, %v", items, err)
	}
	if strings.Contains(err.Error(), "example.com") {
		t.Errorf("error leaks response body: %v", err)
	}
}

func TestCreditsConsumptionAllWrongPageBase(t *testing.T) {
	c := newClientAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[{"name":"user-a","creditsUsed":"1"}],"totalItems":1,"totalPages":1,"currentPage":0}`)
	})
	items, err := c.Advanced().Credits().ConsumptionAll(context.Background(), models.CreditConsumptionQuery{})
	if err == nil || items != nil {
		t.Fatalf("want currentPage mismatch error, got %v, %v", items, err)
	}
}

func TestCreditsConsumptionCustomParams(t *testing.T) {
	var got string
	c := newClientAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"items":[],"totalItems":0,"totalPages":0,"currentPage":0}`)
	})
	_, err := c.Advanced().Credits().Consumption(context.Background(),
		models.CreditConsumptionQuery{Page: 0, Limit: 10, LimitParam: "size"})
	if err != nil || got != "size=10" {
		t.Fatalf("query=%q err=%v", got, err)
	}
}

func TestCreditsConsumptionStatusClassification(t *testing.T) {
	for _, code := range []int{403, 404, 500} {
		c := newClientAgainstWith(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"code":7,"message":"nope"}`)
		}, fastFail)
		_, err := c.Advanced().Credits().Consumption(context.Background(), models.CreditConsumptionQuery{})
		var re *cxerrors.ResponseError
		var ce *cxerrors.CommunicationError
		switch {
		case code >= 500:
			// 5xx exhausts the funnel's retries and is a CommunicationError.
			if !errors.As(err, &ce) {
				t.Errorf("status %d: err = %v", code, err)
			}
		case !errors.As(err, &re) || re.StatusCode != code:
			t.Errorf("status %d: err = %v", code, err)
		}
	}
}

func TestCreditsActionRatesRaw(t *testing.T) {
	var path string
	c := newClientAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = io.WriteString(w, `{"haiku-4.5":1.5,"triage":1}`)
	})
	raw, err := c.Advanced().Credits().ActionRates(context.Background())
	if err != nil || path != "/api/v1/credits/action-rates" || !strings.Contains(string(raw), `"triage":1`) {
		t.Fatalf("raw=%s path=%s err=%v", raw, path, err)
	}
}
