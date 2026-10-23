package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	cxone "github.com/cx-andrew-gehring/cxone-sdk-golang"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Info("Starting", "version", cxversion)

	client, err := cxone.NewClient().
		Region(regionFromEnv()).
		Tenant(envOrDie("CX_TENANT")).
		AgentName("my-tool").
		APIKey(envOrDie("CX_API_KEY")).
		Build()
	if err != nil {
		logger.Error("Failed to create client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	ctx := context.Background()

	// --- Low-level layer: list projects via the Advanced handle ---
	projects, err := client.Advanced().Projects().List(ctx, nil)
	if err != nil {
		logger.Error("Failed to list projects", "error", err)
		os.Exit(1)
	}
	fmt.Printf("Connected! Found %d projects\n", projects.TotalCount)
	for _, p := range projects.Projects {
		fmt.Printf("  %s  %s\n", p.ID, p.Name)
	}

	// --- Workflow layer: cached preset catalog ---
	presets := client.Presets()
	all, err := presets.List(ctx)
	if err != nil {
		logger.Error("Failed to list presets", "error", err)
		os.Exit(1)
	}
	fmt.Printf("\nPreset catalog: %d presets\n", len(all))
	for _, p := range all {
		fmt.Printf("  %s\n", p.Name())
	}
}

func regionFromEnv() cxone.Region {
	switch os.Getenv("CX_REGION") {
	case "US":
		return cxone.RegionUS
	case "US2":
		return cxone.RegionUS2
	case "EU":
		return cxone.RegionEU
	case "EU2":
		return cxone.RegionEU2
	case "DEU":
		return cxone.RegionDEU
	case "ANZ":
		return cxone.RegionANZ
	case "India":
		return cxone.RegionIndia
	case "Singapore":
		return cxone.RegionSingapore
	case "UAE":
		return cxone.RegionUAE
	default:
		return cxone.RegionUS
	}
}

func envOrDie(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "ERROR: environment variable %s is required\n", key)
		os.Exit(1)
	}
	return v
}
