package scans_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

func TestAiConfig_JSON(t *testing.T) {
	b, err := json.Marshal(models.AiConfig{FusionEnabled: "true", LLMModel: "haiku-4.5"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"fusionEnabled":"true","llmModel":"haiku-4.5"}` {
		t.Errorf("got %s", b)
	}
	z, _ := json.Marshal(models.AiConfig{})
	if string(z) != `{}` {
		t.Errorf("zero value = %s", z)
	}
}

func aiEntry(t *testing.T, raw string) models.Config {
	t.Helper()
	var got struct {
		Config []models.Config `json:"config"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	for _, c := range got.Config {
		if c.Type == "ai" {
			return c
		}
	}
	t.Fatalf("no ai entry in %s", raw)
	return models.Config{}
}

func TestInvoker_AiEntryPassesThrough(t *testing.T) {
	mk, raw, _ := captureScan(t)
	_, err := mk().ForBranch("main").WithConfigs(models.AiConfig{FusionEnabled: "true", LLMModel: "opus-4.7"}.Config()).Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := aiEntry(t, *raw)
	if len(c.Value) != 2 || c.Value["fusionEnabled"] != "true" || c.Value["llmModel"] != "opus-4.7" {
		t.Errorf("ai entry = %+v", c)
	}
}

func TestInvoker_FusionEnabledWithoutModelIsConfigurationError(t *testing.T) {
	for _, m := range []string{"", "-"} {
		mk, _, hits := captureScan(t)
		cfg := models.AiConfig{FusionEnabled: "true"}.Config()
		if m == "-" {
			cfg = models.Config{Type: "ai", Value: map[string]any{"fusionEnabled": "true"}}
		}
		_, err := mk().ForBranch("main").WithConfigs(cfg).Start(context.Background())
		var ce *cxerrors.ConfigurationError
		if !errors.As(err, &ce) {
			t.Fatalf("want ConfigurationError, got %v", err)
		}
		if ce.Field != "Configs" || !strings.Contains(ce.Reason, "llmModel") {
			t.Errorf("field=%q reason=%q", ce.Field, ce.Reason)
		}
		wantConfigErr(t, err, hits)
	}
}

func TestInvoker_FusionDisabledNeedsNoModel(t *testing.T) {
	mk, _, hits := captureScan(t)
	_, err := mk().ForBranch("main").WithConfigs(models.AiConfig{FusionEnabled: "false"}.Config()).Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *hits != 1 {
		t.Errorf("hits = %d", *hits)
	}
}

func TestInvoker_WithFusion(t *testing.T) {
	mk, raw, _ := captureScan(t)
	if _, err := mk().ForBranch("main").WithFusion("haiku-4.5").Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := aiEntry(t, *raw)
	if len(c.Value) != 2 || c.Value["fusionEnabled"] != "true" || c.Value["llmModel"] != "haiku-4.5" {
		t.Errorf("ai entry = %+v", c)
	}

	mk2, _, hits := captureScan(t)
	_, err := mk2().ForBranch("main").WithFusion("").Start(context.Background())
	wantConfigErr(t, err, hits)
}
