# AGENTS.md — Copilot Instructions for Checkmarx One Go SDK Projects

This project uses the `cxone-sdk-golang` Go SDK for the Checkmarx One platform. Follow these rules when generating or modifying code.

## Client Construction

- Use the builder: `cxone.NewClient().Region(...).Tenant(...).AgentName(...).APIKey(...).Build()`
- Required: Region (typed constant like `cxone.RegionUS`), Tenant, AgentName, and one of APIKey or OAuth
- Always `defer client.Close()` after successful Build
- The client is goroutine-safe — create one, share it, close at shutdown
- Never use a client after Close

## Two Layers

- **Workflow layer** (default): `client.Scans()`, `client.Projects()`, `client.Presets()`, `client.Users()`, `client.Roles()`, `client.Clients()`, `client.Reports()`, `client.Audit()`, `client.Migration()`, `client.Groups()`
- **Advanced layer** (escape hatch): `client.Advanced().Projects()`, `client.Advanced().Scans()`, etc.
- Always prefer the workflow layer. Use Advanced only when workflow doesn't cover the endpoint.

## Context & Errors

- Every SDK call takes `context.Context` as first argument
- Use `context.WithTimeout` for per-call timeouts (default 60s)
- Use `errors.As` to check error types: `*cxerrors.ConfigurationError`, `*cxerrors.AuthError`, `*cxerrors.CommunicationError`, `*cxerrors.ResponseError`, `*cxerrors.ScanError`, `*cxerrors.ReportError`
- Never swallow errors — they carry correlation IDs

## Auth & Retry

- Auth is lazy (first request), 401 auto-refreshes
- SDK retries 3x with randomized backoff on 5xx/transient errors
- Do NOT add your own retry wrapper

## Common Patterns

```go
// Scan workflow
repo, _ := client.Projects().Get(ctx, projectID)
insp, _ := client.Scans().NewScan(repo).ForBranch("main").WithEngines("sast").Start(ctx)
final, _ := scans.WaitUntilComplete(ctx, insp, scans.WaitOptions{PollInterval: 10 * time.Second})

// Cached reader
presets := client.Presets()
all, _ := presets.List(ctx)

// GetOrCreate
user, _ := client.Users().GetOrCreateByEmail(ctx, &models.User{Email: "alice@example.com", Username: "alice", Enabled: true})

// Error handling
var commErr *cxerrors.CommunicationError
if errors.As(err, &commErr) {
    log.Printf("status=%d attempt=%d", commErr.StatusCode, commErr.Attempt)
}
```

## Do NOT

- Create a new client per request
- Add retry/backoff wrappers around SDK calls
- Parse raw HTTP responses — use typed models from `models/`
- Hardcode Checkmarx One hostnames — use Region constants
- Log credentials or bearer tokens
- Use `context.TODO()` in production code
- Ignore errors from `client.Close()`

## Models

All typed request/response structs live in `github.com/cx-andrew-gehring/cxone-sdk-golang/models`. Unknown fields are tolerated for forward compatibility.
