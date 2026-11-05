package status

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/scan"
)

type Status struct {
	Source gitsync.SourceStatus `json:"source_status,omitempty"`
	// Missing are enabled config apps not installed here.
	Missing []string `json:"missing"`
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
	return (s.Source == "" || s.Source == gitsync.SourceCurrent) &&
		len(s.Missing)+len(s.New)+len(s.ChangedFiles)+len(s.Behind)+len(s.NotSynced) == 0
}

// Compute compares the config with the system in both directions without writing anything.
func Compute(ctx context.Context, r *runner.Runner, cfg *config.Config, existing []byte, root string, p platform.Platform, cat *catalog.Catalog, repo, local *lock.Lock, options scan.Options) (*Status, error) {
	plan, err := install.Build(ctx, r, cfg, root, p, install.Selection{Offline: true}, repo)

	if err != nil {
		return nil, err
	}

	st := &Status{}

	for _, s := range plan.Steps {
		switch {
		case s.Status == install.StatusInstall:
			st.Missing = append(st.Missing, s.App.ID)
		case s.Status == install.StatusUnavailable && (len(s.App.Install) > 0 || p.WSL && s.App.Files != ""):
			st.Unsupported = append(st.Unsupported, s.App.ID)
		case s.Upgrade != (lock.Entry{}):
			st.Behind = append(st.Behind, fmt.Sprintf("%s %s → %s", s.App.ID, s.Installed, s.Upgrade.Version))
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

	st.New = report.Added
	found := map[string]bool{}

	for _, a := range res.Config.Apps {
		found[a.ID] = true
	}

	_, st.NotSynced = lock.Sync(repo, local, scan.Versions(ctx, r, cfg, found, p), p.Key())

	for _, c := range res.FileChanges {
		st.ChangedFiles = append(st.ChangedFiles, c.Dst)
	}
	if options.Sessions {
		for _, mappings := range cfg.Files {
			var sessionMaps []config.FileMap
			for _, mapping := range mappings {
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

func (s *Status) Print(w io.Writer) {
	switch s.Source {
	case gitsync.SourceCurrent:
		fmt.Fprintln(w, "config source: up to date")
	case gitsync.SourceAhead:
		fmt.Fprintln(w, "config source: local commits to publish (rehome push)")
	case gitsync.SourceDirty:
		fmt.Fprintln(w, "config source: local config changes to publish (rehome push)")
	case gitsync.SourceChanged:
		fmt.Fprintln(w, "config source: updates available (rehome sync)")
	case gitsync.SourceUnknown:
		fmt.Fprintln(w, "config source: could not check the linked Git branch")
	}

	if len(s.Unsupported) > 0 {
		fmt.Fprintf(w, "not available on this OS (%d): %s\n", len(s.Unsupported), strings.Join(s.Unsupported, ", "))
	}

	if s.Clean() {
		fmt.Fprintln(w, "in sync: the system matches the config")

		return
	}

	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}

		fmt.Fprintf(w, "%s (%d):\n", title, len(items))

		for _, i := range items {
			fmt.Fprintf(w, "  %s\n", i)
		}
	}

	section("missing on this system (rehome install)", s.Missing)
	section("new on this system (rehome scan)", s.New)
	section("settings differ from files/ (rehome scan)", s.ChangedFiles)
	section("older than rehome.lock (rehome install --upgrade)", s.Behind)
	section("versions changed here, not in rehome.lock yet (rehome scan)", s.NotSynced)
}
