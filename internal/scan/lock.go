package scan

import (
	"context"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/all"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

// Versions records every app of the merged config found here with its manager and, where it
// can be read, the installed version.
func Versions(ctx context.Context, r *runner.Runner, cfg *config.Config, found map[string]bool, p platform.Platform) map[string]lock.Entry {
	entries := map[string]lock.Entry{}
	batches := map[string]map[string]lock.Entry{}

	for _, app := range cfg.Apps {
		if cfg.Ignored(app, p.Key(), config.ThisNode()) {
			continue
		}

		spec, ok := app.Spec(p.Key())

		if !ok || !found[app.ID] {
			continue
		}

		if e, ok := version(ctx, r, spec, batches); ok {
			entries[app.ID] = e
		}
	}

	return entries
}

// version reads spec's installed version; batches holds each batch-capable manager's one read.
func version(ctx context.Context, r *runner.Runner, spec config.InstallSpec, batches map[string]map[string]lock.Entry) (lock.Entry, bool) {
	a, err := all.ByManager(spec)

	if err != nil {
		return lock.Entry{}, false
	}

	e := lock.Entry{}

	if b, ok := a.(adapter.BatchVersioner); ok {
		got, read := batches[a.Name()]

		if !read {
			got = b.Versions(ctx, r)
			batches[a.Name()] = got
		}

		if got != nil {
			_, pkg := spec.Manager()
			e = got[pkg]
			e.Manager = a.Name()

			return e, true
		}
	}

	if v, ok := a.(adapter.Versioner); ok {
		if got, err := v.Version(ctx, r, spec); err == nil {
			e = got
		}
	}

	e.Manager = a.Name()

	return e, true
}
