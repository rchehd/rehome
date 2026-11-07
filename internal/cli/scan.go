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
	"github.com/rchehd/rehome/internal/ui"
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
	out := ui.New(cmd.OutOrStdout())
	unknown := slices.Sorted(maps.Keys(r.Unknown))

	out.Header(sp.Node.Node.Name, sp.Node.Node.System, fmt.Sprintf("%d apps recorded", len(sp.Node.Node.Apps)))
	out.Section("Apps", "")
	out.List(appItems(out, rep, sp.Node.Gone, unknown)...)

	if r.BaseSkipped > 0 {
		out.List(ui.Item{Mark: ui.Plain, Text: ui.Muted(fmt.Sprintf("%d base system packages, libraries and language data skipped", r.BaseSkipped))})
	}
	printSettings(out, r)
	printSecrets(out, r.Findings)
	printAttention(out, r.Attention, r.Warnings)

	if sp.Changed() {
		out.Summary("Plan:", ui.Paint(ui.Add, fmt.Sprintf("%d to add", len(rep.Added))), ui.Paint(ui.Update, fmt.Sprintf("%d to update", len(rep.Updated))), ui.Paint(ui.Drop, fmt.Sprintf("%d to drop", len(sp.Node.Gone))), fmt.Sprintf("%d settings files", len(r.FileChanges)))
		out.Println(ui.Muted("config: " + filepath.Join(sp.Root, config.FileName)))
	}
}

const labelWidth = 13

func appItems(out *ui.Out, rep config.MergeReport, gone, unknown []string) []ui.Item {
	var items []ui.Item

	add := func(m ui.Mark, label string, names []string) {
		if len(names) > 0 {
			items = append(items, ui.Item{Mark: m, Text: fmt.Sprintf("%-*s %s", labelWidth, fmt.Sprintf("%d %s", len(names), label), ui.Names(names, out.Room(labelWidth+6)))})
		}
	}

	add(ui.Add, "new", rep.Added)
	add(ui.Update, "updated", rep.Updated)
	add(ui.Drop, "gone", gone)

	for i, id := range unknown {
		unknown[i] = strings.TrimPrefix(id, "unknown-")
	}

	add(ui.Unknown, "no recipe", unknown)

	if len(unknown) > 0 {
		items[len(items)-1].Sub = []string{"rehome config add-install <id> makes a recipe"}
	}

	if len(items) == 0 {
		items = append(items, ui.Item{Mark: ui.Plain, Text: ui.Muted("no changes")})
	}

	return items
}

// printSettings counts settings files per app, the busiest apps first.
func printSettings(out *ui.Out, r *scan.Result) {
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

	if len(r.FileChanges) == 0 {
		return
	}

	auto := 0

	for _, byKey := range r.Config.Capture {
		for _, rule := range byKey {
			if rule.Origin == config.OriginAuto {
				auto++
			}
		}
	}

	summary := fmt.Sprintf("%d files in %d apps · %s, %s", len(r.FileChanges), len(perApp), ui.Paint(ui.Add, fmt.Sprintf("%d new", newFiles)), ui.Paint(ui.Update, fmt.Sprintf("%d changed", modified)))

	if auto > 0 {
		summary += ui.Muted(fmt.Sprintf(" · %d apps found outside the catalog", auto))
	}

	apps := slices.SortedFunc(maps.Keys(perApp), func(a, b string) int {
		return cmp.Or(perApp[b]-perApp[a], strings.Compare(a, b))
	})
	var counts []string

	for _, id := range apps {
		counts = append(counts, fmt.Sprintf("%s %d", strings.TrimPrefix(id, "unknown-"), perApp[id]))
	}

	out.Section("Settings", summary)
	out.List(ui.Item{Mark: ui.Plain, Text: ui.Names(counts, out.Room(6))})
}

// changeApp is the app a settings file belongs to: files/<app>/… in the config.
func changeApp(c files.Change) string {
	parts := strings.Split(filepath.ToSlash(c.Dst), "/")

	if len(parts) > 2 && parts[0] == "files" {
		return parts[1]
	}

	return filepath.ToSlash(c.Dst)
}

const shownSecrets = 12

// printSecrets groups redacted values by file.
func printSecrets(out *ui.Out, findings []secrets.Finding) {
	if len(findings) == 0 {
		return
	}

	keys := map[string][]string{}

	for _, f := range findings {
		keys[f.Path] = append(keys[f.Path], f.Key)
	}

	paths := slices.Sorted(maps.Keys(keys))
	var rows [][]string

	for _, path := range paths[:min(len(paths), shownSecrets)] {
		rows = append(rows, []string{path, ui.Muted(strings.Join(slices.Compact(slices.Sorted(slices.Values(keys[path]))), ", "))})
	}

	out.Section("Secrets", fmt.Sprintf("%d values redacted in %d files", len(findings), len(keys)))
	out.Table(nil, rows)

	if len(paths) > shownSecrets {
		out.List(ui.Item{Mark: ui.Plain, Text: ui.Muted(fmt.Sprintf("+%d more files; Review files shows them all", len(paths)-shownSecrets))})
	}
}

const groupedAttention = 3

// printAttention lists what the user has to handle; repeated reasons collapse into one line.
func printAttention(out *ui.Out, items []discover.Warning, warnings []string) {
	if len(items)+len(warnings) == 0 {
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

	var lines []ui.Item

	for _, k := range order {
		paths := byReason[k]

		if len(paths) < groupedAttention {
			for _, p := range paths {
				lines = append(lines, ui.Item{Mark: ui.Attention, Text: p + ": " + k.text, Hint: k.action})
			}

			continue
		}

		lines = append(lines, ui.Item{Mark: ui.Attention, Text: fmt.Sprintf("%d × %s", len(paths), k.text), Hint: k.action, Sub: []string{ui.Names(paths, out.Room(8))}})
	}

	for _, w := range warnings {
		lines = append(lines, ui.Item{Mark: ui.Attention, Text: "warning: " + w})
	}

	out.Section("Needs your attention", fmt.Sprintf("%d", len(items)+len(warnings)))
	out.List(lines...)
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

	switch askWrite(cmd) {
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

// askWrite returns y, n or r.
func askWrite(cmd *cobra.Command) string {
	choice, err := ui.Choose("Write the config and settings files?",
		ui.Choice{Key: "y", Label: "Write"},
		ui.Choice{Key: "r", Label: "Review files first"},
		ui.Choice{Key: "n", Label: "Cancel"},
	)

	if err != nil {
		cmd.PrintErrln("error:", err)

		return "n"
	}

	return choice
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
