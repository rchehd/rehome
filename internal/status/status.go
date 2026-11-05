package status

import (
	"context"
	"fmt"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/discover"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/progress"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/scan"
)

type AppVersions struct {
	Installed lock.Entry `json:"installed"`
	Recorded  lock.Entry `json:"recorded"`
	Pinned    string     `json:"pinned,omitempty"`
}

type Status struct {
	Source   gitsync.SourceStatus   `json:"source_status,omitempty"`
	Versions map[string]AppVersions `json:"versions,omitempty"`
	// Missing are enabled config apps not installed here.
	Missing        []string           `json:"missing"`
	MissingReasons map[string]string  `json:"missing_reasons,omitempty"`
	Attention      []discover.Warning `json:"needs_attention,omitempty"`
	// New are apps installed here but absent from the config.
	New []string `json:"new"`
	// ChangedFiles are settings that differ between the system and files/.
	ChangedFiles []string `json:"changed_files"`
	// Behind are installed apps older than the lock ("id 1.2 → 1.3").
	Behind []string `json:"behind_lock"`
	// NotSynced are versions changed here that the next scan would record in the lock.
	NotSynced []string `json:"not_in_lock"`
	// Unsupported have no installer for this OS; informational, not a difference.
	Unsupported []string `json:"unsupported"`
}

func (s *Status) Clean() bool {
	return (s.Source == "" || s.Source == gitsync.SourceCurrent || s.Source == gitsync.SourceLocal) &&
		len(s.Missing)+len(s.New)+len(s.ChangedFiles)+len(s.Behind)+len(s.NotSynced) == 0
}

// Compute compares the config with the system in both directions without writing anything.
func Compute(ctx context.Context, r *runner.Runner, cfg *config.Config, existing []byte, root string, p platform.Platform, cat *catalog.Catalog, repo, local *lock.Lock, options scan.Options) (*Status, error) {
	progress.Step(ctx, "checking installed apps")
	plan, err := install.Build(ctx, r, cfg, root, p, install.Selection{Offline: true}, repo)

	if err != nil {
		return nil, err
	}

	st := &Status{MissingReasons: map[string]string{}}

	for _, s := range plan.Steps {
		switch {
		case s.Status == install.StatusInstall:
			st.Missing = append(st.Missing, s.App.ID)
			st.MissingReasons[s.App.ID] = missingReason(s.Spec)
		case s.Status == install.StatusUnavailable && (len(s.App.Install) > 0 || p.WSL && s.App.Files != ""):
			st.Unsupported = append(st.Unsupported, s.App.ID)
		}
	}

	res, err := scan.Run(ctx, r, p, cat, root, cfg, options)

	if err != nil {
		return nil, err
	}

	_, report, err := config.Merge(existing, res.Config, p.Key(), res.SkipMissing)

	if err != nil {
		return nil, err
	}

	st.Attention = res.Attention
	st.New = report.Added
	found := map[string]bool{}

	for _, a := range res.Config.Apps {
		found[a.ID] = true
	}

	progress.Step(ctx, "checking versions")
	current := scan.Versions(ctx, r, cfg, found, p)
	st.compareVersions(plan.Steps, current, repo, local, p.Key())

	for _, c := range res.FileChanges {
		st.ChangedFiles = append(st.ChangedFiles, c.Dst)
	}
	if options.Sessions {
		for _, step := range plan.Steps {
			if step.Status == install.StatusDisabled || step.Status == install.StatusIgnored || step.Status == install.StatusUnavailable {
				continue
			}
			var sessionMaps []config.FileMap
			for _, mapping := range cfg.Files[step.App.Files] {
				if mapping.Session != "" {
					sessionMaps = append(sessionMaps, mapping)
				}
			}
			results, err := files.Apply(ctx, root, sessionMaps, files.ApplyOptions{Sessions: true, DryRun: true, SessionPaths: options.SessionPaths})
			if err != nil {
				return nil, err
			}
			for _, result := range results {
				switch result.Status {
				case files.StatusCopied:
					st.ChangedFiles = append(st.ChangedFiles, "session missing: "+result.Dest)
				case files.StatusSessionError:
					return nil, fmt.Errorf("%s: %s", result.Src, result.Problem)
				}
			}
		}
	}

	return st, nil
}

func missingReason(spec config.InstallSpec) string {
	manager, pkg := spec.Manager()
	if manager == "script" {
		if spec.Bin != "" {
			return "script install expects " + spec.Bin
		}
		return "script install has no detection path"
	}
	return "expected " + manager + " package " + pkg
}
