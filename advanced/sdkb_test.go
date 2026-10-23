package advanced_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func TestAdvancedAITriageAndListRisks(t *testing.T) {
	c := newClientAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ai-triage/triage":
			w.WriteHeader(202)
			_, _ = io.WriteString(w, `{"scanID":"s","status":"accepted","triageID":"t"}`)
		case "/api/risks":
			_, _ = io.WriteString(w, `{"risks":[{"id":"1","groupId":"g"}]}`)
		default:
			w.WriteHeader(404)
		}
	})
	acc, err := c.Advanced().AITriage().Submit(context.Background(), &models.TriageRequest{ScanID: "s"})
	if err != nil || acc.TriageID != "t" {
		t.Fatalf("%+v %v", acc, err)
	}
	page, err := c.Advanced().Risks().ListRisks(context.Background(), url.Values{"projectId": {"p"}})
	if err != nil || len(page.Risks) != 1 {
		t.Fatalf("%+v %v", page, err)
	}
}
