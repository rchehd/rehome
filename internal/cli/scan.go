package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/scan"
	"github.com/rchehd/rehome/internal/secrets"
)

func newScanCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Detect installed software and settings and update the config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := prepareScan(cmd)

			if err != nil {
				return err
			}

			printScanSummary(cmd, plan)

			if flags.dryRun {
				return nil
			}

			if !plan.report.Changed() && len(plan.result.FileChanges) == 0 && !plan.lock.changed() {
				cmd.Println("config is up to date")

				return nil
			}

			accepted, ok := confirmChanges(cmd, plan.result.FileChanges, yes)

			if !ok {
				cmd.Println("nothing written")

				return nil
			}

			return plan.write(accepted)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "write without asking")

	return cmd
}

type scanPlan struct {
	root     string
	existing []byte
	merged   []byte
	report   config.MergeReport
	result   *scan.Result
	lock     *lockPlan
}

func prepareScan(cmd *cobra.Command) (*scanPlan, error) {
	p, err := platform.Detect()

	if err != nil {
		return nil, err
	}

	cat, err := catalog.Load()

	if err != nil {
		return nil, err
	}

	root, err := config.ResolveRoot(flags.config)

	if err != nil {
		return nil, err
	}

	existing, err := os.ReadFile(filepath.Join(root, config.FileName))

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	cmd.PrintErrln("scanning " + p.String() + " …")
	res, err := scan.Run(cmd.Context(), newRunner(), p, cat, root)

	if err != nil {
		return nil, err
	}

	merged, report, err := config.Merge(existing, res.Config, p.Key())

	if err != nil {
		return nil, err
	}

	// Install specs come from URLs and scripts too; the config file passes the same gate as files/.
	merged, findings := secrets.Redact(config.FileName, merged)
	res.Findings = append(res.Findings, findings...)

	sp := &scanPlan{root: root, existing: existing, merged: merged, report: report, result: res}
	sp.lock, err = prepareLock(cmd, sp, p)

	return sp, err
}

func (sp *scanPlan) write(changes []files.Change) error {
	if err := files.WriteChanges(sp.root, changes); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(sp.root, config.FileName), sp.merged, 0o644); err != nil {
		return err
	}

	return sp.lock.write()
}

func printScanSummary(cmd *cobra.Command, sp *scanPlan) {
	r := sp.result
	cmd.Printf("config: %s\n", filepath.Join(sp.root, config.FileName))
	rep := sp.report
	cmd.Printf("apps: %d added, %d updated, %d returned, %d missing on scan, %d unchanged\n", len(rep.Added), len(rep.Updated), len(rep.Returned), len(rep.MissingOnScan), len(rep.Unchanged))

	if len(sp.report.MissingOnScan) > 0 {
		cmd.Printf("  missing on scan: %v\n", sp.report.MissingOnScan)
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
	cmd.Printf("lock: %d versions recorded, %d changed in %s\n", len(sp.lock.current), len(sp.lock.changes), sp.lock.path)

	for _, f := range r.Findings {
		cmd.Printf("  redacted secret: %s line %d (%s)\n", f.Path, f.Line, f.Key)
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
