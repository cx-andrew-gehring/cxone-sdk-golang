package cxone

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/advanced"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/auth"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/cxerrors"
	riskseps "github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/risks"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/retry"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/observability"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/pagination"
	wfaitriage "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/aitriage"
	wfaudit "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/audit"
	wfclients "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/clients"
	wfgroups "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/groups"
	wfmigration "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/migration"
	wfpresets "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/presets"
	wfprojects "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/projects"
	wfreports "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/reports"
	wfresults "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/results"
	wfroles "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/roles"
	wfscans "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/scans"
	wfscm "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/scm"
	wfusers "github.com/checkmarx-open-labs/cxone-sdk-golang/workflows/users"
)

// Authenticator is the SDK's auth contract. It is an alias of
// [auth.Authenticator] so callers can satisfy the type using either name.
type Authenticator = auth.Authenticator

// Interceptor is the user-extensible request/response hook. See [transport.Interceptor].
type Interceptor = transport.Interceptor

// InterceptorFunc adapts plain functions to [Interceptor].
type InterceptorFunc = transport.InterceptorFunc

// Default values for the client builder. CLAUDE.md §4.1.
const (
	defaultTimeout       = 60 * time.Second
	defaultRetries       = 3
	defaultRetryDelaySec = 15
)

// Client is the entry point for all interaction with Checkmarx One.
//
// Construct one via [NewClient] (which returns a [ClientBuilder]). The
// resulting Client is safe for concurrent use and should be reused for the
// lifetime of your application — it owns a pooled HTTP client, a token
// cache, and a per-instance correlation id.
//
// Always call [Client.Close] at shutdown to release pooled connections.
type Client struct {
	region        Region
	tenant        string
	agentName     string
	correlationID string

	httpClient  *http.Client
	auth        auth.Authenticator
	executor    *transport.Executor
	iamAdminURL string

	logger  observability.Logger
	metrics observability.Metrics
	tracer  observability.Tracer

	closeOnce sync.Once
}

// Region returns the region the client was built against.
func (c *Client) Region() Region { return c.region }

// Tenant returns the tenant the client was built against.
func (c *Client) Tenant() string { return c.tenant }

// CorrelationID returns the per-client UUID sent on every request as the
// CorrelationId header (CLAUDE.md §4.2). Useful for surfacing in logs that
// the user may want to correlate with Checkmarx-side request tracing.
func (c *Client) CorrelationID() string { return c.correlationID }

// Close releases the underlying HTTP client's idle connections and forgets
// any cached auth tokens. Close is idempotent and safe to call from any
// goroutine; in-flight requests are NOT cancelled, but the client cannot be
// reused after Close returns.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		if c.httpClient != nil {
			c.httpClient.CloseIdleConnections()
		}
		if closer, ok := c.auth.(interface{ Close() }); ok {
			closer.Close()
		}
	})
	return nil
}

// ClientBuilder accumulates the inputs to construct a [Client]. Build it via
// [NewClient], chain configuration setters, then call [ClientBuilder.Build].
//
// All setters return the builder so they can be chained. Validation happens
// in Build — setters never panic.
type ClientBuilder struct {
	region    Region
	regionSet bool
	tenant    string
	agentName string

	authMode         authMode
	oauthCreds       auth.OAuthCredentials
	apiKey           string
	customAuth       auth.Authenticator

	timeout       time.Duration
	retries       int
	retryDelaySec int
	randomizeRetry bool

	proxyURL  string
	verifySSL bool

	httpClient   *http.Client
	logger       observability.Logger
	metrics      observability.Metrics
	tracer       observability.Tracer
	interceptors []Interceptor
}

type authMode int

const (
	authModeUnset authMode = iota
	authModeOAuth
	authModeAPIKey
	authModeCustom
)

// NewClient returns a fresh [ClientBuilder] populated with the documented
// default values from CLAUDE.md §4.1: timeout=60s, retries=3,
// retryDelaySeconds=15, randomized backoff, TLS verification on, no proxy.
func NewClient() *ClientBuilder {
	return &ClientBuilder{
		timeout:        defaultTimeout,
		retries:        defaultRetries,
		retryDelaySec:  defaultRetryDelaySec,
		randomizeRetry: true,
		verifySSL:      true,
	}
}

// Region selects the Checkmarx One region. Required.
func (b *ClientBuilder) Region(r Region) *ClientBuilder {
	b.region = r
	b.regionSet = true
	return b
}

// Tenant selects the Checkmarx One tenant (the IAM realm). Required.
func (b *ClientBuilder) Tenant(name string) *ClientBuilder {
	b.tenant = name
	return b
}

// AgentName sets the agent name embedded in the User-Agent header. Required.
// CLAUDE.md §4.1.
func (b *ClientBuilder) AgentName(name string) *ClientBuilder {
	b.agentName = name
	return b
}

// OAuth configures the OAuth client_credentials authentication flow.
// Mutually exclusive with [ClientBuilder.APIKey] and [ClientBuilder.Authenticator].
func (b *ClientBuilder) OAuth(clientID, clientSecret string) *ClientBuilder {
	b.authMode = authModeOAuth
	b.oauthCreds = auth.OAuthCredentials{ClientID: clientID, ClientSecret: clientSecret}
	return b
}

// APIKey configures the API-key (refresh-token) authentication flow.
// Mutually exclusive with [ClientBuilder.OAuth] and [ClientBuilder.Authenticator].
func (b *ClientBuilder) APIKey(apiKey string) *ClientBuilder {
	b.authMode = authModeAPIKey
	b.apiKey = apiKey
	return b
}

// Authenticator installs a custom [Authenticator] implementation, e.g. one
// that fetches tokens from a vault or sidecar. Mutually exclusive with
// [ClientBuilder.OAuth] and [ClientBuilder.APIKey].
func (b *ClientBuilder) Authenticator(a Authenticator) *ClientBuilder {
	b.authMode = authModeCustom
	b.customAuth = a
	return b
}

// Timeout sets the per-request HTTP timeout. Default 60s.
func (b *ClientBuilder) Timeout(d time.Duration) *ClientBuilder {
	b.timeout = d
	return b
}

// Retries sets the total number of attempts per request (including the first).
// Default 3.
func (b *ClientBuilder) Retries(n int) *ClientBuilder {
	b.retries = n
	return b
}

// RetryDelaySeconds sets the upper bound on the inter-attempt backoff in
// seconds. Default 15.
func (b *ClientBuilder) RetryDelaySeconds(seconds int) *ClientBuilder {
	b.retryDelaySec = seconds
	return b
}

// RandomizeRetryDelay controls whether the inter-attempt backoff is jittered.
// Default true.
func (b *ClientBuilder) RandomizeRetryDelay(on bool) *ClientBuilder {
	b.randomizeRetry = on
	return b
}

// Proxy sets an HTTP/HTTPS proxy URL. Empty string disables the proxy.
func (b *ClientBuilder) Proxy(rawURL string) *ClientBuilder {
	b.proxyURL = rawURL
	return b
}

// VerifySSL toggles TLS certificate verification. Default true. Disabling
// verification is intended for self-signed test environments only.
func (b *ClientBuilder) VerifySSL(on bool) *ClientBuilder {
	b.verifySSL = on
	return b
}

// HTTPClient lets callers inject a pre-configured *http.Client (e.g. for
// shared connection pools, custom round-trippers, or testing). When set, the
// builder ignores Proxy/VerifySSL/Timeout for transport configuration; the
// supplied client is used as-is. Per-request timeouts are still applied via
// context.WithTimeout.
func (b *ClientBuilder) HTTPClient(c *http.Client) *ClientBuilder {
	b.httpClient = c
	return b
}

// Logger installs a structured logger. Default is [observability.NopLogger].
func (b *ClientBuilder) Logger(l observability.Logger) *ClientBuilder {
	b.logger = l
	return b
}

// Metrics installs a metrics sink. Default is [observability.NopMetrics].
func (b *ClientBuilder) Metrics(m observability.Metrics) *ClientBuilder {
	b.metrics = m
	return b
}

// Tracer installs a tracer. Default is [observability.NopTracer].
func (b *ClientBuilder) Tracer(t observability.Tracer) *ClientBuilder {
	b.tracer = t
	return b
}

// Interceptors registers request/response interceptors. Interceptors run in
// registration order on the request side and reverse order on the response
// side. Multiple calls accumulate.
func (b *ClientBuilder) Interceptors(ic ...Interceptor) *ClientBuilder {
	b.interceptors = append(b.interceptors, ic...)
	return b
}

// Build validates the inputs and returns a ready-to-use [Client]. Returns a
// [*ConfigurationError] (or [*EndpointError] for region problems) if any
// required field is missing or invalid.
func (b *ClientBuilder) Build() (*Client, error) {
	if !b.regionSet {
		return nil, &cxerrors.ConfigurationError{Field: "Region", Reason: "is required"}
	}
	if err := b.region.Validate(); err != nil {
		return nil, err
	}
	if b.tenant == "" {
		return nil, &cxerrors.ConfigurationError{Field: "Tenant", Reason: "is required"}
	}
	if b.agentName == "" {
		return nil, &cxerrors.ConfigurationError{Field: "AgentName", Reason: "is required"}
	}
	if b.authMode == authModeUnset {
		return nil, &cxerrors.ConfigurationError{
			Field:  "Auth",
			Reason: "one of OAuth, APIKey, or Authenticator must be configured",
		}
	}

	httpClient, err := b.buildHTTPClient()
	if err != nil {
		return nil, err
	}

	tokenURL, err := b.region.TokenURL(b.tenant)
	if err != nil {
		return nil, err
	}
	iamAdminURL, err := b.region.AuthAdminURL(b.tenant)
	if err != nil {
		return nil, err
	}

	correlationID := uuid.NewString()
	userAgent := b.agentName + "/(CxOne " + productName + "/" + Version + ")"

	authPolicy := b.retryPolicy()
	authCfg := auth.Config{
		HTTPClient:  httpClient,
		TokenURL:    tokenURL,
		UserAgent:   userAgent,
		RetryPolicy: authPolicy,
	}

	var authImpl auth.Authenticator
	switch b.authMode {
	case authModeOAuth:
		authImpl, err = auth.NewOAuthClientCredentials(authCfg, b.oauthCreds)
	case authModeAPIKey:
		authImpl, err = auth.NewAPIKey(authCfg, b.apiKey)
	case authModeCustom:
		if b.customAuth == nil {
			return nil, &cxerrors.ConfigurationError{Field: "Authenticator", Reason: "is nil"}
		}
		authImpl = b.customAuth
	}
	if err != nil {
		return nil, err
	}

	logger := b.logger
	if logger == nil {
		logger = observability.NopLogger{}
	}
	metrics := b.metrics
	if metrics == nil {
		metrics = observability.NopMetrics{}
	}
	tracer := b.tracer
	if tracer == nil {
		tracer = observability.NopTracer{}
	}

	executor := transport.NewExecutor(transport.Config{
		HTTPClient:    httpClient,
		Authenticator: authImpl,
		UserAgent:     userAgent,
		CorrelationID: correlationID,
		Timeout:       b.timeout,
		RetryPolicy:   b.retryPolicy(),
		Interceptors:  b.interceptors,
		Logger:        logger,
		Metrics:       metrics,
		Tracer:        tracer,
	})

	return &Client{
		region:        b.region,
		tenant:        b.tenant,
		agentName:     b.agentName,
		correlationID: correlationID,
		httpClient:    httpClient,
		auth:          authImpl,
		executor:      executor,
		iamAdminURL:   iamAdminURL,
		logger:        logger,
		metrics:       metrics,
		tracer:        tracer,
	}, nil
}

func (b *ClientBuilder) retryPolicy() retry.Policy {
	return retry.Policy{
		MaxAttempts: b.retries,
		MaxDelay:    time.Duration(b.retryDelaySec) * time.Second,
		Randomize:   b.randomizeRetry,
	}
}

func (b *ClientBuilder) buildHTTPClient() (*http.Client, error) {
	if b.httpClient != nil {
		return b.httpClient, nil
	}
	transportRT := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if !b.verifySSL {
		transportRT.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user opt-in
	}
	if b.proxyURL != "" {
		u, err := url.Parse(b.proxyURL)
		if err != nil {
			return nil, &cxerrors.ConfigurationError{Field: "Proxy", Reason: "invalid URL: " + err.Error()}
		}
		transportRT.Proxy = http.ProxyURL(u)
	}
	return &http.Client{
		Transport: transportRT,
		Timeout:   b.timeout,
	}, nil
}

// rootURL returns the host root the endpoint layer joins paths against
// (e.g. "https://ast.checkmarx.net/"). The endpoint packages prepend
// "api/" or "auth/" themselves; this matches the ast-cli convention and
// keeps endpoints whose paths live outside /api/ — Groups is the main
// example — addressable through the same handle.
func (c *Client) rootURL() string {
	return c.region.DisplayRootURL()
}

// Projects returns the high-level projects workflow handle. Used to obtain
// a [wfprojects.ProjectRepoConfig] for one or more projects:
//
//	repo, err := client.Projects().Get(ctx, "project-uuid")
//
// Projects is the front door for project-centric workflows. For raw access
// to the underlying REST endpoints (CRUD, configuration, branches), use
// [Client.Advanced].Projects().
func (c *Client) Projects() *ProjectsHandle {
	return &ProjectsHandle{
		backend: &wfprojects.Backend{Executor: c.executor, BaseURL: c.rootURL()},
	}
}

// Scans returns the high-level scans workflow handle. Used to construct
// scan inspectors and start new scans via the fluent invoker:
//
//	insp, err := client.Scans().Get(ctx, scanID)
//	insp, err := client.Scans().NewScan(repoConfig).ForBranch("main").WithEngines("sast").Start(ctx)
func (c *Client) Scans() *ScansHandle {
	return &ScansHandle{
		client:  c,
		backend: &wfscans.Backend{Executor: c.executor, BaseURL: c.rootURL()},
	}
}

// Presets returns the high-level preset/query catalog reader. The returned
// reader has its own multi-level lazy cache; reuse one instance for the
// lifetime of your application rather than constructing many.
func (c *Client) Presets() *wfpresets.PresetReader {
	return wfpresets.NewReader(&wfpresets.Backend{Executor: c.executor, BaseURL: c.rootURL()})
}

// Reports returns the high-level reports workflow handle. Used to start
// JSON or PDF report generation:
//
//	h, err := client.Reports().NewJSONReport().ForScan(s, p, "main").Generate(ctx)
//	err = h.WaitAndDownload(ctx, dest, reports.WaitOptions{})
func (c *Client) Reports() *ReportsHandle {
	return &ReportsHandle{
		backend: &wfreports.Backend{Executor: c.executor, BaseURL: c.rootURL()},
	}
}

// ProjectsHandle wraps the projects workflow with method-style ergonomics.
//
// Constructed by [Client.Projects]; do not instantiate directly.
type ProjectsHandle struct {
	backend *wfprojects.Backend
}

// Get loads a project by id and returns a [wfprojects.ProjectRepoConfig].
// Equivalent to [wfprojects.FromProjectID] but bound to the client.
func (h *ProjectsHandle) Get(ctx context.Context, projectID string) (*wfprojects.ProjectRepoConfig, error) {
	return wfprojects.FromProjectID(ctx, h.backend, projectID)
}

// Wrap turns an already-fetched project model into a [ProjectRepoConfig]
// without issuing a network call. Useful when iterating over projects from
// the low-level layer (e.g. via [Client.Advanced].Projects().Iter).
func (h *ProjectsHandle) Wrap(project *models.ProjectResponseModel) (*wfprojects.ProjectRepoConfig, error) {
	return wfprojects.FromProjectModel(h.backend, project)
}

// GetOrCreateByName returns the existing project or creates one from the
// template. See [wfprojects.GetOrCreateByName].
func (h *ProjectsHandle) GetOrCreateByName(ctx context.Context, template *models.Project) (*wfprojects.ProjectRepoConfig, error) {
	return wfprojects.GetOrCreateByName(ctx, h.backend, template)
}

// GetOrCreateInApplicationByName finds or creates a project and ensures it
// is associated with the named application. See
// [wfprojects.GetOrCreateInApplicationByName].
func (h *ProjectsHandle) GetOrCreateInApplicationByName(ctx context.Context, projectName, applicationName string) (*wfprojects.ProjectRepoConfig, *models.Application, error) {
	return wfprojects.GetOrCreateInApplicationByName(ctx, h.backend, projectName, applicationName)
}

// ScansHandle wraps the scans workflow with method-style ergonomics.
type ScansHandle struct {
	client  *Client
	backend *wfscans.Backend
}

// Get loads a scan by id and returns a populated [wfscans.ScanInspector].
func (h *ScansHandle) Get(ctx context.Context, scanID string) (*wfscans.ScanInspector, error) {
	return wfscans.FromScanID(ctx, h.backend, scanID)
}

// NewScan returns a fluent builder for starting a new scan against the
// supplied project. The builder is bound to the client's HTTP client so
// upload-mode scans can use the shared connection pool.
func (h *ScansHandle) NewScan(repo *wfprojects.ProjectRepoConfig) *wfscans.Invoker {
	return wfscans.NewInvoker(h.backend, repo, h.client.httpClient)
}

// Compare diffs the results of two scans into new/resolved/recurrent counts
// by severity and extracts the not-exploitable list from the new scan. See
// [wfscans.Backend.Compare] for option details.
func (h *ScansHandle) Compare(ctx context.Context, oldScanID, newScanID string, opts ...wfscans.CompareOption) (*models.ScanComparison, error) {
	return h.backend.Compare(ctx, oldScanID, newScanID, opts...)
}

// Results returns the high-level results workflow handle. Use it to walk
// scan results by scan id, with the cross-engine /api/results endpoint
// surfaced as typed helpers.
//
//	rs, err := client.Results().ListByState(ctx, scanID, "NOT_EXPLOITABLE")
func (c *Client) Results() *ResultsHandle {
	return &ResultsHandle{
		reader: wfresults.NewReader(&wfresults.Backend{Executor: c.executor, BaseURL: c.rootURL()}),
	}
}

// ResultsHandle wraps the results workflow with method-style ergonomics.
type ResultsHandle struct {
	reader *wfresults.Reader
}

// ListAll iterates every result page for scanID and returns the flattened
// slice. See [wfresults.Reader.ListAll].
func (h *ResultsHandle) ListAll(ctx context.Context, scanID string, query url.Values) ([]*models.ScanResult, error) {
	return h.reader.ListAll(ctx, scanID, query)
}

// ListByState is shorthand for [ResultsHandle.ListAll] with a "state" filter.
func (h *ResultsHandle) ListByState(ctx context.Context, scanID, state string) ([]*models.ScanResult, error) {
	return h.reader.ListByState(ctx, scanID, state)
}

// Iter returns an item-yielding iterator over the results matching scanID
// plus the optional filters in query.
func (h *ResultsHandle) Iter(scanID string, query url.Values) *pagination.Iterator[*models.ScanResult] {
	return h.reader.Iter(scanID, query)
}

// ReportsHandle wraps the reports workflow with method-style ergonomics.
type ReportsHandle struct {
	backend *wfreports.Backend
}

// NewJSONReport returns a fluent builder for a JSON report.
func (h *ReportsHandle) NewJSONReport() *wfreports.Builder {
	return wfreports.NewJSONReport(h.backend)
}

// NewPDFReport returns a fluent builder for a PDF report.
func (h *ReportsHandle) NewPDFReport() *wfreports.Builder {
	return wfreports.NewPDFReport(h.backend)
}

// Groups returns the high-level groups workflow handle. Used for idempotent
// group provisioning.
func (c *Client) Groups() *GroupsHandle {
	return &GroupsHandle{
		backend: &wfgroups.Backend{Executor: c.executor, IAMAdminURL: c.iamAdminURL},
	}
}

// GroupsHandle wraps the groups workflow.
type GroupsHandle struct {
	backend *wfgroups.Backend
}

// GetOrCreateByName returns the existing group or creates a new top-level
// group. See [wfgroups.GetOrCreateByName].
func (h *GroupsHandle) GetOrCreateByName(ctx context.Context, name string) (*models.Group, error) {
	return wfgroups.GetOrCreateByName(ctx, h.backend, name)
}

// Users returns the high-level user workflow handle. Used to obtain a
// [wfusers.UserReader] for cached lookups by ID/email/username, or to run
// GetOrCreate helpers.
//
//	reader := client.Users().NewReader()
//	user, _ := reader.ByEmail(ctx, "alice@example.com")
func (c *Client) Users() *UsersHandle {
	return &UsersHandle{
		backend: &wfusers.Backend{Executor: c.executor, IAMAdminURL: c.iamAdminURL},
	}
}

// UsersHandle wraps the users workflow.
type UsersHandle struct {
	backend *wfusers.Backend
}

// NewReader constructs a [wfusers.UserReader] with its own lazy cache.
func (h *UsersHandle) NewReader() *wfusers.UserReader {
	return wfusers.NewReader(h.backend)
}

// GetOrCreateByUsername returns the existing user or creates one from the
// supplied template. See [wfusers.GetOrCreateByUsername].
func (h *UsersHandle) GetOrCreateByUsername(ctx context.Context, template *models.User) (*models.User, error) {
	return wfusers.GetOrCreateByUsername(ctx, h.backend, template)
}

// GetOrCreateByEmail returns the existing user or creates one from the
// supplied template. See [wfusers.GetOrCreateByEmail].
func (h *UsersHandle) GetOrCreateByEmail(ctx context.Context, template *models.User) (*models.User, error) {
	return wfusers.GetOrCreateByEmail(ctx, h.backend, template)
}

// Roles returns the high-level role workflow handle. Used to obtain a
// [wfroles.RoleReader] for cached role catalog lookups with composite
// resolution.
func (c *Client) Roles() *RolesHandle {
	return &RolesHandle{
		backend: &wfroles.Backend{Executor: c.executor, IAMAdminURL: c.iamAdminURL},
	}
}

// RolesHandle wraps the roles workflow.
type RolesHandle struct {
	backend *wfroles.Backend
}

// NewReader constructs a [wfroles.RoleReader] with its own lazy cache.
func (h *RolesHandle) NewReader() *wfroles.RoleReader {
	return wfroles.NewReader(h.backend)
}

// Clients returns the high-level OIDC client workflow handle. Used for
// GetOrCreate and the ast-app ID cache.
func (c *Client) Clients() *ClientsHandle {
	return &ClientsHandle{
		backend: &wfclients.Backend{Executor: c.executor, IAMAdminURL: c.iamAdminURL},
	}
}

// ClientsHandle wraps the clients workflow.
type ClientsHandle struct {
	backend    *wfclients.Backend
	astAppOnce sync.Once
	astAppCache *wfclients.ASTAppIDCache
}

// GetOrCreateByName returns the existing OIDC client or creates one. See
// [wfclients.GetOrCreateByName].
func (h *ClientsHandle) GetOrCreateByName(ctx context.Context, name string, defaults *models.OIDCClient) (*models.OIDCClient, error) {
	return wfclients.GetOrCreateByName(ctx, h.backend, name, defaults)
}

// ASTAppID returns the uuid of the "ast-app" Keycloak client. The result is
// cached after the first successful lookup.
func (h *ClientsHandle) ASTAppID(ctx context.Context) (string, error) {
	h.astAppOnce.Do(func() {
		h.astAppCache = wfclients.NewASTAppIDCache(h.backend)
	})
	return h.astAppCache.Get(ctx)
}

// Audit returns the high-level audit session workflow handle.
func (c *Client) Audit() *AuditHandle {
	return &AuditHandle{
		backend: &wfaudit.Backend{Executor: c.executor, BaseURL: c.rootURL()},
	}
}

// AuditHandle wraps the audit workflow.
type AuditHandle struct {
	backend *wfaudit.Backend
}

// WrapSession wraps an already-created [models.AuditSession] into a
// [wfaudit.SessionInspector].
func (h *AuditHandle) WrapSession(session *models.AuditSession) (*wfaudit.SessionInspector, error) {
	return wfaudit.FromSession(h.backend, session)
}

// Migration returns the high-level migration/import workflow handle.
func (c *Client) Migration() *MigrationHandle {
	return &MigrationHandle{
		backend: &wfmigration.Backend{Executor: c.executor, BaseURL: c.rootURL()},
	}
}

// MigrationHandle wraps the migration workflow.
type MigrationHandle struct {
	backend *wfmigration.Backend
}

// GetImport loads the current state of an import by migration id and returns
// an [wfmigration.ImportInspector].
func (h *MigrationHandle) GetImport(ctx context.Context, importID string) (*wfmigration.ImportInspector, error) {
	return wfmigration.FromImportID(ctx, h.backend, importID)
}

// SCM returns the high-level source-control-management workflow handle.
func (c *Client) SCM() *SCMHandle {
	return &SCMHandle{
		backend: &wfscm.Backend{Executor: c.executor, BaseURL: c.rootURL()},
	}
}

// SCMHandle wraps the SCM workflow.
type SCMHandle struct {
	backend *wfscm.Backend
}

// Reconnect points an existing project at a different repository, preserving
// the project and its scan history. It disconnects the project from its
// current integration and submits a conversion against the target repo,
// returning an inspector for the resulting asynchronous process.
//
// The two calls are not atomic — see [wfscm.Reconnect] for the failure
// semantics. Pass the returned inspector to [wfscm.WaitUntilComplete] to poll
// it to a terminal state.
func (h *SCMHandle) Reconnect(ctx context.Context, projectID string, req wfscm.ReconnectRequest) (*wfscm.ConversionInspector, error) {
	return wfscm.Reconnect(ctx, h.backend, projectID, req)
}

// Disconnect reverts a code repository project to a manual project, keeping
// its scan history.
func (h *SCMHandle) Disconnect(ctx context.Context, projectID string) error {
	return wfscm.Disconnect(ctx, h.backend, projectID)
}

// GetConversion loads the current state of a conversion process and returns a
// [wfscm.ConversionInspector]. Process data is retained for 7 days.
func (h *SCMHandle) GetConversion(ctx context.Context, processID string) (*wfscm.ConversionInspector, error) {
	return wfscm.FromProcessID(ctx, h.backend, processID)
}

// AITriage returns the high-level AI Triage workflow handle.
func (c *Client) AITriage() *AITriageHandle {
	return &AITriageHandle{
		backend: &wfaitriage.Backend{Executor: c.executor, BaseURL: c.rootURL()},
	}
}

// AITriageHandle wraps the AI Triage workflow.
type AITriageHandle struct {
	backend *wfaitriage.Backend
}

// Submit requests AI triage of buckets for scanID. A response with
// Published=false (identical job already exists) is a success. HTTP 400, 402
// and 403 are returned as *cxerrors.TriageError.
func (h *AITriageHandle) Submit(ctx context.Context, scanID string, buckets ...models.TriageBucket) (*models.TriageAccepted, error) {
	return wfaitriage.Submit(ctx, h.backend, scanID, buckets...)
}

// Result fetches the triage state for one vulnerability group (use the
// groupId from [RisksHandle.GroupIDs]).
func (h *AITriageHandle) Result(ctx context.Context, projectID, groupID string) (*wfaitriage.ResultInspector, error) {
	return wfaitriage.GetResult(ctx, h.backend, projectID, groupID)
}

// Risks returns the high-level risks handle (GET /api/risks).
func (c *Client) Risks() *RisksHandle {
	return &RisksHandle{executor: c.executor, baseURL: c.rootURL()}
}

// RisksHandle wraps the project risks listing.
type RisksHandle struct {
	executor *transport.Executor
	baseURL  string
}

// GroupIDs pages through GET /api/risks for projectID (optionally filtered to
// engines, e.g. "SAST", "SCA") and returns the unique groupId values in
// first-seen order. Risks without a groupId are skipped.
func (h *RisksHandle) GroupIDs(ctx context.Context, projectID string, engines ...string) ([]string, error) {
	if projectID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "projectID", Reason: "is required"}
	}
	q := url.Values{"projectId": {projectID}}
	for _, e := range engines {
		q.Add("engine", e)
	}
	it := riskseps.Iter(h.executor, h.baseURL, q, pagination.Config{})
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for {
		r, ok, err := it.Next(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if r.GroupID == "" {
			continue
		}
		if _, dup := seen[r.GroupID]; dup {
			continue
		}
		seen[r.GroupID] = struct{}{}
		ids = append(ids, r.GroupID)
	}
	return ids, nil
}

// Advanced returns a typed handle to the SDK's low-level endpoint layer.
//
// The Advanced handle wraps the same executor, HTTP client, and API base URL
// the high-level workflow layer uses, so calls made through it share the
// client's auth token cache, retry policy, observability hooks, and
// connection pool. Use Advanced when:
//
//   - The endpoint you need is not yet covered by a high-level workflow.
//   - You need 1:1 control over an API call's request/response shape.
//   - You're building a custom high-level abstraction on top of the SDK.
//
// Most users should NOT need Advanced — the workflows/ subpackages cover
// the common cases more ergonomically. CLAUDE.md §8 mandates that the
// low-level layer be reachable but never the default surface; Advanced is
// the explicit, opt-in escape hatch.
//
// The returned *advanced.Handle is owned by the client and shares its
// lifecycle. Do not retain it past Client.Close.
func (c *Client) Advanced() *advanced.Handle {
	return advanced.New(c.executor, c.httpClient, c.rootURL(), c.iamAdminURL, c.tenant)
}

// execute is the internal funnel accessor. Currently unused at the package
// level (the endpoint layer reaches the executor through the advanced
// package); kept here so test helpers and future internal callers in this
// package have a single place to invoke the funnel.
func (c *Client) execute(ctx context.Context, req *transport.Request) (*transport.Response, error) {
	return c.executor.Do(ctx, req)
}
