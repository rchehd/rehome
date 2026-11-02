package status

import (
	"context"
	"fmt"
	"io"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/scan"
)

type Status struct {
	// Missing are enabled config apps not installed here.
	Missing []string
	// New are apps installed here but absent from the config.
	New []string
	// ChangedFiles are settings that differ between the system and files/.
	ChangedFiles []string
}

func (s *Status) Clean() bool {
	return len(s.Missing)+len(s.New)+len(s.ChangedFiles) == 0
}

// Compute compares the config with the system in both directions without writing anything.
func Compute(ctx context.Context, r *runner.Runner, cfg *config.Config, existing []byte, root string, p platform.Platform, cat *catalog.Catalog) (*Status, error) {
	plan, err := install.Build(ctx, r, cfg, root, p, install.Selection{})

	if err != nil {
		return nil, err
	}

	st := &Status{}

	for _, s := range plan.Steps {
		if s.Status == install.StatusInstall {
			st.Missing = append(st.Missing, s.App.ID)
		}
	}

	res, err := scan.Run(ctx, r, p, cat, root)

	if err != nil {
		return nil, err
	}

	_, report, err := config.Merge(existing, res.Config, p.Key())

	if err != nil {
		return nil, err
	}

	st.New = report.Added

	for _, c := range res.FileChanges {
		st.ChangedFiles = append(st.ChangedFiles, c.Dst)
	}

	return st, nil
}

func (s *Status) Print(w io.Writer) {
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
}
