package install

import (
	"fmt"
	"slices"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/lock"
)

// install puts the pinned version in place when there is one; a version the repository
// no longer has, or a manager that cannot pin, falls back to the latest with a note.
func (x *executor) install(s Step) error {
	if s.Pin == (lock.Entry{}) {
		return s.Adapter.Install(x.ctx, x.r, s.Spec)
	}

	p, ok := s.Adapter.(adapter.Pinner)

	if !ok {
		x.versionNote("%s: %s cannot install a fixed version, installed the latest", s.App.ID, s.Adapter.Name())

		return s.Adapter.Install(x.ctx, x.r, s.Spec)
	}

	if err := p.InstallVersion(x.ctx, x.r, s.Spec, s.Pin); err == nil {
		return nil
	}

	x.unbreakApt()
	x.versionNote("%s: %s is not available, installed the latest", s.App.ID, pinLabel(s.Pin))

	return s.Adapter.Install(x.ctx, x.r, s.Spec)
}

// upgrade raises installed apps older than the lock only when asked; otherwise it reports them.
func (x *executor) upgrade() {
	for _, s := range x.plan.Steps {
		if s.Upgrade == (lock.Entry{}) || x.state[s.App.ID] != "installed" {
			continue
		}

		o := Outcome{ID: s.App.ID, Reason: s.Installed + " → " + pinLabel(s.Upgrade)}

		if !x.opts.Upgrade && !slices.Contains(x.opts.UpgradeIDs, s.App.ID) {
			x.report.Upgradable = append(x.report.Upgradable, o)

			continue
		}

		p, ok := s.Adapter.(adapter.Pinner)

		if !ok {
			continue
		}

		x.progress(Event{ID: s.App.ID, Phase: "upgrade"})

		if err := p.InstallVersion(x.ctx, x.r, s.Spec, s.Upgrade); err != nil {
			x.unbreakApt()
			x.versionNote("%s: upgrade to %s failed: %v", s.App.ID, pinLabel(s.Upgrade), err)

			continue
		}

		x.report.Upgraded = append(x.report.Upgraded, o)
	}
}

func (x *executor) versionNote(format string, args ...any) {
	x.report.VersionNotes = append(x.report.VersionNotes, fmt.Sprintf(format, args...))
}

func pinLabel(e lock.Entry) string {
	if e.Commit != "" {
		return "commit " + e.Commit[:min(12, len(e.Commit))]
	}

	return e.Version
}

// recordVersions updates this machine's local lock with what is installed after the run.
func (x *executor) recordVersions() {
	if x.r.DryRun {
		return
	}

	path, err := lock.LocalPath()

	if err != nil {
		return
	}

	local, err := lock.Load(path)

	if err != nil {
		x.note("local lock: %v", err)

		return
	}

	for _, s := range x.plan.Steps {
		st := x.state[s.App.ID]

		if (st != "done" && st != "installed") || s.Adapter == nil {
			continue
		}

		v, ok := s.Adapter.(adapter.Versioner)

		if !ok {
			continue
		}

		if e, err := v.Version(x.ctx, x.r, s.Spec); err == nil && (e.Version != "" || e.Commit != "") {
			e.Manager = s.Adapter.Name()
			local.Set(s.App.ID, x.plan.Platform.Key(), e)
		}
	}

	if err := local.Save(path); err != nil {
		x.note("local lock: %v", err)
	}
}
