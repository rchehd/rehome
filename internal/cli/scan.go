package cli

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/discover"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/scan"
	"github.com/rchehd/rehome/internal/secrets"
	"github.com/rchehd/rehome/internal/tui"
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
	r, rep := sp.Result, sp.Report
	cmd.Printf("config    %s\n", filepath.Join(sp.Root, config.FileName))
	cmd.Printf("node      %s · %d apps recorded\n", sp.Node.Node.Name, len(sp.Node.Node.Apps))
	cmd.Printf("apps      %d new · %d updated · %d gone from this node · %d unknown\n", len(rep.Added), len(rep.Updated), len(sp.Node.Gone), len(r.Unknown))
	printNames(cmd, "new", rep.Added, "")
	printNames(cmd, "updated", rep.Updated, "")
	printNames(cmd, "gone", sp.Node.Gone, "")
	printNames(cmd, "unknown", slices.Sorted(maps.Keys(r.Unknown)), "rehome config add-install <id> … makes a recipe")
	printSettings(cmd, r)
	printSecrets(cmd, r.Findings)
	printAttention(cmd, r.Attention)

	for _, w := range r.Warnings {
		cmd.Printf("warning: %s\n", w)
	}
}

const shownNames = 12

// printNames lists names on one indented line, the first shownNames of them.
func printNames(cmd *cobra.Command, label string, names []string, hint string) {
	if len(names) == 0 {
		return
	}

	line := strings.Join(names[:min(len(names), shownNames)], ", ")

	if len(names) > shownNames {
		line += fmt.Sprintf(" (+%d more)", len(names)-shownNames)
	}

	if hint != "" {
		line += " — " + hint
	}

	cmd.Printf("          %s: %s\n", label, line)
}

// printSettings counts settings files per app, the busiest apps first.
func printSettings(cmd *cobra.Command, r *scan.Result) {
	newFiles, modified := 0, 0
	perApp := map[string]int{}

	for _, c := range r.FileChanges {
		if c.Old == nil {
			newFiles++
		} else {
			modified++
		}

		perApp[changeApp(c)]++
	}

	auto := 0

	for _, byKey := range r.Config.Capture {
		for _, rule := range byKey {
			if rule.Origin == config.OriginAuto {
				auto++
			}
		}
	}

	cmd.Printf("settings  %d new · %d changed in %d apps", newFiles, modified, len(perApp))

	if auto > 0 {
		cmd.Printf(" (%d found automatically outside the catalog)", auto)
	}

	cmd.Println()

	apps := slices.SortedFunc(maps.Keys(perApp), func(a, b string) int {
		return cmp.Or(perApp[b]-perApp[a], strings.Compare(a, b))
	})
	var counts []string

	for _, id := range apps {
		counts = append(counts, fmt.Sprintf("%s %d", id, perApp[id]))
	}

	printNames(cmd, "files", counts, "")
}

// changeApp is the app a settings file belongs to: files/<app>/… in the config.
func changeApp(c files.Change) string {
	parts := strings.Split(filepath.ToSlash(c.Dst), "/")

	if len(parts) > 2 && parts[0] == "files" {
		return parts[1]
	}

	return filepath.ToSlash(c.Dst)
}

// printSecrets groups redacted values by file.
func printSecrets(cmd *cobra.Command, findings []secrets.Finding) {
	if len(findings) == 0 {
		return
	}

	keys := map[string][]string{}

	for _, f := range findings {
		keys[f.Path] = append(keys[f.Path], f.Key)
	}

	cmd.Printf("secrets   %d values redacted in %d files\n", len(findings), len(keys))

	paths := slices.Sorted(maps.Keys(keys))

	for _, path := range paths[:min(len(paths), shownNames)] {
		cmd.Printf("          %s (%s)\n", path, strings.Join(slices.Compact(slices.Sorted(slices.Values(keys[path]))), ", "))
	}

	if len(paths) > shownNames {
		cmd.Printf("          +%d more files; r reviews them all\n", len(paths)-shownNames)
	}
}

const groupedAttention = 3

// printAttention lists what the user has to handle; repeated reasons collapse into one line.
func printAttention(cmd *cobra.Command, items []discover.Warning) {
	if len(items) == 0 {
		return
	}

	type reason struct{ text, action string }
	var order []reason
	byReason := map[reason][]string{}

	for _, a := range items {
		who := a.Path

		if a.ID != "" {
			who = a.ID + " (" + a.Path + ")"
		}

		k := reason{a.Reason, a.Action}

		if _, ok := byReason[k]; !ok {
			order = append(order, k)
		}

		byReason[k] = append(byReason[k], who)
	}

	cmd.Printf("needs your attention (%d):\n", len(items))

	for _, k := range order {
		paths := byReason[k]

		if len(paths) < groupedAttention {
			for _, p := range paths {
				cmd.Printf("  ! %s: %s — %s\n", p, k.text, k.action)
			}

			continue
		}

		cmd.Printf("  ! %d × %s — %s\n", len(paths), k.text, k.action)
		printNames(cmd, "paths", paths, "")
	}
}

// confirmChanges asks once for everything; "r" opens the files grouped by app to leave some out.
func confirmChanges(cmd *cobra.Command, changes []files.Change, yes bool) ([]files.Change, bool) {
	if yes {
		return changes, true
	}

	if !interactive() {
		cmd.PrintErrln("not a terminal: rerun with --yes to write")

		return nil, false
	}

	switch ask(cmd, "Write config and settings files? [y]es / [n]o / [r]eview files:", "n") {
	case "y":
		return changes, true
	case "r":
		keep, ok, err := tui.Review(reviewFiles(changes))

		if err != nil {
			cmd.PrintErrln("error:", err)

			return nil, false
		}

		if !ok {
			return nil, false
		}

		var accepted []files.Change

		for i, c := range changes {
			if keep[i] {
				accepted = append(accepted, c)
			}
		}

		return accepted, true
	}

	return nil, false
}

func reviewFiles(changes []files.Change) []tui.ReviewFile {
	out := make([]tui.ReviewFile, 0, len(changes))

	for _, c := range changes {
		f := tui.ReviewFile{App: changeApp(c), Path: adapter.Tilde(c.Src), New: c.Old == nil, Diff: string(c.New)}

		switch {
		case secrets.IsBinary(c.New):
			f.Diff = fmt.Sprintf("binary file, %d bytes", len(c.New))
		case !f.New:
			f.Diff = files.Unified(c.Old, c.New, c.Dst)
		}

		for _, finding := range c.Findings {
			f.Secrets = append(f.Secrets, finding.Key)
		}

		f.Secrets = slices.Compact(slices.Sorted(slices.Values(f.Secrets)))
		out = append(out, f)
	}

	return out
}
