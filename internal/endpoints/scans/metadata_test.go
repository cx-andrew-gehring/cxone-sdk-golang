package scans_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scans"
)

func TestGetConfiguration(t *testing.T) {
	var gotQuery string
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"key":"scan.config.sast.presetName","value":"Default","allowOverride":true}]`)
	})
	cfg, err := scans.GetConfiguration(context.Background(), exec, base, "p1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg) != 1 || cfg[0].Key != "scan.config.sast.presetName" {
		t.Errorf("config = %+v", cfg)
	}
	if !strings.Contains(gotQuery, "project-id=p1") || !strings.Contains(gotQuery, "scan-id=s1") {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestGetSummary(t *testing.T) {
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/summary") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"scanId":"s1","status":"Completed","engine":"sast","totalCount":42}]`)
	})
	summaries, err := scans.GetSummary(context.Background(), exec, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].TotalCount != 42 {
		t.Errorf("summaries = %+v", summaries)
	}
}

func TestGetSASTAggregate(t *testing.T) {
	var gotQuery string
	exec, base := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"queryID":"123","queryName":"SQL Injection","severity":"High","count":5}]`)
	})
	agg, err := scans.GetSASTAggregate(context.Background(), exec, base, url.Values{"scan-id": {"s1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(agg) != 1 || agg[0].QueryName != "SQL Injection" {
		t.Errorf("aggregate = %+v", agg)
	}
	if !strings.Contains(gotQuery, "scan-id=s1") {
		t.Errorf("query = %q", gotQuery)
	}
}
