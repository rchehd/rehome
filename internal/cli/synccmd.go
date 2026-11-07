package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lan"
	"github.com/rchehd/rehome/internal/source"
	"github.com/rchehd/rehome/internal/ui"
)

type syncReport struct {
	Install *install.Report `json:"install,omitempty"`
	// RemovedElsewhere are apps still here that another node removed everywhere.
	RemovedElsewhere []string        `json:"removed_elsewhere"`
	Scan             app.ScanReport  `json:"scan"`
	Published        bool            `json:"published"`
	LAN              *lan.PullReport `json:"lan,omitempty"`
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

			if linked && len(s.LAN.Paired) > 0 && !flags.dryRun {
				ctx, cancel := context.WithCancel(cmd.Context())
				defer cancel()

				rep, err := syncLAN(ctx, cmd, s, f.yes)

				if err != nil {
					return err
				}

				out.LAN = &rep
			}

			if !noApply {
				rep, err := syncApply(cmd, f)

				if err != nil {
					return err
				}

				out.Install = rep

				if out.RemovedElsewhere, err = syncRemovals(cmd, f.yes); err != nil {
					return err
				}
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

// syncRemovals offers to remove apps other nodes removed everywhere; nothing is removed unless the user says so.
func syncRemovals(cmd *cobra.Command, yes bool) ([]string, error) {
	o := opts(cmd)
	w, err := app.LoadWorkspace(o)

	if err != nil {
		return nil, err
	}

	var offered []string

	for _, p := range w.Tombstoned(cmd.Context(), o) {
		offered = append(offered, p.ID)
		about := fmt.Sprintf("%s removed %s everywhere on %s", p.Tombstone.By, p.ID, p.Tombstone.At.Format(time.DateOnly))

		if yes || flags.json || !interactive() {
			cmd.PrintErrf("%s; rehome remove %s removes it here\n", about, p.ID)

			continue
		}

		if !confirm(cmd, about+". Remove it here?", false) {
			continue
		}

		notes, err := p.Run(cmd.Context(), o)

		if err != nil {
			return offered, err
		}

		for _, n := range notes {
			cmd.PrintErrln("note: " + n)
		}
	}

	return offered, nil
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

// syncLAN shares this machine for the rest of the sync and pulls from every paired machine it finds.
func syncLAN(ctx context.Context, cmd *cobra.Command, s config.Settings, yes bool) (lan.PullReport, error) {
	id, err := lanIdentity()

	if err != nil {
		return lan.PullReport{}, err
	}

	p, err := share(ctx, s, id, nil, cmd.ErrOrStderr(), nil)

	if err != nil {
		cmd.PrintErrln("warning:", err, "— pulling without sharing")
		p = &pins{s: s}
	}

	client := &lan.Client{Self: id, Trusted: p.trusted}
	rep, err := lan.Pull(ctx, lan.PullOptions{Root: s.ClonePath, OwnID: s.NodeID, Client: client, Found: peers(ctx, s), Settings: s, Confirm: func(a config.App) bool {
		return confirmRecipe(cmd, a, yes)
	}})

	if err != nil {
		return rep, err
	}

	if rep.OwnRevision > s.LAN.OwnRevision {
		if err := saveOwnRevision(rep.OwnRevision); err != nil {
			return rep, err
		}
	}

	if !flags.json {
		printLAN(cmd, rep)
	}

	return rep, nil
}

// saveOwnRevision remembers how far peers saw this node, so the next save continues past it.
func saveOwnRevision(revision int) error {
	s, err := config.LoadSettings()

	if err != nil {
		return err
	}

	s.LAN.OwnRevision = max(s.LAN.OwnRevision, revision)

	return s.Save()
}

// confirmRecipe shows what a peer's recipe would run or add, escaped, and asks before taking it.
func confirmRecipe(cmd *cobra.Command, a config.App, yes bool) bool {
	if yes || flags.json || !interactive() {
		return false
	}

	var lines []ui.Item

	for _, key := range slices.Sorted(maps.Keys(a.Install)) {
		spec := a.Install[key]

		for label, v := range map[string]string{"script": spec.Script, "deb": spec.DebURL, "tap": spec.Tap, "repo": spec.Repo, "remote": spec.RemoteURL} {
			if v != "" {
				lines = append(lines, ui.Item{Mark: ui.Attention, Text: lan.Clean(key + " " + label + ": " + v)})
			}
		}
	}

	for _, c := range a.PostInstall {
		lines = append(lines, ui.Item{Mark: ui.Attention, Text: lan.Clean("post install: " + c)})
	}

	out := ui.New(cmd.OutOrStdout())
	out.Section("Recipe "+lan.Clean(a.ID)+" from a paired machine runs code or adds a source", "")
	out.List(lines...)

	return confirm(cmd, "Add this recipe?", false)
}

func printLAN(cmd *cobra.Command, rep lan.PullReport) {
	var items []ui.Item
	clean := func(names []string) string { return lan.Clean(strings.Join(names, ", ")) }

	if len(rep.Updated) > 0 {
		items = append(items, ui.Item{Mark: ui.Add, Text: clean(rep.Updated), Hint: "newer nodes taken"})
	}

	if len(rep.RecipesAdded) > 0 {
		items = append(items, ui.Item{Mark: ui.Add, Text: clean(slices.Compact(slices.Sorted(slices.Values(rep.RecipesAdded)))), Hint: "recipes added"})
	}

	for _, name := range slices.Sorted(maps.Keys(rep.Skipped)) {
		items = append(items, ui.Item{Mark: ui.Drop, Text: lan.Clean(name), Hint: lan.Clean(rep.Skipped[name])})
	}

	if len(rep.Declined) > 0 {
		items = append(items, ui.Item{Mark: ui.Attention, Text: clean(rep.Declined), Hint: "recipes that run code or add sources, not taken"})
	}

	for _, u := range rep.Unreachable {
		items = append(items, ui.Item{Mark: ui.Unknown, Text: lan.Clean(u)})
	}

	if len(items) == 0 {
		items = append(items, ui.Item{Mark: ui.Plain, Text: ui.Muted("no paired machine found on the network")})
	}

	out := ui.New(cmd.OutOrStdout())
	out.Section("LAN", "")
	out.List(items...)
}
