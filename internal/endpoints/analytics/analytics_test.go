package analytics_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/analytics"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/retry"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
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

func TestFetchSendsKPIInBody(t *testing.T) {
	var gotMethod, gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"distribution":[],"loc":0,"total":0}`)
	})
	var out models.AnalyticsDistributionStats
	err := analytics.Fetch(context.Background(), exec, base, "vulnerabilitiesBySeverityTotal", 0, nil, models.AnalyticsFilter{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.Contains(gotBody, `"kpi":"vulnerabilitiesBySeverityTotal"`) {
		t.Errorf("body missing kpi: %q", gotBody)
	}
}

func TestGetVulnerabilitiesBySeverityTotal(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"distribution":[{"label":"High","values":[{"label":"SAST","density":0.5,"percentage":50,"results":10}]}],"loc":1000,"total":20}`)
	})
	out, err := analytics.GetVulnerabilitiesBySeverityTotal(context.Background(), exec, base, models.AnalyticsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Total != 20 || out.LOC != 1000 {
		t.Errorf("total=%d loc=%d", out.Total, out.LOC)
	}
	if len(out.Distribution) != 1 || out.Distribution[0].Label != "High" {
		t.Errorf("distribution = %+v", out.Distribution)
	}
}

func TestGetMeanTimeToResolution(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"meanTimeData":[{"label":"High","results":5,"meanTime":3600}],"meanTimeStateData":[],"totalResults":5}`)
	})
	out, err := analytics.GetMeanTimeToResolution(context.Background(), exec, base, models.AnalyticsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalResults != 5 {
		t.Errorf("totalResults = %d", out.TotalResults)
	}
	if len(out.MeanTimeData) != 1 || out.MeanTimeData[0].MeanTime != 3600 {
		t.Errorf("meanTimeData = %+v", out.MeanTimeData)
	}
}

func TestGetAllVulnerabilities(t *testing.T) {
	var gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"allVulnerabilities":[{"vulnerabilityName":"SQL Injection","total":3,"severities":[]}],"page":1}`)
	})
	out, err := analytics.GetAllVulnerabilities(context.Background(), exec, base, 10, 0, models.AnalyticsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].VulnerabilityName != "SQL Injection" {
		t.Errorf("got %+v", out)
	}
	if !strings.Contains(gotBody, `"limit":10`) || !strings.Contains(gotBody, `"offset":0`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestGetIDETotal(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ideData":[{"label":"VS Code","scans":100,"developers":5}]}`)
	})
	out, err := analytics.GetIDETotal(context.Background(), exec, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Label != "VS Code" {
		t.Errorf("got %+v", out)
	}
}

func TestGetVulnerabilitiesByAgingTotal(t *testing.T) {
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"agingAndSeverities":[{"label":"30+ days","results":15,"severities":[{"label":"High","results":10}]}]}`)
	})
	out, err := analytics.GetVulnerabilitiesByAgingTotal(context.Background(), exec, base, models.AnalyticsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Label != "30+ days" {
		t.Errorf("got %+v", out)
	}
}

func TestFetchFilterPassthrough(t *testing.T) {
	var gotBody string
	exec, base := newExec(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{}`)
	})
	filter := models.AnalyticsFilter{
		Projects:   []string{"p1"},
		Severities: []string{"High"},
	}
	var out any
	_ = analytics.Fetch(context.Background(), exec, base, "test", 0, nil, filter, &out)
	if !strings.Contains(gotBody, `"projects":["p1"]`) {
		t.Errorf("missing projects in body: %q", gotBody)
	}
	if !strings.Contains(gotBody, `"severities":["High"]`) {
		t.Errorf("missing severities in body: %q", gotBody)
	}
}
