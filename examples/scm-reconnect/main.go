// scm-reconnect re-points an existing Checkmarx One project at a different
// code repository — possibly on a different SCM provider — without recreating
// the project, so its scan history is preserved.
//
// It disconnects the project from its current code repository integration and
// then runs a conversion against the target repo, polling until the
// conversion reaches a terminal state.
//
// SAFETY: the disconnect and the conversion are two separate API calls and are
// NOT atomic. If the conversion fails after the disconnect succeeds, the
// project is left as a manual project. This program therefore runs as a DRY
// RUN by default and only mutates when CX_CONFIRM=yes. It is also safe to
// re-run: an already-manual project skips the disconnect step.
//
// Usage:
//
//	export CX_TENANT=your-tenant CX_API_KEY=your-key CX_REGION=US
//	export CX_PROJECT="my-project"           # name or project id
//	export CX_TARGET_SCM=github
//	export CX_TARGET_ORG=my-org
//	export CX_TARGET_REPO_URL=https://github.com/my-org/new-repo
//	export CX_TARGET_BRANCHES=main
//	export CX_TARGET_SCM_TOKEN=ghp_...       # optional, see below
//	go run ./examples/scm-reconnect          # dry run — mutates nothing
//	CX_CONFIRM=yes go run ./examples/scm-reconnect
//
// Optional variables: CX_TARGET_SCM_URL (self-hosted SCM URL), CX_SCAN_BRANCH,
// CX_ENGINES (comma-separated: sast,sca,kics,apisec), CX_WEBHOOK,
// CX_DECORATE_PRS, CX_SCA_AUTO_PR, CX_AUTO_SCAN.
//
// CX_TARGET_SCM_TOKEN may be omitted when a Code Repository project already
// exists for the target organization (the token is reused), and is not used at
// all for githubApp integrations.
package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	cxone "github.com/checkmarx-open-labs/cxone-sdk-golang"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/scm"
)

func main() {
	client, err := cxone.NewClient().
		Region(regionFromEnv()).
		Tenant(mustEnv("CX_TENANT")).
		AgentName("scm-reconnect-example").
		APIKey(mustEnv("CX_API_KEY")).
		Build()
	if err != nil {
		log.Fatalf("build client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	targetSCM := mustEnv("CX_TARGET_SCM")
	targetOrg := mustEnv("CX_TARGET_ORG")
	repoURL := normalizeRepoURL(mustEnv("CX_TARGET_REPO_URL"))
	branches := splitCSV(mustEnv("CX_TARGET_BRANCHES"))
	scanBranch := os.Getenv("CX_SCAN_BRANCH")
	autoScan := boolEnv("CX_AUTO_SCAN")

	// Validate the auto-scan pairing before anything destructive happens.
	if autoScan && scanBranch == "" {
		log.Fatal("CX_AUTO_SCAN=true requires CX_SCAN_BRANCH")
	}

	// --- Resolve the project (accepts a name or an id) ---

	project, err := resolveProject(ctx, client, mustEnv("CX_PROJECT"))
	if err != nil {
		log.Fatalf("resolve project: %v", err)
	}

	connected := project.ScmRepoID != "" || project.RepoURL != ""

	fmt.Println("Current state")
	fmt.Printf("  project     : %s\n", project.Name)
	fmt.Printf("  id          : %s\n", project.ID)
	fmt.Printf("  repo url    : %s\n", orNone(project.RepoURL))
	fmt.Printf("  main branch : %s\n", orNone(project.MainBranch))
	fmt.Printf("  scm repo id : %s\n", orNone(project.ScmRepoID))
	fmt.Printf("  connected   : %t\n", connected)

	fmt.Println("\nTarget state")
	fmt.Printf("  scm type    : %s\n", targetSCM)
	fmt.Printf("  organization: %s\n", targetOrg)
	fmt.Printf("  repo url    : %s\n", repoURL)
	fmt.Printf("  branches    : %s\n", strings.Join(branches, ", "))
	fmt.Printf("  engines     : %s\n", orNone(strings.Join(splitCSV(os.Getenv("CX_ENGINES")), ", ")))
	fmt.Printf("  auto scan   : %t\n", autoScan)
	if autoScan {
		fmt.Printf("  scan branch : %s\n", scanBranch)
	}

	// --- Pre-flight: fail on a bad target BEFORE disconnecting ---

	if err := verifyIntegration(ctx, client, targetSCM); err != nil {
		log.Fatalf("pre-flight: %v", err)
	}
	fmt.Printf("\nPre-flight OK: a %q integration is configured for this tenant.\n", targetSCM)

	req := scm.ReconnectRequest{
		SCMType:                  targetSCM,
		SCMOnPremURL:             os.Getenv("CX_TARGET_SCM_URL"),
		OrgIdentity:              targetOrg,
		Token:                    os.Getenv("CX_TARGET_SCM_TOKEN"),
		RepoURL:                  repoURL,
		ProtectedBranches:        branches,
		BranchToScanUponCreation: scanBranch,
		Types:                    splitCSV(os.Getenv("CX_ENGINES")),
		WebhookEnabled:           boolEnv("CX_WEBHOOK"),
		AutoScan:                 autoScan,
		SCAAutoPREnabled:         boolEnv("CX_SCA_AUTO_PR"),
		DecoratePullRequests:     boolEnv("CX_DECORATE_PRS"),
		// Already-manual projects have nothing to disconnect. This is what
		// makes the program safe to re-run after a partial failure.
		SkipDisconnect: !connected,
	}
	if err := req.Validate(); err != nil {
		log.Fatalf("invalid request: %v", err)
	}

	if os.Getenv("CX_CONFIRM") != "yes" {
		fmt.Println("\nDRY RUN — nothing was changed.")
		fmt.Println("Re-run with CX_CONFIRM=yes to apply.")
		return
	}

	// --- Mutate ---

	if req.SkipDisconnect {
		fmt.Println("\nProject is already manual — skipping disconnect.")
	} else {
		fmt.Println("\nDisconnecting from the current repository...")
	}

	insp, err := client.SCM().Reconnect(ctx, project.ID, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nReconnect failed.")
		if !req.SkipDisconnect {
			fmt.Fprintf(os.Stderr,
				"The project may have been disconnected before the failure. Re-running this\n"+
					"program is safe and will resume from the current state:\n"+
					"  CX_CONFIRM=yes go run ./examples/scm-reconnect\n")
		}
		fmt.Fprintf(os.Stderr,
			"Note: only one conversion may run per organization at a time. If a conversion\n"+
				"for %s/%s is already in flight, wait for it rather than retrying.\n",
			targetSCM, targetOrg)
		log.Fatalf("reconnect: %v", err)
	}

	fmt.Printf("Conversion started: processId=%s\n", insp.ID())
	fmt.Println("Polling...")

	final, waitErr := scm.WaitUntilComplete(ctx, insp, scm.WaitOptions{
		PollInterval: 10 * time.Second,
		MaxDuration:  10 * time.Minute,
	})

	if data := final.Data(); data != nil {
		fmt.Printf("\nStatus   : %s\n", orNone(data.Status))
		fmt.Printf("Summary  : %s\n", orNone(data.Summary))
		fmt.Printf("Converted: %d/%d\n", data.MigratedProjects, data.TotalProjects)
		if len(data.SuccessfulProjectsList) > 0 {
			fmt.Printf("Succeeded: %s\n", strings.Join(data.SuccessfulProjectsList, ", "))
		}
		if len(data.FailedProjectList) > 0 {
			fmt.Printf("Failed   : %s\n", strings.Join(data.FailedProjectList, ", "))
		}
	}
	if waitErr != nil {
		log.Fatalf("conversion did not succeed: %v", waitErr)
	}

	// --- Confirm by re-reading the project ---

	updated, err := client.Advanced().Projects().Get(ctx, project.ID)
	if err != nil {
		log.Fatalf("re-read project: %v", err)
	}
	fmt.Printf("\nProject %s now points at %s\n", updated.Name, orNone(updated.RepoURL))
}

// resolveProject accepts either a project id or a project name. An id is
// tried first; anything that is not found by id is looked up by exact name.
func resolveProject(ctx context.Context, client *cxone.Client, ref string) (*models.ProjectResponseModel, error) {
	if p, err := client.Advanced().Projects().Get(ctx, ref); err == nil {
		return p, nil
	}

	coll, err := client.Advanced().Projects().List(ctx, url.Values{"names": {ref}})
	if err != nil {
		return nil, fmt.Errorf("look up %q by name: %w", ref, err)
	}

	// The names filter is not guaranteed to be an exact match, so narrow it
	// here rather than trusting the server-side filter.
	var matches []models.ProjectResponseModel
	for _, p := range coll.Projects {
		if p.Name == ref {
			matches = append(matches, p)
		}
	}

	switch len(matches) {
	case 1:
		return &matches[0], nil
	case 0:
		return nil, fmt.Errorf("no project found with id or name %q", ref)
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.ID
		}
		return nil, fmt.Errorf("%d projects are named %q (ids: %s) — set CX_PROJECT to one of these ids",
			len(matches), ref, strings.Join(ids, ", "))
	}
}

// verifyIntegration confirms the tenant has an integration of the requested
// type. This runs before the disconnect so that a typo in CX_TARGET_SCM fails
// while the project is still connected.
func verifyIntegration(ctx context.Context, client *cxone.Client, scmType string) error {
	integrations, err := client.Advanced().SCM().ListIntegrations(ctx)
	if err != nil {
		return fmt.Errorf("list SCM integrations: %w", err)
	}
	available := make([]string, 0, len(integrations))
	for _, in := range integrations {
		if strings.EqualFold(in.Type, scmType) {
			return nil
		}
		available = append(available, in.Type)
	}
	return fmt.Errorf("no %q integration configured for this tenant (found: %s)",
		scmType, orNone(strings.Join(available, ", ")))
}

// normalizeRepoURL strips a trailing ".git". The conversion API matches the
// repo URL literally and would otherwise fail to find the repository.
func normalizeRepoURL(raw string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	if trimmed != raw {
		fmt.Fprintf(os.Stderr, "note: trimmed repo URL to %q (the API rejects a .git suffix)\n", trimmed)
	}
	return trimmed
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func boolEnv(key string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "true")
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
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

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("environment variable %s is required", key)
	}
	return v
}
