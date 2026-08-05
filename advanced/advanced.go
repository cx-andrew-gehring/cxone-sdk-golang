// Package advanced exposes the SDK's low-level endpoint layer to power
// users who need direct, 1:1 access to the Checkmarx One REST API surface.
//
// This is the only public path to the internal/endpoints/* packages — they
// live under internal/ specifically so the default consumption pattern is
// the high-level workflow layer (cxone/workflows/...). Reach this package
// through the cxone client:
//
//	client, _ := cxone.NewClient().Region(...).Tenant(...).AgentName(...).APIKey(...).Build()
//	defer client.Close()
//
//	adv := client.Advanced()
//	project, err := adv.Projects().Create(ctx, &models.Project{Name: "demo"})
//
// Stability: the function and method signatures here are part of the public
// API and follow the SDK's semver policy. The wrapped internal packages can
// add new exported functions without a major bump; existing ones cannot
// change incompatibly.
package advanced

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/access"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/analytics"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/applications"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/audit"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/bfl"
	iamclients "github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/clients"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/export"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/featureflags"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/groups"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/logs"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/migration"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/policies"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/predicates"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/presetmanager"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/projects"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/reports"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/roles"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/results"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/risks"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/sastmetadata"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/scm"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/scanoverview"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/scans"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/telemetry"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/tenant"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/uploads"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/endpoints/users"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/internal/transport"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/models"
	"github.com/checkmarx-open-labs/cxone-sdk-golang/pagination"
)

// Handle bundles the dependencies the endpoint packages need: the request
// funnel, the host root the client was built against, the tenant name (used
// by IAM-side endpoints like Groups), and the underlying HTTP client (for
// uploads' presigned-URL PUT path which bypasses the funnel).
//
// The "rootURL" the Handle holds is the host root (e.g.
// "https://ast.checkmarx.net/"), NOT the /api/ subpath. Each endpoint
// package declares its own path prefix ("api/projects",
// "auth/realms/.../pip/groups", etc.) so we can address everything that
// lives on the same host with a single base.
//
// Construction is internal; obtain a Handle via cxone.Client.Advanced(). The
// fields are unexported because the SDK relies on the cxone.Client lifecycle
// to manage them; reaching in directly would risk leaking the executor past
// Client.Close.
type Handle struct {
	executor    *transport.Executor
	httpClient  *http.Client
	rootURL     string
	iamAdminURL string
	tenant      string
}

// New constructs a Handle. Used by the cxone package's Client.Advanced()
// accessor and by tests; not part of the public API and may change.
//
// rootURL is the API host root (e.g. "https://ast.checkmarx.net/"), used by
// every /api/... endpoint. iamAdminURL is the tenant-scoped IAM admin base
// (e.g. "https://iam.checkmarx.net/auth/admin/realms/<tenant>/"), used by the
// Keycloak-backed administration endpoints (users, roles, clients). Endpoint
// packages pick whichever base URL applies to their path.
func New(executor *transport.Executor, httpClient *http.Client, rootURL, iamAdminURL, tenant string) *Handle {
	return &Handle{
		executor:    executor,
		httpClient:  httpClient,
		rootURL:     rootURL,
		iamAdminURL: iamAdminURL,
		tenant:      tenant,
	}
}

// Projects returns a typed handle to the Checkmarx One Projects endpoints.
func (h *Handle) Projects() *Projects { return &Projects{h: h} }

// Scans returns a typed handle to the Checkmarx One Scans endpoints.
func (h *Handle) Scans() *Scans { return &Scans{h: h} }

// Uploads returns a typed handle to the Checkmarx One Uploads endpoints.
func (h *Handle) Uploads() *Uploads { return &Uploads{h: h} }

// Results returns a typed handle to the Checkmarx One cross-engine Results endpoint.
func (h *Handle) Results() *Results { return &Results{h: h} }

// Reports returns a typed handle to the JSON/PDF reports endpoints.
func (h *Handle) Reports() *Reports { return &Reports{h: h} }

// Policies returns a typed handle to the policy evaluation endpoint.
func (h *Handle) Policies() *Policies { return &Policies{h: h} }

// Groups returns a typed handle to the IAM groups lookup endpoint.
// Calls to Groups automatically substitute the client's tenant into the path.
func (h *Handle) Groups() *Groups { return &Groups{h: h} }

// Access returns a typed handle to the Access Management endpoints.
func (h *Handle) Access() *Access { return &Access{h: h} }

// Applications returns a typed handle to the Applications endpoints.
func (h *Handle) Applications() *Applications { return &Applications{h: h} }

// FeatureFlags returns a typed handle to the feature-flags endpoint.
func (h *Handle) FeatureFlags() *FeatureFlags { return &FeatureFlags{h: h} }

// SastMetadata returns a typed handle to the SAST metadata endpoint.
func (h *Handle) SastMetadata() *SastMetadata { return &SastMetadata{h: h} }

// Tenant returns a typed handle to the tenant configuration endpoint.
func (h *Handle) Tenant() *Tenant { return &Tenant{h: h} }

// Logs returns a typed handle to the scan logs endpoint.
func (h *Handle) Logs() *Logs { return &Logs{h: h} }

// BFL returns a typed handle to the Best Fix Location endpoint.
func (h *Handle) BFL() *BFL { return &BFL{h: h} }

// Predicates returns a typed handle to the per-engine predicates endpoints.
func (h *Handle) Predicates() *Predicates { return &Predicates{h: h} }

// Export returns a typed handle to the SCA export endpoint.
func (h *Handle) Export() *Export { return &Export{h: h} }

// ScanOverview returns a typed handle to the SCS scan-overview endpoint.
func (h *Handle) ScanOverview() *ScanOverview { return &ScanOverview{h: h} }

// Risks returns a typed handle to the Risk Management + APISec risks endpoints.
func (h *Handle) Risks() *Risks { return &Risks{h: h} }

// Telemetry returns a typed handle to the AI telemetry endpoint.
func (h *Handle) Telemetry() *Telemetry { return &Telemetry{h: h} }

// Users returns a typed handle to the IAM user-administration endpoints.
// These calls target the tenant's Keycloak admin realm, not the API host.
func (h *Handle) Users() *Users { return &Users{h: h} }

// Roles returns a typed handle to the IAM role-administration endpoints
// (realm roles, client roles, composites).
func (h *Handle) Roles() *Roles { return &Roles{h: h} }

// Clients returns a typed handle to the OIDC client-administration endpoints
// (the Keycloak "clients" resource — both the ast-app client and any
// tenant-created service accounts).
func (h *Handle) Clients() *Clients { return &Clients{h: h} }

// Analytics returns a typed handle to the analytics KPI endpoints.
func (h *Handle) Analytics() *Analytics { return &Analytics{h: h} }

// Audit returns a typed handle to the query-editor (audit) session endpoints.
func (h *Handle) Audit() *Audit { return &Audit{h: h} }

// Migration returns a typed handle to the data-import (migration) endpoints.
func (h *Handle) Migration() *Migration { return &Migration{h: h} }

// SCM returns a typed handle to the repos-manager (SCM) endpoints.
func (h *Handle) SCM() *SCM { return &SCM{h: h} }

// PresetManager returns a typed handle to the preset-manager endpoints for
// SAST/IAC preset CRUD and query-family browsing.
func (h *Handle) PresetManager() *PresetManager { return &PresetManager{h: h} }

// ----- Projects --------------------------------------------------------------

// Projects wraps the projects endpoint package with method-style ergonomics.
//
// Methods preserve 1:1 parity with the underlying internal/endpoints/projects
// functions; the only thing that changes is that the executor and base URL
// are bound from the parent client.
type Projects struct{ h *Handle }

func (p *Projects) Create(ctx context.Context, in *models.Project) (*models.ProjectResponseModel, error) {
	return projects.Create(ctx, p.h.executor, p.h.rootURL, in)
}

func (p *Projects) List(ctx context.Context, query url.Values) (*models.ProjectsCollectionResponseModel, error) {
	return projects.List(ctx, p.h.executor, p.h.rootURL, query)
}

func (p *Projects) Get(ctx context.Context, projectID string) (*models.ProjectResponseModel, error) {
	return projects.Get(ctx, p.h.executor, p.h.rootURL, projectID)
}

func (p *Projects) Update(ctx context.Context, projectID string, in *models.Project) error {
	return projects.Update(ctx, p.h.executor, p.h.rootURL, projectID, in)
}

func (p *Projects) Delete(ctx context.Context, projectID string) error {
	return projects.Delete(ctx, p.h.executor, p.h.rootURL, projectID)
}

func (p *Projects) GetBranches(ctx context.Context, projectID string, query url.Values) (models.BranchList, error) {
	return projects.GetBranches(ctx, p.h.executor, p.h.rootURL, projectID, query)
}

func (p *Projects) Tags(ctx context.Context) (map[string][]string, error) {
	return projects.Tags(ctx, p.h.executor, p.h.rootURL)
}

func (p *Projects) UpdateConfiguration(ctx context.Context, projectID string, cfg []models.ProjectConfiguration) error {
	return projects.UpdateConfiguration(ctx, p.h.executor, p.h.rootURL, projectID, cfg)
}

// Iter returns an item-yielding iterator that walks every project matching
// query, transparently fetching successive pages. See [pagination.Iterator]
// for the streaming contract.
func (p *Projects) Iter(query url.Values, cfg pagination.Config) *pagination.Iterator[models.ProjectResponseModel] {
	return projects.Iter(p.h.executor, p.h.rootURL, query, cfg)
}

// GetConfiguration returns the per-project configuration overrides.
func (p *Projects) GetConfiguration(ctx context.Context, projectID string) ([]models.ProjectConfiguration, error) {
	return projects.GetConfiguration(ctx, p.h.executor, p.h.rootURL, projectID)
}

func (p *Projects) Count(ctx context.Context, query url.Values) (int, error) {
	return projects.Count(ctx, p.h.executor, p.h.rootURL, query)
}
func (p *Projects) Patch(ctx context.Context, projectID string, patch *models.ProjectPatch) error {
	return projects.Patch(ctx, p.h.executor, p.h.rootURL, projectID, patch)
}
func (p *Projects) SetConfigurationKey(ctx context.Context, projectID, key, value string, allowOverride bool) error {
	return projects.SetConfigurationKey(ctx, p.h.executor, p.h.rootURL, projectID, key, value, allowOverride)
}
func (p *Projects) AssignApplicationsDirect(ctx context.Context, projectID string, applicationIDs []string) error {
	return projects.AssignApplicationsDirect(ctx, p.h.executor, p.h.rootURL, projectID, applicationIDs)
}
func (p *Projects) RemoveApplicationsDirect(ctx context.Context, projectID string, applicationIDs []string) error {
	return projects.RemoveApplicationsDirect(ctx, p.h.executor, p.h.rootURL, projectID, applicationIDs)
}

// ----- Scans -----------------------------------------------------------------

// Scans wraps the scans endpoint package.
type Scans struct{ h *Handle }

func (s *Scans) Create(ctx context.Context, in *models.Scan) (*models.ScanResponseModel, error) {
	return scans.Create(ctx, s.h.executor, s.h.rootURL, in)
}

func (s *Scans) List(ctx context.Context, query url.Values) (*models.ScansCollectionResponseModel, error) {
	return scans.List(ctx, s.h.executor, s.h.rootURL, query)
}

func (s *Scans) Get(ctx context.Context, scanID string) (*models.ScanResponseModel, error) {
	return scans.Get(ctx, s.h.executor, s.h.rootURL, scanID)
}

func (s *Scans) GetWorkflow(ctx context.Context, scanID string) ([]models.ScanTaskResponseModel, error) {
	return scans.GetWorkflow(ctx, s.h.executor, s.h.rootURL, scanID)
}

func (s *Scans) Cancel(ctx context.Context, scanID string) error {
	return scans.Cancel(ctx, s.h.executor, s.h.rootURL, scanID)
}

func (s *Scans) Delete(ctx context.Context, scanID string) error {
	return scans.Delete(ctx, s.h.executor, s.h.rootURL, scanID)
}

func (s *Scans) Tags(ctx context.Context) (map[string][]string, error) {
	return scans.Tags(ctx, s.h.executor, s.h.rootURL)
}

// Iter returns an item-yielding iterator over the scans matching query.
func (s *Scans) Iter(query url.Values, cfg pagination.Config) *pagination.Iterator[models.ScanResponseModel] {
	return scans.Iter(s.h.executor, s.h.rootURL, query, cfg)
}

// ----- Uploads ---------------------------------------------------------------

// Uploads wraps the uploads endpoint package.
type Uploads struct{ h *Handle }

// GetPresignedURL requests a one-shot presigned URL for uploading a source
// archive. See [uploads.GetPresignedURL].
func (u *Uploads) GetPresignedURL(ctx context.Context) (string, error) {
	return uploads.GetPresignedURL(ctx, u.h.executor, u.h.rootURL)
}

// PutFile uploads body (size bytes) to the supplied presigned URL using the
// client's pooled HTTP client. See [uploads.PutFile] for the rationale on
// why this bypasses the funnel.
func (u *Uploads) PutFile(ctx context.Context, presignedURL string, body io.Reader, size int64) error {
	return uploads.PutFile(ctx, u.h.httpClient, presignedURL, body, size)
}

// UploadFromReader is a convenience that combines [Uploads.GetPresignedURL]
// and [Uploads.PutFile] into a single call. Returns the presigned URL the
// scan service should reference when starting the scan.
func (u *Uploads) UploadFromReader(ctx context.Context, body io.Reader, size int64) (string, error) {
	url, err := u.GetPresignedURL(ctx)
	if err != nil {
		return "", err
	}
	if err := u.PutFile(ctx, url, body, size); err != nil {
		return "", err
	}
	return url, nil
}

// ----- Results ---------------------------------------------------------------

// Results wraps the cross-engine /api/results endpoint.
type Results struct{ h *Handle }

// List returns one page of scan results. query MUST contain "scan-id".
func (r *Results) List(ctx context.Context, query url.Values) (*models.ScanResultsCollection, error) {
	return results.List(ctx, r.h.executor, r.h.rootURL, query)
}

// Iter returns an item-yielding iterator over the results matching query.
// query MUST contain "scan-id".
func (r *Results) Iter(query url.Values, cfg pagination.Config) *pagination.Iterator[*models.ScanResult] {
	return results.Iter(r.h.executor, r.h.rootURL, query, cfg)
}

// ----- Reports ---------------------------------------------------------------

// Reports wraps the JSON and PDF report endpoints.
type Reports struct{ h *Handle }

func (r *Reports) CreateJSON(ctx context.Context, in *models.JSONReportRequest) (*models.JSONReportResponse, error) {
	return reports.CreateJSON(ctx, r.h.executor, r.h.rootURL, in)
}
func (r *Reports) PollJSON(ctx context.Context, reportID string) (*models.JSONReportPollingResponse, error) {
	return reports.PollJSON(ctx, r.h.executor, r.h.rootURL, reportID)
}
func (r *Reports) DownloadJSON(ctx context.Context, reportID string) (io.ReadCloser, error) {
	return reports.DownloadJSON(ctx, r.h.executor, r.h.rootURL, reportID)
}
func (r *Reports) CreatePDF(ctx context.Context, in *models.PDFReportRequest) (*models.PDFReportResponse, error) {
	return reports.CreatePDF(ctx, r.h.executor, r.h.rootURL, in)
}
func (r *Reports) PollPDF(ctx context.Context, reportID string) (*models.PDFReportPollingResponse, error) {
	return reports.PollPDF(ctx, r.h.executor, r.h.rootURL, reportID)
}
func (r *Reports) DownloadPDF(ctx context.Context, reportID string) (io.ReadCloser, error) {
	return reports.DownloadPDF(ctx, r.h.executor, r.h.rootURL, reportID)
}

// ----- Policies --------------------------------------------------------------

// Policies wraps the policy evaluation endpoint.
type Policies struct{ h *Handle }

func (p *Policies) Evaluate(ctx context.Context, scanID string) (*models.PolicyResponseModel, error) {
	return policies.Evaluate(ctx, p.h.executor, p.h.rootURL, scanID)
}
func (p *Policies) List(ctx context.Context, filter models.PolicyFilter) (*models.PolicyListResponse, error) {
	return policies.List(ctx, p.h.executor, p.h.rootURL, filter)
}
func (p *Policies) Count(ctx context.Context, filter models.PolicyFilter) (uint64, error) {
	return policies.CountFiltered(ctx, p.h.executor, p.h.rootURL, filter)
}
func (p *Policies) ListViolations(ctx context.Context, filter models.PolicyViolationFilter) (*models.PolicyViolationListResponse, error) {
	return policies.ListViolations(ctx, p.h.executor, p.h.rootURL, filter)
}
func (p *Policies) CountViolations(ctx context.Context, filter models.PolicyViolationFilter) (uint64, error) {
	return policies.CountViolationsFiltered(ctx, p.h.executor, p.h.rootURL, filter)
}
func (p *Policies) GetViolationDetails(ctx context.Context, projectID, scanID string) (*models.PolicyViolationDetails, error) {
	return policies.GetViolationDetails(ctx, p.h.executor, p.h.rootURL, projectID, scanID)
}
func (p *Policies) AssignToProjects(ctx context.Context, policyID string, projects []models.ProjectResponseModel) error {
	return policies.AssignToProjects(ctx, p.h.executor, p.h.rootURL, policyID, projects)
}

// ----- Groups ----------------------------------------------------------------

// Groups wraps the IAM groups lookup endpoint. The client's tenant is
// substituted into the path automatically.
type Groups struct{ h *Handle }

func (g *Groups) List(ctx context.Context, groupName string) ([]models.Group, error) {
	return groups.List(ctx, g.h.executor, g.h.rootURL, g.h.tenant, groupName)
}

// Admin-realm (Keycloak) group methods. These target the tenant admin URL
// rather than the PIP endpoint and expose full CRUD + hierarchy + role
// binding access.

func (g *Groups) ListAdmin(ctx context.Context, filter models.GroupFilter) ([]models.Group, error) {
	return groups.ListAdmin(ctx, g.h.executor, g.h.iamAdminURL, filter)
}
func (g *Groups) Count(ctx context.Context, filter models.GroupFilter) (uint64, error) {
	return groups.Count(ctx, g.h.executor, g.h.iamAdminURL, filter)
}
func (g *Groups) GetByID(ctx context.Context, groupID string) (*models.Group, error) {
	return groups.GetByID(ctx, g.h.executor, g.h.iamAdminURL, groupID)
}
func (g *Groups) GetByPath(ctx context.Context, path string) (*models.Group, error) {
	return groups.GetByPath(ctx, g.h.executor, g.h.iamAdminURL, path)
}
func (g *Groups) GetChildren(ctx context.Context, groupID string, first, max int) ([]models.Group, error) {
	return groups.GetChildren(ctx, g.h.executor, g.h.iamAdminURL, groupID, first, max)
}
func (g *Groups) CreateAdmin(ctx context.Context, in *models.Group) (*models.Group, error) {
	return groups.CreateAdmin(ctx, g.h.executor, g.h.iamAdminURL, in)
}
func (g *Groups) CreateChild(ctx context.Context, parentID string, in *models.Group) (*models.Group, error) {
	return groups.CreateChild(ctx, g.h.executor, g.h.iamAdminURL, parentID, in)
}
func (g *Groups) UpdateAdmin(ctx context.Context, in *models.Group) error {
	return groups.UpdateAdmin(ctx, g.h.executor, g.h.iamAdminURL, in)
}
func (g *Groups) DeleteAdmin(ctx context.Context, groupID string) error {
	return groups.DeleteAdmin(ctx, g.h.executor, g.h.iamAdminURL, groupID)
}
func (g *Groups) GetMembers(ctx context.Context, groupID string) ([]models.User, error) {
	return groups.GetMembers(ctx, g.h.executor, g.h.iamAdminURL, groupID)
}
func (g *Groups) GetRealmRoles(ctx context.Context, groupID string) ([]models.Role, error) {
	return groups.GetRealmRoles(ctx, g.h.executor, g.h.iamAdminURL, groupID)
}
func (g *Groups) AddRealmRoles(ctx context.Context, groupID string, roles []models.Role) error {
	return groups.AddRealmRoles(ctx, g.h.executor, g.h.iamAdminURL, groupID, roles)
}
func (g *Groups) RemoveRealmRoles(ctx context.Context, groupID string, roles []models.Role) error {
	return groups.RemoveRealmRoles(ctx, g.h.executor, g.h.iamAdminURL, groupID, roles)
}
func (g *Groups) GetClientRoles(ctx context.Context, groupID, clientID string) ([]models.Role, error) {
	return groups.GetClientRoles(ctx, g.h.executor, g.h.iamAdminURL, groupID, clientID)
}
func (g *Groups) AddClientRoles(ctx context.Context, groupID, clientID string, roles []models.Role) error {
	return groups.AddClientRoles(ctx, g.h.executor, g.h.iamAdminURL, groupID, clientID, roles)
}
func (g *Groups) RemoveClientRoles(ctx context.Context, groupID, clientID string, roles []models.Role) error {
	return groups.RemoveClientRoles(ctx, g.h.executor, g.h.iamAdminURL, groupID, clientID, roles)
}

// ----- Access ----------------------------------------------------------------

// Access wraps the Access Management endpoints.
type Access struct{ h *Handle }

func (a *Access) CreateAssignment(ctx context.Context, in *models.AssignmentPayload) error {
	return access.CreateAssignment(ctx, a.h.executor, a.h.rootURL, in)
}
func (a *Access) EntitiesFor(ctx context.Context, resourceID, resourceType string) ([]*models.AssignmentResponse, error) {
	return access.EntitiesFor(ctx, a.h.executor, a.h.rootURL, resourceID, resourceType)
}
func (a *Access) GetAssignment(ctx context.Context, entityID, resourceID string) (*models.AssignmentResponse, error) {
	return access.GetAssignment(ctx, a.h.executor, a.h.rootURL, entityID, resourceID)
}
func (a *Access) DeleteAssignment(ctx context.Context, entityID, resourceID string) error {
	return access.DeleteAssignment(ctx, a.h.executor, a.h.rootURL, entityID, resourceID)
}
func (a *Access) ResourcesFor(ctx context.Context, entityID, entityType string, resourceTypes []string) ([]*models.AssignmentResponse, error) {
	return access.ResourcesFor(ctx, a.h.executor, a.h.rootURL, entityID, entityType, resourceTypes)
}
func (a *Access) HasAccess(ctx context.Context, resourceID, resourceType, action string) (bool, error) {
	return access.HasAccess(ctx, a.h.executor, a.h.rootURL, resourceID, resourceType, action)
}
func (a *Access) AccessibleResources(ctx context.Context, resourceTypes []string, action string) (*models.AccessibleResourcesResponse, error) {
	return access.AccessibleResources(ctx, a.h.executor, a.h.rootURL, resourceTypes, action)
}

// ----- Applications ----------------------------------------------------------

// Applications wraps the Applications endpoints.
type Applications struct{ h *Handle }

func (a *Applications) List(ctx context.Context, query url.Values) (*models.ApplicationsResponseModel, error) {
	return applications.List(ctx, a.h.executor, a.h.rootURL, query)
}
func (a *Applications) Update(ctx context.Context, appID string, in *models.ApplicationConfiguration) error {
	return applications.Update(ctx, a.h.executor, a.h.rootURL, appID, in)
}
func (a *Applications) AssociateProjects(ctx context.Context, appID string, projectIDs []string) error {
	return applications.AssociateProjects(ctx, a.h.executor, a.h.rootURL, appID, projectIDs)
}
func (a *Applications) Get(ctx context.Context, appID string) (*models.Application, error) {
	return applications.Get(ctx, a.h.executor, a.h.rootURL, appID)
}
func (a *Applications) Create(ctx context.Context, in *models.ApplicationCreateRequest) (*models.Application, error) {
	return applications.Create(ctx, a.h.executor, a.h.rootURL, in)
}
func (a *Applications) Delete(ctx context.Context, appID string) error {
	return applications.Delete(ctx, a.h.executor, a.h.rootURL, appID)
}
func (a *Applications) Patch(ctx context.Context, appID string, patch *models.ApplicationPatch) error {
	return applications.Patch(ctx, a.h.executor, a.h.rootURL, appID, patch)
}
func (a *Applications) Count(ctx context.Context, query url.Values) (int, error) {
	return applications.Count(ctx, a.h.executor, a.h.rootURL, query)
}
func (a *Applications) AssignProjectsDirect(ctx context.Context, appID string, projectIDs []string) error {
	return applications.AssignProjectsDirect(ctx, a.h.executor, a.h.rootURL, appID, projectIDs)
}
func (a *Applications) RemoveProjectsDirect(ctx context.Context, appID string, projectIDs []string) error {
	return applications.RemoveProjectsDirect(ctx, a.h.executor, a.h.rootURL, appID, projectIDs)
}

// Iter returns an item-yielding iterator over the applications matching query.
func (a *Applications) Iter(query url.Values, cfg pagination.Config) *pagination.Iterator[models.Application] {
	return applications.Iter(a.h.executor, a.h.rootURL, query, cfg)
}

// ----- FeatureFlags ----------------------------------------------------------

// FeatureFlags wraps the feature-flags lookup endpoint.
type FeatureFlags struct{ h *Handle }

func (f *FeatureFlags) List(ctx context.Context, tenantID string) ([]models.FeatureFlag, error) {
	return featureflags.List(ctx, f.h.executor, f.h.rootURL, tenantID)
}
func (f *FeatureFlags) Get(ctx context.Context, tenantID, name string) (*models.FeatureFlag, error) {
	return featureflags.Get(ctx, f.h.executor, f.h.rootURL, tenantID, name)
}

// ----- SastMetadata ----------------------------------------------------------

// SastMetadata wraps the SAST metadata endpoint.
type SastMetadata struct{ h *Handle }

func (s *SastMetadata) List(ctx context.Context, scanIDs ...string) (*models.SastMetadataModel, error) {
	return sastmetadata.List(ctx, s.h.executor, s.h.rootURL, scanIDs...)
}

// ----- Tenant ----------------------------------------------------------------

// Tenant wraps the tenant configuration endpoint.
type Tenant struct{ h *Handle }

func (t *Tenant) GetConfiguration(ctx context.Context) ([]*models.TenantConfigurationEntry, error) {
	return tenant.GetConfiguration(ctx, t.h.executor, t.h.rootURL)
}

// ----- Logs ------------------------------------------------------------------

// Logs wraps the engine logs endpoint.
type Logs struct{ h *Handle }

func (l *Logs) Get(ctx context.Context, scanID, scanType string) (string, error) {
	return logs.Get(ctx, l.h.executor, l.h.rootURL, scanID, scanType)
}

// ----- BFL -------------------------------------------------------------------

// BFL wraps the Best Fix Location endpoint.
type BFL struct{ h *Handle }

func (b *BFL) Get(ctx context.Context, scanID, queryID string) (*models.BFLResponseModel, error) {
	return bfl.Get(ctx, b.h.executor, b.h.rootURL, scanID, queryID)
}

// ----- Predicates ------------------------------------------------------------

// Predicates wraps the per-engine predicates endpoints.
type Predicates struct{ h *Handle }

func (p *Predicates) GetSAST(ctx context.Context, similarityID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	return predicates.GetSAST(ctx, p.h.executor, p.h.rootURL, similarityID, projectIDs)
}
func (p *Predicates) GetKICS(ctx context.Context, similarityID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	return predicates.GetKICS(ctx, p.h.executor, p.h.rootURL, similarityID, projectIDs)
}
func (p *Predicates) GetSCS(ctx context.Context, similarityID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	return predicates.GetSCS(ctx, p.h.executor, p.h.rootURL, similarityID, projectIDs)
}
func (p *Predicates) UpdateSAST(ctx context.Context, in []models.PredicateRequest) error {
	return predicates.UpdateSAST(ctx, p.h.executor, p.h.rootURL, in)
}
func (p *Predicates) UpdateKICS(ctx context.Context, in []models.PredicateRequest) error {
	return predicates.UpdateKICS(ctx, p.h.executor, p.h.rootURL, in)
}
func (p *Predicates) UpdateSCS(ctx context.Context, in *models.PredicateRequest) error {
	return predicates.UpdateSCS(ctx, p.h.executor, p.h.rootURL, in)
}
func (p *Predicates) UpdateSCA(ctx context.Context, in *models.ScaPredicateRequest) error {
	return predicates.UpdateSCA(ctx, p.h.executor, p.h.rootURL, in)
}
func (p *Predicates) ListCustomStates(ctx context.Context, includeDeleted bool) ([]models.CustomState, error) {
	return predicates.ListCustomStates(ctx, p.h.executor, p.h.rootURL, includeDeleted)
}
func (p *Predicates) CreateCustomState(ctx context.Context, name string) (*models.CustomState, error) {
	return predicates.CreateCustomState(ctx, p.h.executor, p.h.rootURL, name)
}
func (p *Predicates) DeleteCustomState(ctx context.Context, stateID int) error {
	return predicates.DeleteCustomState(ctx, p.h.executor, p.h.rootURL, stateID)
}
func (p *Predicates) GetSASTLatest(ctx context.Context, similarityID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	return predicates.GetSASTLatest(ctx, p.h.executor, p.h.rootURL, similarityID, projectIDs)
}
func (p *Predicates) GetSASTWithScan(ctx context.Context, similarityID, scanID string, projectIDs []string) (*models.PredicatesCollectionResponseModel, error) {
	return predicates.GetSASTWithScan(ctx, p.h.executor, p.h.rootURL, similarityID, scanID, projectIDs)
}
func (p *Predicates) GetChangeHistory(ctx context.Context, entityType, entityID string, limit, offset int) (*models.ResultsChangeHistoryResponse, error) {
	return predicates.GetChangeHistory(ctx, p.h.executor, p.h.rootURL, entityType, entityID, limit, offset)
}

// ----- Export ----------------------------------------------------------------

// Export wraps the SCA export endpoints.
type Export struct{ h *Handle }

func (e *Export) Create(ctx context.Context, in *models.ExportRequestPayload) (*models.ExportResponse, error) {
	return export.Create(ctx, e.h.executor, e.h.rootURL, in)
}
func (e *Export) Poll(ctx context.Context, exportID string) (*models.ExportPollingResponse, error) {
	return export.Poll(ctx, e.h.executor, e.h.rootURL, exportID)
}
func (e *Export) Download(ctx context.Context, exportID string) (io.ReadCloser, error) {
	return export.Download(ctx, e.h.executor, e.h.rootURL, exportID)
}

// ----- ScanOverview ----------------------------------------------------------

// ScanOverview wraps the SCS scan-overview endpoint.
type ScanOverview struct{ h *Handle }

func (s *ScanOverview) Get(ctx context.Context, scanID string) (*models.SCSOverview, error) {
	return scanoverview.Get(ctx, s.h.executor, s.h.rootURL, scanID)
}

// ----- Risks -----------------------------------------------------------------

// Risks wraps the Risk Management + APISec risks endpoints.
type Risks struct{ h *Handle }

func (r *Risks) GetRiskManagement(ctx context.Context, projectID, scanID string) (*models.ASPMResult, error) {
	return risks.GetRiskManagement(ctx, r.h.executor, r.h.rootURL, projectID, scanID)
}
func (r *Risks) GetAPISecOverview(ctx context.Context, scanID string) (*models.APISecResult, error) {
	return risks.GetAPISecOverview(ctx, r.h.executor, r.h.rootURL, scanID)
}
func (r *Risks) ListAPISecRisks(ctx context.Context, query url.Values) (*models.APISecPaginatedResult, error) {
	return risks.ListAPISecRisks(ctx, r.h.executor, r.h.rootURL, query)
}

// ----- Telemetry -------------------------------------------------------------

// Telemetry wraps the AI telemetry endpoint.
type Telemetry struct{ h *Handle }

func (t *Telemetry) LogAIEvent(ctx context.Context, in *models.AITelemetryEvent) error {
	return telemetry.LogAIEvent(ctx, t.h.executor, t.h.rootURL, in)
}

// ----- Users -----------------------------------------------------------------

// Users wraps the IAM user-administration endpoints. All calls target the
// per-tenant Keycloak admin realm the client was built against.
type Users struct{ h *Handle }

func (u *Users) List(ctx context.Context, filter models.UserFilter) ([]models.User, error) {
	return users.List(ctx, u.h.executor, u.h.iamAdminURL, filter)
}
func (u *Users) Count(ctx context.Context, filter models.UserFilter) (uint64, error) {
	return users.Count(ctx, u.h.executor, u.h.iamAdminURL, filter)
}
func (u *Users) Get(ctx context.Context, userID string) (*models.User, error) {
	return users.Get(ctx, u.h.executor, u.h.iamAdminURL, userID)
}
func (u *Users) Create(ctx context.Context, in *models.User) (*models.User, error) {
	return users.Create(ctx, u.h.executor, u.h.iamAdminURL, in)
}
func (u *Users) CreateSAML(ctx context.Context, in *models.User, idpAlias, idpUserID, idpUserName string) (*models.User, error) {
	return users.CreateSAML(ctx, u.h.executor, u.h.iamAdminURL, in, idpAlias, idpUserID, idpUserName)
}
func (u *Users) Update(ctx context.Context, user *models.User) error {
	return users.Update(ctx, u.h.executor, u.h.iamAdminURL, user)
}
func (u *Users) Delete(ctx context.Context, userID string) error {
	return users.Delete(ctx, u.h.executor, u.h.iamAdminURL, userID)
}
func (u *Users) GetGroups(ctx context.Context, userID string) ([]models.Group, error) {
	return users.GetGroups(ctx, u.h.executor, u.h.iamAdminURL, userID)
}
func (u *Users) AssignGroup(ctx context.Context, userID, groupID string) error {
	return users.AssignGroup(ctx, u.h.executor, u.h.iamAdminURL, userID, groupID)
}
func (u *Users) RemoveGroup(ctx context.Context, userID, groupID string) error {
	return users.RemoveGroup(ctx, u.h.executor, u.h.iamAdminURL, userID, groupID)
}
func (u *Users) GetClientRoles(ctx context.Context, userID, clientID string) ([]models.Role, error) {
	return users.GetClientRoles(ctx, u.h.executor, u.h.iamAdminURL, userID, clientID)
}
func (u *Users) AddClientRoles(ctx context.Context, userID, clientID string, roles []models.Role) error {
	return users.AddClientRoles(ctx, u.h.executor, u.h.iamAdminURL, userID, clientID, roles)
}
func (u *Users) RemoveClientRoles(ctx context.Context, userID, clientID string, roles []models.Role) error {
	return users.RemoveClientRoles(ctx, u.h.executor, u.h.iamAdminURL, userID, clientID, roles)
}
func (u *Users) GetRealmRoles(ctx context.Context, userID string) ([]models.Role, error) {
	return users.GetRealmRoles(ctx, u.h.executor, u.h.iamAdminURL, userID)
}
func (u *Users) AddRealmRoles(ctx context.Context, userID string, roles []models.Role) error {
	return users.AddRealmRoles(ctx, u.h.executor, u.h.iamAdminURL, userID, roles)
}
func (u *Users) RemoveRealmRoles(ctx context.Context, userID string, roleList []models.Role) error {
	return users.RemoveRealmRoles(ctx, u.h.executor, u.h.iamAdminURL, userID, roleList)
}

// ----- Roles -----------------------------------------------------------------

// Roles wraps the IAM role-administration endpoints. Realm roles and
// client-scoped roles are surfaced separately; composites are managed via
// the roles-by-id sub-resource.
type Roles struct{ h *Handle }

func (r *Roles) ListRealm(ctx context.Context) ([]models.Role, error) {
	return roles.ListRealm(ctx, r.h.executor, r.h.iamAdminURL)
}
func (r *Roles) GetRealmByName(ctx context.Context, name string) (*models.Role, error) {
	return roles.GetRealmByName(ctx, r.h.executor, r.h.iamAdminURL, name)
}
func (r *Roles) SearchRealm(ctx context.Context, search string) ([]models.Role, error) {
	return roles.SearchRealm(ctx, r.h.executor, r.h.iamAdminURL, search)
}
func (r *Roles) ListClient(ctx context.Context, clientID string) ([]models.Role, error) {
	return roles.ListClient(ctx, r.h.executor, r.h.iamAdminURL, clientID)
}
func (r *Roles) GetClientByName(ctx context.Context, clientID, name string) (*models.Role, error) {
	return roles.GetClientByName(ctx, r.h.executor, r.h.iamAdminURL, clientID, name)
}
func (r *Roles) SearchClient(ctx context.Context, clientID, search string) ([]models.Role, error) {
	return roles.SearchClient(ctx, r.h.executor, r.h.iamAdminURL, clientID, search)
}
func (r *Roles) CreateClient(ctx context.Context, clientID string, in *models.Role) error {
	return roles.CreateClient(ctx, r.h.executor, r.h.iamAdminURL, clientID, in)
}
func (r *Roles) GetByID(ctx context.Context, roleID string) (*models.Role, error) {
	return roles.GetByID(ctx, r.h.executor, r.h.iamAdminURL, roleID)
}
func (r *Roles) DeleteByID(ctx context.Context, roleID string) error {
	return roles.DeleteByID(ctx, r.h.executor, r.h.iamAdminURL, roleID)
}
func (r *Roles) GetComposites(ctx context.Context, roleID string) ([]models.Role, error) {
	return roles.GetComposites(ctx, r.h.executor, r.h.iamAdminURL, roleID)
}
func (r *Roles) AddComposites(ctx context.Context, roleID string, subRoles []models.Role) error {
	return roles.AddComposites(ctx, r.h.executor, r.h.iamAdminURL, roleID, subRoles)
}
func (r *Roles) RemoveComposites(ctx context.Context, roleID string, subRoles []models.Role) error {
	return roles.RemoveComposites(ctx, r.h.executor, r.h.iamAdminURL, roleID, subRoles)
}

// ----- Clients (OIDC) --------------------------------------------------------

// Clients wraps the OIDC client-administration endpoints.
type Clients struct{ h *Handle }

func (c *Clients) List(ctx context.Context, filter models.OIDCClientFilter) ([]models.OIDCClient, error) {
	return iamclients.List(ctx, c.h.executor, c.h.iamAdminURL, filter)
}
func (c *Clients) Get(ctx context.Context, clientUUID string) (*models.OIDCClient, error) {
	return iamclients.Get(ctx, c.h.executor, c.h.iamAdminURL, clientUUID)
}
func (c *Clients) Create(ctx context.Context, in *models.OIDCClient) error {
	return iamclients.Create(ctx, c.h.executor, c.h.iamAdminURL, in)
}
func (c *Clients) Update(ctx context.Context, in *models.OIDCClient) error {
	return iamclients.Update(ctx, c.h.executor, c.h.iamAdminURL, in)
}
func (c *Clients) Delete(ctx context.Context, clientUUID string) error {
	return iamclients.Delete(ctx, c.h.executor, c.h.iamAdminURL, clientUUID)
}
func (c *Clients) GetSecret(ctx context.Context, clientUUID string) (*models.ClientSecretResponse, error) {
	return iamclients.GetSecret(ctx, c.h.executor, c.h.iamAdminURL, clientUUID)
}
func (c *Clients) RegenerateSecret(ctx context.Context, clientUUID string) (*models.ClientSecretResponse, error) {
	return iamclients.RegenerateSecret(ctx, c.h.executor, c.h.iamAdminURL, clientUUID)
}
func (c *Clients) AddDefaultScope(ctx context.Context, clientUUID, scopeID string) error {
	return iamclients.AddDefaultScope(ctx, c.h.executor, c.h.iamAdminURL, clientUUID, scopeID)
}
func (c *Clients) RemoveDefaultScope(ctx context.Context, clientUUID, scopeID string) error {
	return iamclients.RemoveDefaultScope(ctx, c.h.executor, c.h.iamAdminURL, clientUUID, scopeID)
}
func (c *Clients) GetServiceAccountUser(ctx context.Context, clientUUID string) (*models.User, error) {
	return iamclients.GetServiceAccountUser(ctx, c.h.executor, c.h.iamAdminURL, clientUUID)
}
func (c *Clients) ListScopes(ctx context.Context) ([]models.OIDCClientScope, error) {
	return iamclients.ListScopes(ctx, c.h.executor, c.h.iamAdminURL)
}

// ----- Analytics -------------------------------------------------------------

// Analytics wraps the analytics KPI endpoints.
type Analytics struct{ h *Handle }

func (a *Analytics) Fetch(ctx context.Context, kpi string, limit uint64, offset *uint64, filter models.AnalyticsFilter, out any) error {
	return analytics.Fetch(ctx, a.h.executor, a.h.rootURL, kpi, limit, offset, filter, out)
}
func (a *Analytics) VulnerabilitiesBySeverityTotal(ctx context.Context, filter models.AnalyticsFilter) (*models.AnalyticsDistributionStats, error) {
	return analytics.GetVulnerabilitiesBySeverityTotal(ctx, a.h.executor, a.h.rootURL, filter)
}
func (a *Analytics) VulnerabilitiesByStateTotal(ctx context.Context, filter models.AnalyticsFilter) (*models.AnalyticsDistributionStats, error) {
	return analytics.GetVulnerabilitiesByStateTotal(ctx, a.h.executor, a.h.rootURL, filter)
}
func (a *Analytics) VulnerabilitiesByStatusTotal(ctx context.Context, filter models.AnalyticsFilter) (*models.AnalyticsDistributionStats, error) {
	return analytics.GetVulnerabilitiesByStatusTotal(ctx, a.h.executor, a.h.rootURL, filter)
}
func (a *Analytics) VulnerabilitiesBySeverityAndStateTotal(ctx context.Context, filter models.AnalyticsFilter) ([]models.AnalyticsSeverityAndStateEntry, error) {
	return analytics.GetVulnerabilitiesBySeverityAndStateTotal(ctx, a.h.executor, a.h.rootURL, filter)
}
func (a *Analytics) VulnerabilitiesByAgingTotal(ctx context.Context, filter models.AnalyticsFilter) ([]models.AnalyticsAgingEntry, error) {
	return analytics.GetVulnerabilitiesByAgingTotal(ctx, a.h.executor, a.h.rootURL, filter)
}
func (a *Analytics) VulnerabilitiesBySeverityOvertime(ctx context.Context, filter models.AnalyticsFilter) ([]models.AnalyticsOverTimeStats, error) {
	return analytics.GetVulnerabilitiesBySeverityOvertime(ctx, a.h.executor, a.h.rootURL, filter)
}
func (a *Analytics) FixedVulnerabilitiesBySeverityOvertime(ctx context.Context, filter models.AnalyticsFilter) ([]models.AnalyticsOverTimeStats, error) {
	return analytics.GetFixedVulnerabilitiesBySeverityOvertime(ctx, a.h.executor, a.h.rootURL, filter)
}
func (a *Analytics) MeanTimeToResolution(ctx context.Context, filter models.AnalyticsFilter) (*models.AnalyticsMeanTimeStats, error) {
	return analytics.GetMeanTimeToResolution(ctx, a.h.executor, a.h.rootURL, filter)
}
func (a *Analytics) MostCommonVulnerabilities(ctx context.Context, limit uint64, filter models.AnalyticsFilter) ([]models.AnalyticsVulnerabilityStats, error) {
	return analytics.GetMostCommonVulnerabilities(ctx, a.h.executor, a.h.rootURL, limit, filter)
}
func (a *Analytics) MostAgingVulnerabilities(ctx context.Context, limit uint64, filter models.AnalyticsFilter) ([]models.AnalyticsVulnerabilityStats, error) {
	return analytics.GetMostAgingVulnerabilities(ctx, a.h.executor, a.h.rootURL, limit, filter)
}
func (a *Analytics) AllVulnerabilities(ctx context.Context, limit, offset uint64, filter models.AnalyticsFilter) ([]models.AnalyticsVulnerabilityStats, error) {
	return analytics.GetAllVulnerabilities(ctx, a.h.executor, a.h.rootURL, limit, offset, filter)
}
func (a *Analytics) IDETotal(ctx context.Context) ([]models.AnalyticsIDEStatEntry, error) {
	return analytics.GetIDETotal(ctx, a.h.executor, a.h.rootURL)
}
func (a *Analytics) IDEOverTime(ctx context.Context) ([]models.AnalyticsIDEOverTimeDistribution, error) {
	return analytics.GetIDEOverTime(ctx, a.h.executor, a.h.rootURL)
}

// ----- Audit -----------------------------------------------------------------

// Audit wraps the query-editor session endpoints.
type Audit struct{ h *Handle }

func (a *Audit) CreateSession(ctx context.Context, req *models.AuditCreateRequest) (*models.AuditSession, error) {
	return audit.CreateSession(ctx, a.h.executor, a.h.rootURL, req)
}
func (a *Audit) DeleteSession(ctx context.Context, sessionID string) error {
	return audit.DeleteSession(ctx, a.h.executor, a.h.rootURL, sessionID)
}
func (a *Audit) KeepAlive(ctx context.Context, sessionID string) error {
	return audit.KeepAlive(ctx, a.h.executor, a.h.rootURL, sessionID)
}
func (a *Audit) GetRequestStatus(ctx context.Context, sessionID, requestID string) (*models.AuditRequestStatus, error) {
	return audit.GetRequestStatus(ctx, a.h.executor, a.h.rootURL, sessionID, requestID)
}
func (a *Audit) GetScanSources(ctx context.Context, sessionID string) ([]models.AuditScanSourceFile, error) {
	return audit.GetScanSources(ctx, a.h.executor, a.h.rootURL, sessionID)
}
func (a *Audit) RunScan(ctx context.Context, sessionID string) error {
	return audit.RunScan(ctx, a.h.executor, a.h.rootURL, sessionID)
}

// ----- Migration -------------------------------------------------------------

// Migration wraps the data-import (migration) endpoints.
type Migration struct{ h *Handle }

func (m *Migration) StartImport(ctx context.Context, fileName, mappingFileName, encryptionKey string) (string, error) {
	return migration.StartImport(ctx, m.h.executor, m.h.rootURL, fileName, mappingFileName, encryptionKey)
}
func (m *Migration) List(ctx context.Context) ([]models.DataImport, error) {
	return migration.List(ctx, m.h.executor, m.h.rootURL)
}
func (m *Migration) Get(ctx context.Context, importID string) (*models.DataImport, error) {
	return migration.Get(ctx, m.h.executor, m.h.rootURL, importID)
}
func (m *Migration) GetLogs(ctx context.Context, importID string) ([]byte, error) {
	return migration.GetLogs(ctx, m.h.executor, m.h.httpClient, m.h.rootURL, importID)
}

// ----- SCM -------------------------------------------------------------------

// SCM wraps the repos-manager (source control management) endpoints.
type SCM struct{ h *Handle }

func (s *SCM) ListIntegrations(ctx context.Context) ([]models.SCMIntegration, error) {
	return scm.ListIntegrations(ctx, s.h.executor, s.h.rootURL)
}
func (s *SCM) GetRepository(ctx context.Context, repositoryID uint64) (*models.SCMRepository, error) {
	return scm.GetRepository(ctx, s.h.executor, s.h.rootURL, repositoryID)
}
func (s *SCM) DisconnectProject(ctx context.Context, projectID string) error {
	return scm.DisconnectProject(ctx, s.h.executor, s.h.rootURL, projectID)
}
// ----- PresetManager ---------------------------------------------------------

// PresetManager wraps the preset-manager endpoints for SAST/IAC preset CRUD
// and query-family browsing.
type PresetManager struct{ h *Handle }

func (pm *PresetManager) ListPresets(ctx context.Context, engine string, limit int) (*models.PresetListResponse, error) {
	return presetmanager.ListPresets(ctx, pm.h.executor, pm.h.rootURL, engine, limit)
}
func (pm *PresetManager) GetPreset(ctx context.Context, engine, presetID string) (*models.PresetManagerEntry, error) {
	return presetmanager.GetPreset(ctx, pm.h.executor, pm.h.rootURL, engine, presetID)
}
func (pm *PresetManager) SearchPreset(ctx context.Context, engine, name string, exactMatch bool) (*models.PresetListResponse, error) {
	return presetmanager.SearchPreset(ctx, pm.h.executor, pm.h.rootURL, engine, name, exactMatch)
}
func (pm *PresetManager) CreatePreset(ctx context.Context, engine string, in *models.PresetCreateRequest) (*models.PresetManagerEntry, error) {
	return presetmanager.CreatePreset(ctx, pm.h.executor, pm.h.rootURL, engine, in)
}
func (pm *PresetManager) UpdatePreset(ctx context.Context, engine, presetID string, in *models.PresetCreateRequest) error {
	return presetmanager.UpdatePreset(ctx, pm.h.executor, pm.h.rootURL, engine, presetID, in)
}
func (pm *PresetManager) DeletePreset(ctx context.Context, engine, presetID string) error {
	return presetmanager.DeletePreset(ctx, pm.h.executor, pm.h.rootURL, engine, presetID)
}
func (pm *PresetManager) ListQueryFamilies(ctx context.Context, engine string) ([]string, error) {
	return presetmanager.ListQueryFamilies(ctx, pm.h.executor, pm.h.rootURL, engine)
}
func (pm *PresetManager) GetSASTQueryFamilyContents(ctx context.Context, family string) (*models.SASTQueryCollection, error) {
	return presetmanager.GetSASTQueryFamilyContents(ctx, pm.h.executor, pm.h.rootURL, family)
}
func (pm *PresetManager) GetIACQueryFamilyContents(ctx context.Context, family string) (*models.IACQueryCollection, error) {
	return presetmanager.GetIACQueryFamilyContents(ctx, pm.h.executor, pm.h.rootURL, family)
}

// ----- Audit query editor extensions -----------------------------------------

func (a *Audit) GetSASTQueries(ctx context.Context, sessionID, level, levelID string) (*models.SASTQueryCollection, error) {
	return audit.GetSASTQueries(ctx, a.h.executor, a.h.rootURL, sessionID, level, levelID)
}
func (a *Audit) GetIACQueries(ctx context.Context, sessionID, level, levelID string) (*models.IACQueryCollection, error) {
	return audit.GetIACQueries(ctx, a.h.executor, a.h.rootURL, sessionID, level, levelID)
}
func (a *Audit) GetQueryTree(ctx context.Context, sessionID, level, levelID string) ([]models.AuditQueryTree, error) {
	return audit.GetQueryTree(ctx, a.h.executor, a.h.rootURL, sessionID, level, levelID)
}
func (a *Audit) DeleteQueryOverride(ctx context.Context, sessionID, queryKey string) error {
	return audit.DeleteQueryOverride(ctx, a.h.executor, a.h.rootURL, sessionID, queryKey)
}
func (a *Audit) CreateQueryOverride(ctx context.Context, sessionID string, body any) error {
	return audit.CreateQueryOverride(ctx, a.h.executor, a.h.rootURL, sessionID, body)
}
func (a *Audit) UpdateQuerySource(ctx context.Context, sessionID, queryKey string, body any) ([]models.QueryFailure, error) {
	return audit.UpdateQuerySource(ctx, a.h.executor, a.h.rootURL, sessionID, queryKey, body)
}
func (a *Audit) UpdateQueryMetadata(ctx context.Context, sessionID, queryKey string, body any) error {
	return audit.UpdateQueryMetadata(ctx, a.h.executor, a.h.rootURL, sessionID, queryKey, body)
}
func (a *Audit) ValidateQuerySource(ctx context.Context, sessionID string, body any) ([]models.QueryFailure, error) {
	return audit.ValidateQuerySource(ctx, a.h.executor, a.h.rootURL, sessionID, body)
}
func (a *Audit) RunQuery(ctx context.Context, sessionID string, body any) (*models.QueryFailure, error) {
	return audit.RunQuery(ctx, a.h.executor, a.h.rootURL, sessionID, body)
}

// ----- Scans metadata extensions ---------------------------------------------

func (s *Scans) GetConfiguration(ctx context.Context, projectID, scanID string) ([]models.ProjectConfiguration, error) {
	return scans.GetConfiguration(ctx, s.h.executor, s.h.rootURL, projectID, scanID)
}
func (s *Scans) GetSummary(ctx context.Context, query url.Values) ([]models.ScanSummary, error) {
	return scans.GetSummary(ctx, s.h.executor, s.h.rootURL, query)
}
func (s *Scans) GetSASTAggregate(ctx context.Context, query url.Values) ([]models.SASTAggregateSummary, error) {
	return scans.GetSASTAggregate(ctx, s.h.executor, s.h.rootURL, query)
}

// ----- Access advanced extensions --------------------------------------------

func (a *Access) GetMyGroups(ctx context.Context, search string, subgroups bool, limit, offset int) ([]models.Group, error) {
	return access.GetMyGroups(ctx, a.h.executor, a.h.rootURL, search, subgroups, limit, offset)
}
func (a *Access) GetAvailableGroups(ctx context.Context, projectID, search string, limit, offset int) ([]models.Group, error) {
	return access.GetAvailableGroups(ctx, a.h.executor, a.h.rootURL, projectID, search, limit, offset)
}
func (a *Access) ListAMGroups(ctx context.Context, search string, limit, offset int) ([]models.Group, error) {
	return access.ListAMGroups(ctx, a.h.executor, a.h.rootURL, search, limit, offset)
}
func (a *Access) ListAMUsers(ctx context.Context, search string, limit, offset int) ([]models.User, error) {
	return access.ListAMUsers(ctx, a.h.executor, a.h.rootURL, search, limit, offset)
}
func (a *Access) ListAMClients(ctx context.Context, search string, limit, offset int) ([]models.OIDCClient, error) {
	return access.ListAMClients(ctx, a.h.executor, a.h.rootURL, search, limit, offset)
}
func (a *Access) ListAMApplications(ctx context.Context, search string, limit, offset int) ([]models.Application, error) {
	return access.ListAMApplications(ctx, a.h.executor, a.h.rootURL, search, limit, offset)
}
func (a *Access) ListAMProjects(ctx context.Context, search string, limit, offset int) ([]models.ProjectResponseModel, error) {
	return access.ListAMProjects(ctx, a.h.executor, a.h.rootURL, search, limit, offset)
}
func (a *Access) ListPermissions(ctx context.Context) ([]models.AMPermission, error) {
	return access.ListPermissions(ctx, a.h.executor, a.h.rootURL)
}
func (a *Access) ListAMRoles(ctx context.Context) ([]models.AMRole, error) {
	return access.ListAMRoles(ctx, a.h.executor, a.h.rootURL)
}

// ----- Projects MoveApplications extension -----------------------------------

func (p *Projects) MoveApplications(ctx context.Context, projectID string, fromAppIDs, toAppIDs []string) error {
	return projects.MoveApplications(ctx, p.h.executor, p.h.rootURL, projectID, fromAppIDs, toAppIDs)
}
