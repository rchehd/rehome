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

// Versions records, for every app of the merged config, what this machine has: the installed
// version for apps found here, or unsupported when the config has no installer for this OS.
func Versions(ctx context.Context, r *runner.Runner, cfg *config.Config, found map[string]bool, p platform.Platform) map[string]lock.Entry {
	entries := map[string]lock.Entry{}

	for _, app := range cfg.Apps {
		if cfg.Ignored(app, p.Key(), config.ThisNode()) {
			continue
		}

		spec, ok := app.Spec(p.Key())

		if !ok {
			if !p.WSL && len(app.Install) > 0 {
				entries[app.ID] = lock.Entry{Unsupported: true}
			}

			continue
		}

		if !found[app.ID] {
			continue
		}

		if e, ok := version(ctx, r, spec); ok {
			entries[app.ID] = e
		}
	}

	return entries
}

func version(ctx context.Context, r *runner.Runner, spec config.InstallSpec) (lock.Entry, bool) {
	a, err := all.ByManager(spec)

	if err != nil {
		return lock.Entry{}, false
	}

	v, ok := a.(adapter.Versioner)

	if !ok {
		return lock.Entry{}, false
	}

	e, err := v.Version(ctx, r, spec)

	if err != nil || (e.Version == "" && e.Commit == "") {
		return lock.Entry{}, false
	}

	e.Manager = a.Name()

	return e, true
}
