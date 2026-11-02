package cli

import (
	"bytes"
	"errors"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/tui"
)

type installFlags struct {
	all, missing, yes, overwrite bool
}

func newInstallCmd() *cobra.Command {
	var f installFlags

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install everything in the config, or only what is missing",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.all && f.missing {
				return errors.New("--all and --missing are mutually exclusive")
			}

			if !f.yes && !flags.dryRun && !interactive() {
				return errors.New("use --yes in non-interactive mode")
			}

			plan, err := buildPlan(cmd, install.Selection{All: f.all})

			if err != nil {
				return err
			}

			if !f.yes && interactive() {
				return installInteractive(cmd, plan, f)
			}

			printPlan(cmd, plan)

			return runPlan(cmd, plan, install.Options{OverwriteFiles: f.overwrite})
		},
	}

	fl := cmd.Flags()
	fl.BoolVar(&f.all, "all", false, "reinstall everything enabled, even what is present")
	fl.BoolVar(&f.missing, "missing", false, "install only what is missing (default)")
	fl.BoolVarP(&f.yes, "yes", "y", false, "run without asking")
	fl.BoolVar(&f.overwrite, "overwrite-files", false, "replace differing settings files (keeps .rehome.bak)")

	return cmd
}

// installInteractive: checklist, then a summary with every command, then the run.
func installInteractive(cmd *cobra.Command, plan *install.Plan, f installFlags) error {
	sel, ok, err := tui.Checklist(plan)

	if err != nil || !ok {
		return err
	}

	if len(sel.IDs) == 0 {
		cmd.Println("nothing selected")

		return nil
	}

	sel.All = f.all
	plan, err = install.Build(cmd.Context(), newRunner(), plan.Config, plan.Root, plan.Platform, sel)

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

	report, err := executePlan(cmd, plan, install.Options{OverwriteFiles: f.overwrite})

	if err != nil {
		return err
	}

	reviewKeptLocal(cmd, report)
	report.Print(cmd.OutOrStdout())

	return exitCode(report)
}

// reviewKeptLocal offers each differing settings file with its diff (spec §9).
func reviewKeptLocal(cmd *cobra.Command, report *install.Report) {
	var kept []files.ApplyResult

	for _, res := range report.KeptLocal {
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

func buildPlan(cmd *cobra.Command, sel install.Selection) (*install.Plan, error) {
	cfg, root, err := loadConfig(cmd)

	if err != nil {
		return nil, err
	}

	p, err := platform.Detect()

	if err != nil {
		return nil, err
	}

	cmd.PrintErrln("checking what is installed …")

	return install.Build(cmd.Context(), newRunner(), cfg, root, p, sel)
}

func printPlan(cmd *cobra.Command, plan *install.Plan) {
	c := plan.Counts()
	cmd.Printf("to install: %d, settings only: %d, already installed: %d, unavailable: %d, disabled: %d\n",
		c[install.StatusInstall], c[install.StatusFilesOnly], c[install.StatusInstalled], c[install.StatusUnavailable], c[install.StatusDisabled])

	for _, s := range plan.Steps {
		if s.Status == install.StatusInstall {
			name, pkg := s.Spec.Manager()
			cmd.Printf("  + %-28s %s %s\n", s.App.ID, name, pkg)
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
