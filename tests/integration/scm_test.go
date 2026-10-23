//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	cxone "github.com/checkmarx-open-labs/cxone-sdk-golang"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/scm"
)

func TestListSCMIntegrations(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		integrations, err := c.Advanced().SCM().ListIntegrations(ctx)
		if err != nil {
			t.Fatalf("list SCM integrations: %v", err)
		}
		t.Logf("SCM integrations = %d", len(integrations))
		for _, integ := range integrations {
			t.Logf("  %s (type=%s repos=%d)", integ.DisplayName, integ.Type, integ.RepoCount)
		}
	})
}

// singleClient builds one client for tests that mutate tenant state, where
// running the same mutation twice (once per auth mode, as dualAuth does)
// would be wrong. OAuth is preferred; the API key is the fallback.
func singleClient(t *testing.T) *cxone.Client {
	t.Helper()

	tenant := envOrSkip(t, "TEST_TENANT")
	region := regionFromKey(envOrSkip(t, "TEST_REGION"))

	b := cxone.NewClient().
		Region(region).
		Tenant(tenant).
		AgentName("integration-test")

	switch {
	case os.Getenv("TEST_OAUTH_CLIENT_ID") != "" && os.Getenv("TEST_OAUTH_CLIENT_SECRET") != "":
		b = b.OAuth(os.Getenv("TEST_OAUTH_CLIENT_ID"), os.Getenv("TEST_OAUTH_CLIENT_SECRET"))
	case os.Getenv("TEST_API_KEY") != "":
		b = b.APIKey(os.Getenv("TEST_API_KEY"))
	default:
		t.Skip("skipping: neither TEST_OAUTH_CLIENT_ID nor TEST_API_KEY is set")
	}

	c, err := b.Build()
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestGetConversionStatusNotFound exercises the conversion status endpoint
// without mutating anything: a process id that cannot exist must produce a
// clean error rather than a decode failure or a bogus terminal state.
func TestGetConversionStatusNotFound(t *testing.T) {
	dualAuth(t, func(t *testing.T, c *cxone.Client) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, err := c.Advanced().SCM().GetConversionStatus(ctx, "definitely-not-a-real-process-id")
		if err == nil {
			t.Fatal("expected an error for an unknown process id")
		}
		t.Logf("unknown process id error (expected): %v", err)
	})
}

// TestReconnectProject performs a real disconnect-and-reconnect against a
// throwaway project. It is opt-in: without the full TEST_SCM_* environment it
// skips, so the default integration run never mutates a tenant.
//
// Required:
//
//	TEST_SCM_PROJECT_ID  the id of a disposable project to re-point
//	TEST_SCM_TYPE        github | gitlab | azure | bitbucket | githubApp
//	TEST_SCM_ORG         the target organization
//	TEST_SCM_REPO_URL    the target repo URL (no .git suffix)
//
// Optional: TEST_SCM_BRANCHES (comma-separated, default "main"),
// TEST_SCM_TOKEN.
//
// This test does NOT restore the project's original repository: reverting
// would require a second conversion against the old repo, which is itself the
// operation under test. Point it only at a project you are willing to leave
// re-pointed.
func TestReconnectProject(t *testing.T) {
	projectID := os.Getenv("TEST_SCM_PROJECT_ID")
	scmType := os.Getenv("TEST_SCM_TYPE")
	org := os.Getenv("TEST_SCM_ORG")
	repoURL := os.Getenv("TEST_SCM_REPO_URL")
	if projectID == "" || scmType == "" || org == "" || repoURL == "" {
		t.Skip("set TEST_SCM_PROJECT_ID, TEST_SCM_TYPE, TEST_SCM_ORG and TEST_SCM_REPO_URL to run")
	}

	branches := []string{"main"}
	if v := os.Getenv("TEST_SCM_BRANCHES"); v != "" {
		branches = strings.Split(v, ",")
	}

	// Deliberately not dualAuth: this mutates real state, so run it once.
	c := singleClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	before, err := c.Advanced().Projects().Get(ctx, projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	t.Logf("before: name=%s repoUrl=%q scmRepoId=%q", before.Name, before.RepoURL, before.ScmRepoID)

	insp, err := c.SCM().Reconnect(ctx, projectID, scm.ReconnectRequest{
		SCMType:           scmType,
		OrgIdentity:       org,
		Token:             os.Getenv("TEST_SCM_TOKEN"),
		RepoURL:           repoURL,
		ProtectedBranches: branches,
		SkipDisconnect:    before.ScmRepoID == "" && before.RepoURL == "",
	})
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	t.Logf("conversion processId=%s", insp.ID())

	final, err := scm.WaitUntilComplete(ctx, insp, scm.WaitOptions{
		PollInterval: 10 * time.Second,
		MaxDuration:  10 * time.Minute,
	})
	if data := final.Data(); data != nil {
		t.Logf("status=%q summary=%q converted=%d/%d failed=%v",
			data.Status, data.Summary, data.MigratedProjects, data.TotalProjects, data.FailedProjectList)
	}
	if err != nil {
		t.Fatalf("conversion did not succeed: %v", err)
	}

	after, err := c.Advanced().Projects().Get(ctx, projectID)
	if err != nil {
		t.Fatalf("re-read project: %v", err)
	}
	t.Logf("after: repoUrl=%q scmRepoId=%q", after.RepoURL, after.ScmRepoID)
	if after.RepoURL == before.RepoURL && before.RepoURL != "" {
		t.Errorf("repo URL unchanged after conversion: %q", after.RepoURL)
	}
}
