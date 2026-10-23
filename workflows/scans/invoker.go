package scans

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/scans"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/uploads"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/projects"
)

// Invoker is the fluent builder for starting a new Checkmarx One scan.
//
// Construct one with [NewInvoker]; chain configuration setters; call
// [Invoker.Start] to send the scan request.
//
// CLAUDE.md §9.2 mandates fluent builders with up-front validation for
// operations with many optional inputs. The Invoker validates engine names,
// branch presence, and source-mode consistency before issuing any HTTP
// request — invalid combinations surface as *cxerrors.ConfigurationError
// from Start, not from a remote 400.
//
// Two source modes are supported:
//
//   - Git mode: ForBranch sets a branch on the project's primary repo URL.
//     The platform checks out the source via its own SCM integration.
//   - Upload mode: WithUploadReader uploads a zip via the two-step
//     presigned-URL dance and references the URL in the scan request.
//
// Use exactly one source mode per Invoker. Mixing them surfaces a
// ConfigurationError on Start.
type Invoker struct {
	backend *Backend
	repo    *projects.ProjectRepoConfig

	branch       string
	tag          string
	commit       string
	engines      []string
	tags         map[string]string
	projectTags  map[string]string // nil unless WithProjectTags (overwrites project tags server-side)
	extraConfigs []models.Config   // WithConfigs passthrough
	sastConfig   *models.SastConfig
	scaConfig    *models.ScaConfig
	kicsConfig   *models.KicsConfig
	containerCfg *models.ContainerConfig
	apisecCfg    *models.APISecConfig
	scsCfg       *models.SCSConfig

	// upload-mode state
	uploadBody    io.Reader
	uploadSize    int64
	uploadFormat  string
	uploadTimeout time.Duration // 0 = use the HTTP client's own timeout

	// httpClient is used for the upload-mode PUT (which bypasses the funnel).
	httpClient *http.Client

	// configs is the validated config[] array assembled by validate().
	configs []models.Config

	// validation errors collected during the chain — surfaced from Start.
	configErr error
}

// NewInvoker constructs a fluent scan builder bound to the supplied
// project. The httpClient is used for the upload-mode presigned-URL PUT
// path; pass the same client the cxone client is using for shared connection
// pools.
func NewInvoker(backend *Backend, repo *projects.ProjectRepoConfig, httpClient *http.Client) *Invoker {
	if backend == nil || backend.Executor == nil {
		return &Invoker{configErr: &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}}
	}
	if repo == nil {
		return &Invoker{configErr: &cxerrors.ConfigurationError{Field: "repo", Reason: "is required"}}
	}
	return &Invoker{
		backend:    backend,
		repo:       repo,
		httpClient: httpClient,
		tags:       map[string]string{},
	}
}

// ForBranch selects the git branch to scan in git mode. Mutually exclusive
// with [Invoker.WithUploadReader].
func (i *Invoker) ForBranch(branch string) *Invoker {
	i.branch = branch
	return i
}

// ForCommit narrows the git-mode scan to a specific commit. Optional.
func (i *Invoker) ForCommit(commit string) *Invoker {
	i.commit = commit
	return i
}

// ForTag narrows the git-mode scan to a specific git tag. Optional.
func (i *Invoker) ForTag(tag string) *Invoker {
	i.tag = tag
	return i
}

// WithEngines selects which Checkmarx One engines to run.
//
// Valid names (the spec's config-type enum): "sast", "sca", "kics",
// "apisec", "containers", "microengines", "aisc". "scs" is a deprecated alias
// for "microengines". Unknown or duplicate names (after aliasing) cause Start
// to return a *cxerrors.ConfigurationError.
func (i *Invoker) WithEngines(engines ...string) *Invoker {
	i.engines = append([]string(nil), engines...)
	return i
}

// WithConfigs appends raw config[] entries to the scan request verbatim
// (a passthrough of the spec's config array). Types must be in the spec enum
// (see [Invoker.WithEngines]), values must be strings, and a type may appear
// only once across WithEngines and WithConfigs; violations surface from Start
// as a *cxerrors.ConfigurationError before any HTTP call. Multiple calls
// accumulate. WithConfigs alone satisfies the "at least one engine" rule.
func (i *Invoker) WithConfigs(cfgs ...models.Config) *Invoker {
	i.extraConfigs = append(i.extraConfigs, cfgs...)
	return i
}

// WithProjectTags sets project.tags in the scan request. NOTE: per the spec
// this OVERWRITES the project's tags. Without this call project.tags is
// omitted entirely. Multiple calls accumulate.
func (i *Invoker) WithProjectTags(tags map[string]string) *Invoker {
	if i.projectTags == nil {
		i.projectTags = map[string]string{}
	}
	for k, v := range tags {
		i.projectTags[k] = v
	}
	return i
}

// WithUploadTimeout gives the upload-mode presigned-URL PUT its own
// deadline d, which is not capped by the HTTP client's Timeout. Without it
// the client's timeout applies to the PUT (or a two-hour ceiling if the
// client has none). d must be > 0.
func (i *Invoker) WithUploadTimeout(d time.Duration) *Invoker {
	if d <= 0 && i.configErr == nil {
		i.configErr = &cxerrors.ConfigurationError{Field: "UploadTimeout", Reason: "must be > 0"}
	}
	i.uploadTimeout = d
	return i
}

// WithUploadFormat sets handler.uploadFormat for upload scans (the spec
// uses "single" for an SBOM upload).
func (i *Invoker) WithUploadFormat(format string) *Invoker {
	i.uploadFormat = format
	return i
}

// WithTags adds key/value tags to the scan itself (the request's top-level
// "tags"). Multiple calls accumulate.
//
// Behaviour change: WithTags no longer sets project.tags. Earlier versions
// sent these tags as the project's tags, which overwrites the project's
// existing tags server-side. To set project tags deliberately use
// [Invoker.WithProjectTags].
func (i *Invoker) WithTags(tags map[string]string) *Invoker {
	for k, v := range tags {
		i.tags[k] = v
	}
	return i
}

// WithSastConfig overrides the SAST engine configuration for this scan
// (preset name, fast-scan mode, light queries, etc.). The platform's
// project-level configuration is used for any field left unset.
func (i *Invoker) WithSastConfig(c models.SastConfig) *Invoker {
	i.sastConfig = &c
	return i
}

// WithScaConfig overrides the SCA engine configuration.
func (i *Invoker) WithScaConfig(c models.ScaConfig) *Invoker {
	i.scaConfig = &c
	return i
}

// WithKicsConfig overrides the KICS (IaC) engine configuration.
func (i *Invoker) WithKicsConfig(c models.KicsConfig) *Invoker {
	i.kicsConfig = &c
	return i
}

// WithContainerConfig overrides the container-scanning engine configuration.
func (i *Invoker) WithContainerConfig(c models.ContainerConfig) *Invoker {
	i.containerCfg = &c
	return i
}

// WithAPISecConfig overrides the API security engine configuration.
func (i *Invoker) WithAPISecConfig(c models.APISecConfig) *Invoker {
	i.apisecCfg = &c
	return i
}

// WithSCSConfig overrides the source-code security engine configuration.
func (i *Invoker) WithSCSConfig(c models.SCSConfig) *Invoker {
	i.scsCfg = &c
	return i
}

// WithUploadReader switches the invoker to upload mode. body is streamed
// to the platform's object store via the two-step presigned-URL dance;
// size is the byte length of the body and MUST be accurate (object stores
// require Content-Length).
//
// Mutually exclusive with [Invoker.ForBranch] / [Invoker.ForCommit] /
// [Invoker.ForTag].
func (i *Invoker) WithUploadReader(body io.Reader, size int64) *Invoker {
	i.uploadBody = body
	i.uploadSize = size
	return i
}

// Start validates the builder, optionally uploads the source archive, and
// issues POST /api/scans. Returns a [*ScanInspector] populated with the
// platform's response so the caller can poll for completion or pass it to
// [WaitUntilComplete] directly.
//
// Validation errors surface as *cxerrors.ConfigurationError. Upload errors
// (when in upload mode) and HTTP errors are returned verbatim from the
// underlying endpoint helpers.
func (i *Invoker) Start(ctx context.Context) (*ScanInspector, error) {
	if i.configErr != nil {
		return nil, i.configErr
	}
	if err := i.validate(); err != nil {
		return nil, err
	}

	// Build handler payload depending on source mode.
	var (
		handlerJSON json.RawMessage
		err         error
	)
	if i.uploadBody != nil {
		handlerJSON, err = i.buildUploadHandler(ctx)
	} else {
		handlerJSON, err = i.buildGitHandler()
	}
	if err != nil {
		return nil, err
	}

	scanReq := &models.Scan{
		Type:    i.sourceType(),
		Handler: handlerJSON,
		Project: models.ScanProject{ID: i.repo.ID(), Tags: i.projectTags},
		Tags:    i.tags,
		Config:  i.configs,
	}

	resp, err := endpoints.Create(ctx, i.backend.Executor, i.backend.BaseURL, scanReq)
	if err != nil {
		return nil, err
	}
	return FromScanModel(i.backend, resp)
}

// validate runs up-front sanity checks. CLAUDE.md §9.2 — invalid combinations
// must surface as *cxerrors.ConfigurationError BEFORE any HTTP call.
func (i *Invoker) validate() error {
	cfgs, err := i.assembleConfigs()
	if err != nil {
		return err
	}
	i.configs = cfgs
	gitMode := i.branch != "" || i.commit != "" || i.tag != ""
	uploadMode := i.uploadBody != nil
	if gitMode && uploadMode {
		return &cxerrors.ConfigurationError{Field: "Source", Reason: "git mode (ForBranch/ForCommit/ForTag) and upload mode (WithUploadReader) are mutually exclusive"}
	}
	if !gitMode && !uploadMode {
		// Fall back to the project's primary branch when nothing is set.
		if i.repo.MainBranch() == "" {
			return &cxerrors.ConfigurationError{Field: "Branch", Reason: "no branch supplied and project has no MainBranch set"}
		}
		i.branch = i.repo.MainBranch()
	}
	if uploadMode && i.uploadSize <= 0 {
		return &cxerrors.ConfigurationError{Field: "UploadSize", Reason: "must be > 0 (object stores require Content-Length)"}
	}
	if uploadMode && i.httpClient == nil {
		return &cxerrors.ConfigurationError{Field: "httpClient", Reason: "is required for upload mode"}
	}
	return nil
}

func (i *Invoker) sourceType() string {
	if i.uploadBody != nil {
		return "upload"
	}
	return "git"
}

func (i *Invoker) buildGitHandler() (json.RawMessage, error) {
	repoURL := i.repo.RepoURL()
	if repoURL == "" {
		return nil, &cxerrors.ConfigurationError{Field: "repoUrl", Reason: "project has no RepoURL set"}
	}
	h := models.GitProjectHandler{
		RepoURL: repoURL,
		Branch:  i.branch,
		Commit:  i.commit,
		Tag:     i.tag,
	}
	return json.Marshal(h)
}

func (i *Invoker) buildUploadHandler(ctx context.Context) (json.RawMessage, error) {
	// Step 1: get a presigned URL.
	uploadURL, err := uploads.GetPresignedURL(ctx, i.backend.Executor, i.backend.BaseURL)
	if err != nil {
		return nil, err
	}
	// Step 2: PUT the body. This bypasses the funnel; the bearer token is
	// attached only when the presigned URL is on the API origin itself
	// (storage fronted by the API host), never for an external object store.
	pa := &uploads.Auth{Authenticator: i.backend.Executor.Authenticator(), APIBaseURL: i.backend.BaseURL}
	var putErr error
	if i.uploadTimeout > 0 {
		putErr = uploads.PutFileAuthWithTimeout(ctx, i.httpClient, uploadURL, i.uploadBody, i.uploadSize, pa, i.uploadTimeout)
	} else {
		putErr = uploads.PutFileAuth(ctx, i.httpClient, uploadURL, i.uploadBody, i.uploadSize, pa)
	}
	if putErr != nil {
		return nil, putErr
	}
	h := models.ScanHandler{
		RepoURL:      i.repo.RepoURL(),
		UploadURL:    uploadURL,
		UploadFormat: i.uploadFormat,
	}
	return json.Marshal(h)
}

// specConfigTypes is the config[].type enum from Scans_copy.yaml.
var specConfigTypes = map[string]bool{
	"sast": true, "sca": true, "kics": true, "apisec": true,
	"containers": true, "microengines": true, "aisc": true,
}

// normalizeEngine maps deprecated aliases to spec config types.
func normalizeEngine(n string) string {
	if n == "scs" {
		return "microengines"
	}
	return n
}

// assembleConfigs builds the final config[] array from WithEngines (typed
// per-engine configs) plus WithConfigs passthrough entries and validates it:
// at least one entry, types in the spec enum, no duplicate types, all values
// strings. Engines whose typed config is nil get an entry with no value so
// the platform applies project-level defaults.
func (i *Invoker) assembleConfigs() ([]models.Config, error) {
	if len(i.engines) == 0 && len(i.extraConfigs) == 0 {
		return nil, &cxerrors.ConfigurationError{Field: "Engines", Reason: "at least one engine must be selected"}
	}
	out := make([]models.Config, 0, len(i.engines)+len(i.extraConfigs))
	seen := map[string]bool{}
	add := func(field string, c models.Config) error {
		if !specConfigTypes[c.Type] {
			return &cxerrors.ConfigurationError{Field: field, Reason: "unknown config type " + quote(c.Type) + "; valid: sast, sca, kics, apisec, containers, microengines, aisc (scs is a deprecated alias for microengines)"}
		}
		if seen[c.Type] {
			return &cxerrors.ConfigurationError{Field: field, Reason: "duplicate config type " + quote(c.Type)}
		}
		seen[c.Type] = true
		for k, v := range c.Value {
			if _, ok := v.(string); !ok {
				return &cxerrors.ConfigurationError{Field: field, Reason: "config " + quote(c.Type) + " value " + quote(k) + " must be a string (the API uses stringified values)"}
			}
		}
		out = append(out, c)
		return nil
	}
	for _, eng := range i.engines {
		t := normalizeEngine(eng)
		var src any
		switch t {
		case "sast":
			src = i.sastConfig
		case "sca":
			src = i.scaConfig
		case "kics":
			src = i.kicsConfig
		case "containers":
			src = i.containerCfg
		case "apisec":
			src = i.apisecCfg
		case "microengines":
			src = i.scsCfg
		}
		if err := add("Engines", models.Config{Type: t, Value: structToMap(src)}); err != nil {
			return nil, err
		}
	}
	for _, c := range i.extraConfigs {
		if err := add("Configs", c); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func quote(s string) string { return strconv.Quote(s) }

// structToMap marshals a typed config struct into a map for the wire's
// untyped {string: interface{}} value. nil sources collapse to nil so the
// platform applies project-level defaults.
func structToMap(src any) map[string]any {
	if src == nil {
		return nil
	}
	// Defensive: if the typed pointer is the typed-nil case (e.g.
	// `(*SastConfig)(nil)`), reflect would still see a non-nil interface.
	// Marshal/Unmarshal handles both shapes uniformly.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(src); err != nil || buf.Len() <= len("null\n") {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		return nil
	}
	return out
}
