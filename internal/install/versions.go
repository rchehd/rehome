package install

import (
	"context"
	"slices"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
)

// withVersions decides the version of a step: what to pin for a missing app, and whether a
// present app is older than the lock (an optional upgrade; never a downgrade).
func withVersions(ctx context.Context, r *runner.Runner, s Step, lk *lock.Lock, platformKey string, sel Selection) Step {
	if s.Adapter == nil {
		return s
	}

	target, ok := targetVersion(s, lk, platformKey)

	if !ok {
		return s
	}

	switch s.Status {
	case StatusInstall:
		if s.App.Version != "" || (sel.Mode == ModeLocked && !slices.Contains(sel.Latest, s.App.ID)) {
			s.Pin = target
		}
	case StatusInstalled:
		s.Installed, s.Upgrade = upgradeFor(ctx, r, s, target)
	}

	return s
}

// targetVersion is the permanent `version:` pin, else the lock entry for this platform.
func targetVersion(s Step, lk *lock.Lock, platformKey string) (lock.Entry, bool) {
	if s.App.Version != "" {
		return lock.Entry{Manager: s.Adapter.Name(), Version: s.App.Version}, true
	}

	e, ok := lk.Get(s.App.ID, platformKey)

	if !ok || e.Unsupported || (e.Version == "" && e.Commit == "") || e.Manager != s.Adapter.Name() {
		return lock.Entry{}, false
	}

	return e, true
}

func upgradeFor(ctx context.Context, r *runner.Runner, s Step, target lock.Entry) (string, lock.Entry) {
	v, ok := s.Adapter.(adapter.Versioner)

	if !ok || target.Version == "" {
		return "", lock.Entry{}
	}

	cur, err := v.Version(ctx, r, s.Spec)

	if err != nil || cur.Version == "" {
		return "", lock.Entry{}
	}

	if lock.Compare(target.Manager, cur.Version, target.Version) < 0 {
		return cur.Version, target
	}

	return cur.Version, lock.Entry{}
}
