package projects

import (
	"context"
	"sync"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	endpoints "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/endpoints/projects"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// CreateOpts configures [GetOrCreateForRepo].
type CreateOpts struct {
	// RepoURL is the repository URL recorded on the project.
	RepoURL string
	// Groups are group IDs (not names) to assign on creation. Resolve names
	// with the groups workflow (Client.Groups().GetOrCreateByName) first.
	// Groups are applied only when the project is created; an existing
	// project's groups are never changed.
	Groups []string
	// Tags are key/value tags. On an existing project missing tags are
	// added and differing values overwritten; other existing tags are kept.
	Tags map[string]string
	// Origin is the project origin (e.g. "scm-broker"). Create-only.
	Origin string
}

// repoLocks serializes GetOrCreateForRepo per (server, name) inside this
// process so concurrent callers issue a single create POST; the server-side
// conflict recovery in [GetOrCreateByName] still covers cross-process races.
var repoLocks sync.Map // key string -> *sync.Mutex

// GetOrCreateForRepo finds the project called name or creates it with the
// repository URL, origin, tags and groups from opts. For an existing project
// it also PATCHes RepoURL (when opts.RepoURL is set and differs) and merges
// opts.Tags, issuing no write when nothing differs. Safe for concurrent use.
func GetOrCreateForRepo(ctx context.Context, b *Backend, name string, opts CreateOpts) (*ProjectRepoConfig, error) {
	if b == nil || b.Executor == nil {
		return nil, &cxerrors.ConfigurationError{Field: "backend", Reason: "is required"}
	}
	if name == "" {
		return nil, &cxerrors.ConfigurationError{Field: "name", Reason: "is required"}
	}

	mu, _ := repoLocks.LoadOrStore(b.BaseURL+"\x00"+name, &sync.Mutex{})
	l := mu.(*sync.Mutex)
	l.Lock()
	defer l.Unlock()

	cfg, err := GetOrCreateByName(ctx, b, &models.Project{
		Name:    name,
		RepoURL: opts.RepoURL,
		Origin:  opts.Origin,
		Tags:    opts.Tags,
		Groups:  opts.Groups,
	})
	if err != nil {
		return nil, err
	}

	cur := cfg.project
	patch := &models.ProjectPatch{}
	changed := false
	if opts.RepoURL != "" && cur.RepoURL != opts.RepoURL {
		u := opts.RepoURL
		patch.RepoURL = &u
		changed = true
	}
	merged := make(map[string]string, len(cur.Tags)+len(opts.Tags))
	for k, v := range cur.Tags {
		merged[k] = v
	}
	tagsChanged := false
	for k, v := range opts.Tags {
		if old, ok := merged[k]; !ok || old != v {
			merged[k] = v
			tagsChanged = true
		}
	}
	if tagsChanged {
		patch.Tags = &merged
		changed = true
	}
	if !changed {
		return cfg, nil
	}
	if err := endpoints.Patch(ctx, b.Executor, b.BaseURL, cur.ID, patch); err != nil {
		return nil, err
	}
	updated := *cur
	if patch.RepoURL != nil {
		updated.RepoURL = *patch.RepoURL
	}
	if patch.Tags != nil {
		updated.Tags = merged
	}
	cfg.project = &updated
	return cfg, nil
}
