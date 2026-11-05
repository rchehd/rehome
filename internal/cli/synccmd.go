package cli

import (
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/source"
)

type syncReport struct {
	Install   *install.Report `json:"install,omitempty"`
	Scan      app.ScanReport  `json:"scan"`
	Published bool            `json:"published"`
}

func newSyncCmd() *cobra.Command {
	var f installFlags
	var noApply bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Pull, update installed apps' settings, scan this node and push",
		Long: "Exchange state with the shared config: pull, apply the newest settings of apps already installed here,\n" +
			"record this node with a scan, then publish. It never installs new apps; rehome install does that.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !f.yes && !flags.dryRun && (flags.json || !interactive()) {
				return errors.New("use --yes or --dry-run in non-interactive mode")
			}

			var s config.Settings
			linked := flags.config == ""

			if linked {
				var err error

				if s, err = linkedSettings(); err != nil {
					return err
				}

				if err := source.Open(s, flags.dryRun, cmd.ErrOrStderr()).Pull(cmd.Context()); err != nil {
					return err
				}
			}

			var out syncReport

			if !noApply {
				rep, err := syncApply(cmd, f)

				if err != nil {
					return err
				}

				out.Install = rep
			}

			scanned, err := syncScan(cmd, f.yes)

			if err != nil {
				return err
			}

			out.Scan = scanned

			if linked && !f.noPush {
				if err := publishChecked(cmd, s, ""); err != nil {
					return err
				}

				out.Published = !flags.dryRun
			}

			if flags.json {
				if err := report(cmd, out, ""); err != nil {
					return err
				}
			}

			if out.Install != nil {
				return exitCode(out.Install)
			}

			return nil
		},
	}

	fl := cmd.Flags()
	fl.BoolVar(&noApply, "no-apply", false, "only exchange node snapshots; leave this machine's settings alone")
	fl.BoolVar(&f.noPush, "no-push", false, "do not publish this node afterwards")
	fl.BoolVarP(&f.yes, "yes", "y", false, "run without asking")
	fl.BoolVar(&f.overwrite, "overwrite-files", false, "replace differing settings files (keeps .rehome.bak)")
	fl.BoolVar(&f.offline, "offline", false, "map installers between OSes from local package indexes only (no Homebrew API, no Repology)")
	fl.BoolVar(&f.bitwarden, "bitwarden", false, "restore settings from an unlocked Bitwarden vault")

	return cmd
}

// syncApply brings the settings of apps already installed here up to the newest node's; nothing new is installed.
func syncApply(cmd *cobra.Command, f installFlags) (*install.Report, error) {
	options, restore, err := installOptions(cmd, f)

	if err != nil {
		return nil, err
	}

	defer restore()

	f.version = install.ModeLatest
	_, plan, err := buildPlan(cmd, app.InstallRequest{}, install.Selection{Offline: f.offline}, f)

	if err != nil {
		return nil, err
	}

	plan.Steps = slices.DeleteFunc(plan.Steps, func(s install.Step) bool {
		return s.Status != install.StatusInstalled && s.Status != install.StatusFilesOnly
	})

	rep, err := executePlan(cmd, plan, options)

	if err != nil || flags.json {
		return rep, err
	}

	if !f.yes && !flags.dryRun {
		reviewKeptLocal(cmd, rep)
	}

	rep.Print(cmd.OutOrStdout())

	return rep, nil
}

// syncScan records this node: the apps and settings found here, written once confirmed.
func syncScan(cmd *cobra.Command, yes bool) (app.ScanReport, error) {
	plan, err := withSpinner(cmd, opts(cmd), app.PrepareScan)

	if err != nil {
		return app.ScanReport{}, err
	}

	written := false

	if !flags.json {
		printScanSummary(cmd, plan)
	}

	if flags.dryRun || !plan.Changed() {
		return plan.JSON(false), nil
	}

	accepted, ok := plan.Result.FileChanges, yes

	if !flags.json {
		accepted, ok = confirmChanges(cmd, plan.Result.FileChanges, yes)
	}

	if ok {
		if err := plan.Write(accepted); err != nil {
			return app.ScanReport{}, err
		}

		written = true
	}

	return plan.JSON(written), nil
}

// completeApps offers every recipe, described by the nodes that have it, without network access.
func completeApps(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	w, err := app.LoadWorkspace(opts(cmd))

	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	owners := map[string][]string{}

	for _, a := range w.Config.Apps {
		owners[a.ID] = nil
	}

	for _, n := range w.Nodes {
		for id, e := range n.Apps {
			if _, ok := owners[id]; ok && !e.Unsupported {
				owners[id] = append(owners[id], n.Name)
			}
		}
	}

	var out []string

	for _, id := range slices.Sorted(maps.Keys(owners)) {
		if slices.Contains(args, id) || !strings.HasPrefix(id, toComplete) {
			continue
		}

		desc := "recipe only"

		if len(owners[id]) > 0 {
			desc = strings.Join(owners[id], ", ")
		}

		out = append(out, id+"\t"+desc)
	}

	return out, cobra.ShellCompDirectiveNoFileComp
}
