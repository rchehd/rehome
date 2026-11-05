package cli

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
)

func newScanCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Detect installed software and settings and update the config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := withSpinner(cmd, opts(cmd), app.PrepareScan)

			if err != nil {
				return err
			}

			if flags.json {
				written := false

				// Without --yes a JSON scan only reports; an agent writes by asking again with --yes.
				if yes && !flags.dryRun && plan.Changed() {
					if err := plan.Write(plan.Result.FileChanges); err != nil {
						return err
					}

					written = true
				}

				return report(cmd, plan.JSON(written), "")
			}

			printScanSummary(cmd, plan)

			if flags.dryRun {
				return nil
			}

			if !plan.Changed() {
				cmd.Println("config is up to date")

				return nil
			}

			accepted, ok := confirmChanges(cmd, plan.Result.FileChanges, yes)

			if !ok {
				cmd.Println("nothing written")

				return nil
			}

			return plan.Write(accepted)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "write without asking")

	return cmd
}

func printScanSummary(cmd *cobra.Command, sp *app.ScanPlan) {
	r := sp.Result
	cmd.Printf("config: %s\n", filepath.Join(sp.Root, config.FileName))
	rep := sp.Report
	cmd.Printf("apps: %d added, %d updated, %d returned, %d missing on scan, %d unchanged\n", len(rep.Added), len(rep.Updated), len(rep.Returned), len(rep.MissingOnScan), len(rep.Unchanged))

	if len(sp.Report.MissingOnScan) > 0 {
		cmd.Printf("  missing on scan: %v\n", sp.Report.MissingOnScan)
	}

	if len(r.Unknown) > 0 {
		cmd.Printf("unknown (disabled, add an install method to enable): %v\n", r.Unknown)
	}

	newFiles, modified := 0, 0

	for _, c := range r.FileChanges {
		if c.Old == nil {
			newFiles++
		} else {
			modified++
		}
	}

	cmd.Printf("settings files: %d new, %d changed\n", newFiles, modified)
	cmd.Printf("node %s: %d versions recorded, %d changed in %s\n", sp.Node.Node.Name, len(sp.Node.Node.Apps), len(sp.Node.Changes), sp.Node.Node.Path)

	for _, f := range r.Findings {
		cmd.Printf("  redacted secret: %s line %d (%s)\n", f.Path, f.Line, f.Key)
	}

	auto := 0

	for _, byKey := range r.Config.Capture {
		for _, rule := range byKey {
			if rule.Origin == config.OriginAuto {
				auto++
			}
		}
	}

	if auto > 0 {
		cmd.Printf("settings found automatically for %d apps outside the catalog (capture: origin auto)\n", auto)
	}

	if len(r.Attention) > 0 {
		cmd.Printf("needs your attention (%d):\n", len(r.Attention))

		for _, a := range r.Attention {
			who := a.Path

			if a.ID != "" {
				who = a.ID + " (" + a.Path + ")"
			}

			cmd.Printf("  ! %s: %s — %s\n", who, a.Reason, a.Action)
		}
	}

	for _, w := range r.Warnings {
		cmd.Printf("warning: %s\n", w)
	}
}

// confirmChanges asks once for everything; "r" reviews each changed file with its diff.
func confirmChanges(cmd *cobra.Command, changes []files.Change, yes bool) ([]files.Change, bool) {
	if yes {
		return changes, true
	}

	if !interactive() {
		cmd.PrintErrln("not a terminal: rerun with --yes to write")

		return nil, false
	}

	switch ask(cmd, "Write config and settings files? [y]es / [n]o / [r]eview changed files:", "n") {
	case "y":
		return changes, true
	case "r":
		var accepted []files.Change

		for _, c := range changes {
			if c.Old != nil {
				cmd.Print(files.Unified(c.Old, c.New, c.Dst))

				if ask(cmd, "Take this version from the system? [y/N]", "n") != "y" {
					continue
				}
			}

			accepted = append(accepted, c)
		}

		return accepted, true
	}

	return nil, false
}
