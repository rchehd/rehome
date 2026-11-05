package cli

import (
	"bytes"
	"context"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/secrets"
	"github.com/rchehd/rehome/internal/sessions"
	"github.com/rchehd/rehome/internal/source"
	"github.com/rchehd/rehome/internal/tui"
)

type installFlags struct {
	all, missing, yes, overwrite, upgrade, offline bool
	versions                                       string
	bitwarden                                      bool
}

func newInstallCmd() *cobra.Command {
	return newApplyCmd("install", "Install everything in the config, or only what is missing", false)
}

func newSyncCmd() *cobra.Command {
	return newApplyCmd("sync", "Pull the linked config and apply it to this machine", true)
}

func newApplyCmd(name, description string, sync bool) *cobra.Command {
	var f installFlags

	cmd := &cobra.Command{
		Use:   name,
		Short: description,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := sessions.ValidatePaths(flags.sessionPaths); err != nil {
				return err
			}
			if f.all && f.missing {
				return errors.New("--all and --missing are mutually exclusive")
			}

			if !f.yes && !flags.dryRun && (flags.json || !interactive()) {
				return errors.New("use --yes or --dry-run in non-interactive mode")
			}

			if f.versions != "" && f.versions != install.ModeLatest && f.versions != install.ModeLocked {
				return errors.New("--versions must be latest or locked")
			}

			options := install.Options{OverwriteFiles: f.overwrite, Upgrade: f.upgrade, HarnessData: flags.harnessData, Sessions: flags.sessions, SessionPaths: flags.sessionPaths}

			if f.bitwarden {
				if config.IsURL(flags.config) {
					return errors.New("--bitwarden requires a trusted local config")
				}

				if session, ok := os.LookupEnv("BW_SESSION"); ok {
					if err := os.Unsetenv("BW_SESSION"); err != nil {
						return err
					}

					defer os.Setenv("BW_SESSION", session)
					options.ResolveSecret = func(ref string) (string, error) {
						return secrets.BitwardenValue(cmd.Context(), session, ref)
					}
				} else if !flags.dryRun {
					return errors.New("unlock Bitwarden and supply BW_SESSION")
				}
			}

			if sync && flags.config == "" {
				s, err := config.LoadSettings()

				if err != nil {
					return err
				}

				if s.Linked() {
					if flags.dryRun {
						cmd.PrintErrln("dry run: the install preview uses the current local config")
					}

					if err := source.Open(s, flags.dryRun, cmd.ErrOrStderr()).Pull(cmd.Context()); err != nil {
						return err
					}
				}
			}

			plan, err := buildPlan(cmd, install.Selection{All: f.all, Offline: f.offline}, f)

			if err != nil {
				return err
			}

			if flags.json {
				return installJSON(cmd, plan, options)
			}

			if !f.yes && interactive() {
				return installInteractive(cmd, plan, f, options)
			}

			printPlan(cmd, plan)

			return runPlan(cmd, plan, options)
		},
	}

	fl := cmd.Flags()
	fl.BoolVar(&f.all, "all", false, "reinstall everything enabled, even what is present")
	fl.BoolVar(&f.missing, "missing", false, "install only what is missing (default)")
	fl.BoolVarP(&f.yes, "yes", "y", false, "run without asking")
	fl.BoolVar(&f.overwrite, "overwrite-files", false, "replace differing settings files (keeps .rehome.bak)")
	fl.StringVar(&f.versions, "versions", "", "latest, or locked to install the versions from rehome.lock (default: locked when a lock exists)")
	fl.BoolVar(&f.upgrade, "upgrade", false, "upgrade installed apps that are older than the lock")
	fl.BoolVar(&f.offline, "offline", false, "map installers between OSes from local package indexes only (no Homebrew API, no Repology)")
	fl.BoolVar(&f.bitwarden, "bitwarden", false, "restore settings from an unlocked Bitwarden vault")

	return cmd
}

// installInteractive: checklist, then a summary with every command, then the run.
func installInteractive(cmd *cobra.Command, plan *install.Plan, f installFlags, options install.Options) error {
	sel, upgrades, ok, err := tui.Checklist(plan)

	if err != nil || !ok {
		return err
	}

	if len(sel.IDs) == 0 {
		cmd.Println("nothing selected")

		return offerMCPInstall(cmd)
	}

	sel.All = f.all
	sel.Mode = plan.Mode
	plan, err = install.Build(cmd.Context(), newRunner(), plan.Config, plan.Root, plan.Platform, sel, plan.Lock)

	if err != nil {
		return err
	}

	var summary bytes.Buffer
	summaryCmd := &cobra.Command{}
	summaryCmd.SetOut(&summary)
	printPlan(summaryCmd, plan)

	start, err := tui.Confirm("Start the installation?", summary.String())

	if err != nil || !start {
		return err
	}

	options.UpgradeIDs = upgrades
	report, err := executePlan(cmd, plan, options)

	if err != nil {
		return err
	}

	if !flags.dryRun {
		reviewKeptLocal(cmd, report)
	}
	report.Print(cmd.OutOrStdout())

	if err := offerMCPInstall(cmd); err != nil {
		return err
	}

	return exitCode(report)
}

// reviewKeptLocal offers each differing settings file with its diff (spec §9).
func reviewKeptLocal(cmd *cobra.Command, report *install.Report) {
	var kept []files.ApplyResult

	for _, res := range report.KeptLocal {
		if res.Repo == nil && res.Local == nil {
			kept = append(kept, res)

			continue
		}

		replace, err := tui.Confirm("Replace your "+res.Dest+" with the config's version? (a .rehome.bak is kept)", files.Unified(res.Local, res.Repo, res.Dest))

		if err == nil && replace {
			if err := files.Overwrite(res); err == nil {
				continue
			}
		}

		kept = append(kept, res)
	}

	report.KeptLocal = kept
}

func buildPlan(cmd *cobra.Command, sel install.Selection, f installFlags) (*install.Plan, error) {
	o := opts(cmd)
	in, err := app.LoadInstall(o)

	if err != nil {
		return nil, err
	}

	if sel.Mode, err = installMode(f, in.Lock); err != nil {
		return nil, err
	}

	return withSpinner(cmd, o, func(ctx context.Context, o app.Options) (*install.Plan, error) {
		return app.BuildPlan(ctx, o, in, sel)
	})
}

// installMode: the flag wins; with a lock the user picks (locked under --yes); no lock means latest.
func installMode(f installFlags, lk *lock.Lock) (string, error) {
	switch {
	case f.versions != "":
		return f.versions, nil
	case len(lk.Apps) == 0:
		return install.ModeLatest, nil
	case f.yes || flags.json || !interactive():
		return install.ModeLocked, nil
	}

	choice, err := tui.Menu("rehome.lock found", []tui.MenuItem{
		{Key: install.ModeLocked, Title: "Locked", Help: "install the same versions as in rehome.lock"},
		{Key: install.ModeLatest, Title: "Latest", Help: "install the newest versions available"},
	})

	if err != nil {
		return "", err
	}

	if choice == "" {
		return "", errors.New("cancelled")
	}

	return choice, nil
}

func printPlan(cmd *cobra.Command, plan *install.Plan) {
	c := plan.Counts()
	cmd.Printf("to install: %d, settings only: %d, already installed: %d, unavailable: %d, disabled: %d, ignored: %d\n",
		c[install.StatusInstall], c[install.StatusFilesOnly], c[install.StatusInstalled], c[install.StatusUnavailable], c[install.StatusDisabled], c[install.StatusIgnored])

	for _, s := range plan.Steps {
		if plan.Platform.WSL && s.Status == install.StatusUnavailable {
			cmd.Printf("  - %-28s %s\n", s.App.ID, s.Reason)
		}

		if s.Status == install.StatusInstall {
			name, pkg := s.Spec.Manager()
			version := ""

			if s.Pin != (lock.Entry{}) {
				version = " @" + pinLabel(s.Pin)
			}

			if s.Mapped != "" {
				version += " (auto-mapped: " + s.Mapped + ")"
			}

			cmd.Printf("  + %-28s %s %s%s\n", s.App.ID, name, pkg, version)
		}
	}

	for _, s := range plan.Steps {
		if s.Upgrade != (lock.Entry{}) {
			cmd.Printf("  ↑ %-28s %s → %s (only with --upgrade or u in the checklist)\n", s.App.ID, s.Installed, pinLabel(s.Upgrade))
		}
	}

	if cmds := plan.Commands(); len(cmds) > 0 {
		cmd.Println("commands that will run:")

		for _, c := range cmds {
			cmd.Println("  $ " + c)
		}
	}
}

func runPlan(cmd *cobra.Command, plan *install.Plan, opts install.Options) error {
	report, err := executePlan(cmd, plan, opts)

	if err != nil {
		return err
	}

	report.Print(cmd.OutOrStdout())

	return exitCode(report)
}

func exitCode(report *install.Report) error {
	if len(report.Failed) > 0 {
		return ExitError{Code: 1}
	}

	return nil
}

func executePlan(cmd *cobra.Command, plan *install.Plan, opts install.Options) (*install.Report, error) {
	return install.Execute(cmd.Context(), newRunner(), plan, opts, func(e install.Event) {
		switch {
		case e.Total > 0:
			cmd.PrintErrf("» [%d/%d] %s %s\n", e.Index, e.Total, e.Phase, e.ID)
		case e.ID != "":
			cmd.PrintErrf("» %s %s\n", e.Phase, e.ID)
		default:
			cmd.PrintErrf("» %s\n", e.Phase)
		}
	})
}

func pinLabel(e lock.Entry) string {
	if e.Commit != "" {
		return e.Commit[:min(12, len(e.Commit))]
	}

	return e.Version
}

// installJSON prints the plan for --dry-run, or runs it (--yes) and prints the report.
func installJSON(cmd *cobra.Command, plan *install.Plan, options install.Options) error {
	if flags.dryRun {
		return report(cmd, app.PlanJSON(plan, opts(cmd)), "")
	}

	rep, err := executePlan(cmd, plan, options)

	if err != nil {
		return err
	}

	if err := report(cmd, rep, ""); err != nil {
		return err
	}

	return exitCode(rep)
}
