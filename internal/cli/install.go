package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/secrets"
	"github.com/rchehd/rehome/internal/sessions"
	"github.com/rchehd/rehome/internal/tui"
)

type installFlags struct {
	all, yes, overwrite, upgrade, offline, reinstall, noSettings, includeStale, push, noPush, bitwarden bool
	version, from                                                                                       string
	prefer                                                                                              []string
}

func newInstallCmd() *cobra.Command {
	var f installFlags

	cmd := &cobra.Command{
		Use:   "install [app...]",
		Short: "Install apps the nodes have on this machine",
		Long: "Install the named apps, every app the live nodes have (--all) or one node's apps (--from).\n" +
			"Without arguments in a terminal it opens a picker. Only this machine's node changes.",
		ValidArgsFunction: completeApps,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := sessions.ValidatePaths(flags.sessionPaths); err != nil {
				return err
			}

			if len(args) > 0 && f.all {
				return errors.New("name apps or use --all, not both")
			}

			if f.push && f.noPush {
				return errors.New("--push and --no-push are mutually exclusive")
			}

			picker := len(args) == 0 && !f.all && f.from == ""

			if picker && (f.yes || flags.json || !interactive()) {
				return errors.New("name the apps to install, or use --all or --from <node> (without arguments rehome install opens a picker in a terminal)")
			}

			if !f.yes && !flags.dryRun && (flags.json || !interactive()) {
				return errors.New("use --yes or --dry-run in non-interactive mode")
			}

			if f.version != "" && f.version != install.ModeLatest && f.version != install.ModeSame {
				return errors.New("--version must be same or latest")
			}

			options, restore, err := installOptions(cmd, f)

			if err != nil {
				return err
			}

			defer restore()

			req := app.InstallRequest{IDs: args, From: f.from, Prefer: f.prefer, IncludeStale: f.includeStale, NoSettings: f.noSettings}
			sel := install.Selection{All: f.reinstall, Offline: f.offline}
			var in *app.InstallInput
			var rep *install.Report

			if picker {
				in, rep, err = installPicker(cmd, req, sel, f, options)
			} else {
				var plan *install.Plan

				if in, plan, err = buildPlan(cmd, req, sel, f); err != nil {
					return err
				}

				switch {
				case flags.json:
					return installJSON(cmd, plan, options, req)
				case !f.yes:
					rep, err = confirmAndRun(cmd, plan, options)
				default:
					printPlan(cmd, plan)
					rep, err = executePlan(cmd, plan, options)
				}
			}

			if err != nil || rep == nil {
				return err
			}

			if !f.yes && !flags.dryRun {
				reviewKeptLocal(cmd, rep)
			}

			rep.Print(cmd.OutOrStdout())

			if err := liftTombstones(cmd, in, rep); err != nil {
				return err
			}

			if err := offerPush(cmd, f); err != nil {
				return err
			}

			if picker {
				if err := offerMCPInstall(cmd); err != nil {
					return err
				}
			}

			return exitCode(rep)
		},
	}

	fl := cmd.Flags()
	fl.BoolVar(&f.all, "all", false, "install every app the live nodes have")
	fl.StringVar(&f.from, "from", "", "take apps, versions and settings from this node only")
	fl.StringSliceVar(&f.prefer, "prefer", nil, "nodes to take versions and settings from first when several have an app")
	fl.BoolVar(&f.includeStale, "include-stale", false, "also take apps from nodes that have not pushed for a long time")
	fl.StringVar(&f.version, "version", "", "same to install the versions the other nodes have, or latest (asked in a terminal, otherwise latest)")
	fl.BoolVar(&f.noSettings, "no-settings", false, "install the apps without their settings files")
	fl.BoolVar(&f.reinstall, "reinstall", false, "install again what is already present")
	fl.BoolVar(&f.upgrade, "upgrade", false, "upgrade installed apps that are older than on the other nodes")
	fl.BoolVar(&f.push, "push", false, "publish this node's new versions to the linked config afterwards")
	fl.BoolVar(&f.noPush, "no-push", false, "do not offer to publish afterwards")
	fl.BoolVarP(&f.yes, "yes", "y", false, "run without asking")
	fl.BoolVar(&f.overwrite, "overwrite-files", false, "replace differing settings files (keeps .rehome.bak)")
	fl.BoolVar(&f.offline, "offline", false, "map installers between OSes from local package indexes only (no Homebrew API, no Repology)")
	fl.BoolVar(&f.bitwarden, "bitwarden", false, "restore settings from an unlocked Bitwarden vault")
	_ = cmd.RegisterFlagCompletionFunc("from", completeNodes)
	_ = cmd.RegisterFlagCompletionFunc("prefer", completeNodes)

	return cmd
}

// installOptions turns the flags into run options; restore puts BW_SESSION back afterwards.
func installOptions(cmd *cobra.Command, f installFlags) (install.Options, func(), error) {
	options := install.Options{OverwriteFiles: f.overwrite, Upgrade: f.upgrade, HarnessData: flags.harnessData, Sessions: flags.sessions, SessionPaths: flags.sessionPaths}
	restore := func() {}

	if !f.bitwarden {
		return options, restore, nil
	}

	if config.IsURL(flags.config) {
		return options, restore, errors.New("--bitwarden requires a trusted local config")
	}

	session, ok := os.LookupEnv("BW_SESSION")

	if !ok {
		if flags.dryRun {
			return options, restore, nil
		}

		return options, restore, errors.New("unlock Bitwarden and supply BW_SESSION")
	}

	if err := os.Unsetenv("BW_SESSION"); err != nil {
		return options, restore, err
	}

	options.ResolveSecret = func(ref string) (string, error) {
		return secrets.BitwardenValue(cmd.Context(), session, ref)
	}

	return options, func() { os.Setenv("BW_SESSION", session) }, nil
}

// confirmAndRun shows the plan and runs it once the user agrees.
func confirmAndRun(cmd *cobra.Command, plan *install.Plan, options install.Options) (*install.Report, error) {
	var summary bytes.Buffer
	summaryCmd := &cobra.Command{}
	summaryCmd.SetOut(&summary)
	printPlan(summaryCmd, plan)

	start, err := tui.Confirm("Start the installation?", summary.String())

	if err != nil || !start {
		return nil, err
	}

	return executePlan(cmd, plan, options)
}

// installPicker: the nodes, then the chosen node's apps, then the summary and the run. esc in the
// checklist goes back to the nodes; a recipe added from it reloads the config and reopens it.
func installPicker(cmd *cobra.Command, req app.InstallRequest, sel install.Selection, f installFlags, options install.Options) (*app.InstallInput, *install.Report, error) {
nodes:
	for {
		rows, err := nodeRows(cmd)

		if err != nil {
			return nil, nil, err
		}

		name, ok, err := tui.Nodes(rows)

		if err != nil || !ok {
			return nil, nil, err
		}

		req.From = name
		in, plan, err := buildPlan(cmd, req, sel, f)

		if err != nil {
			return nil, nil, err
		}

		f.version = plan.Mode

		for {
			res, ok, err := tui.Checklist(plan, tui.ChecklistOptions{Source: name, Back: true})

			switch {
			case err != nil || !ok:
				return nil, nil, err
			case res.Back:
				continue nodes
			case res.AddRecipe != "":
				if err := addRecipe(in, res.AddRecipe); err != nil {
					cmd.PrintErrln("error:", err)
				}

				if in, plan, err = buildPlan(cmd, req, sel, f); err != nil {
					return nil, nil, err
				}

				continue
			case len(res.Selection.IDs) == 0:
				cmd.Println("nothing selected")

				return in, nil, nil
			}

			chosen := res.Selection
			chosen.All, chosen.Offline = sel.All, sel.Offline

			if plan, err = app.BuildPlan(cmd.Context(), opts(cmd), in, chosen); err != nil {
				return nil, nil, err
			}

			options.UpgradeIDs = res.Upgrades
			rep, err := confirmAndRun(cmd, plan, options)

			return in, rep, err
		}
	}
}

// nodeRows describes every node for the picker, with how many of its apps this machine lacks.
func nodeRows(cmd *cobra.Command) ([]tui.NodeRow, error) {
	w, err := app.LoadWorkspace(opts(cmd))

	if err != nil {
		return nil, err
	}

	var rows []tui.NodeRow
	now := time.Now()

	for _, n := range w.All() {
		r := tui.NodeRow{Name: n.Name, System: n.System, Seen: seen(n.LastSeen, now), This: n.ID == w.Self.ID, Stale: n.Stale(now, w.Settings.StaleAfter())}

		for id, e := range n.Apps {
			if e.Unsupported {
				continue
			}

			r.Apps++

			if _, ok := w.Self.Apps[id]; !ok {
				r.Missing++
			}
		}

		rows = append(rows, r)
	}

	return rows, nil
}

// addRecipe asks how id installs on this platform and writes it to rehome.yml.
func addRecipe(in *app.InstallInput, id string) error {
	if in.Root == "" {
		return errors.New("the config is not a local folder; add the recipe where it lives")
	}

	var items []tui.MenuItem

	for _, m := range config.Managers {
		items = append(items, tui.MenuItem{Key: m, Title: m})
	}

	manager, err := tui.Menu("How does "+id+" install on "+in.Platform.Key()+"?", items)

	if err != nil || manager == "" {
		return err
	}

	help := manager + " package name"

	if manager == "script" {
		help = "the shell command that installs it"
	}

	pkg, ok, err := tui.Input("Recipe for "+id, help)

	if err != nil || !ok {
		return err
	}

	spec, err := config.NewSpec(manager, pkg)

	if err != nil {
		return err
	}

	if manager == "script" {
		if spec.Bin, ok, err = tui.Input("Recipe for "+id, "binary name or path that proves it is installed"); err != nil || !ok {
			return err
		}
	}

	return config.AddInstall(config.FilePath(in.Root), id, in.Platform.Key(), spec, "")
}

// liftTombstones drops the everywhere-removal of apps the user just installed again by name.
func liftTombstones(cmd *cobra.Command, in *app.InstallInput, rep *install.Report) error {
	if flags.dryRun || in.Root == "" {
		return nil
	}

	for _, o := range rep.Done {
		t, ok := in.Config.Tombstone(o.ID)

		if !ok {
			continue
		}

		if err := config.DropRemoved(config.FilePath(in.Root), o.ID); err != nil {
			return err
		}

		cmd.Printf("%s is no longer marked removed everywhere (%s removed it on %s)\n", o.ID, t.By, t.At.Format(time.DateOnly))
	}

	return nil
}

// offerPush publishes this node after an install: with --push, or when asked in a terminal.
func offerPush(cmd *cobra.Command, f installFlags) error {
	if flags.dryRun || f.noPush || flags.config != "" {
		return nil
	}

	s, err := config.LoadSettings()

	if err != nil || !s.Linked() {
		return err
	}

	if !f.push && (f.yes || !interactive() || ask(cmd, "Publish this node's new versions to the shared config? [y/N]", "n") != "y") {
		return nil
	}

	return publishChecked(cmd, s, "")
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

func buildPlan(cmd *cobra.Command, req app.InstallRequest, sel install.Selection, f installFlags) (*app.InstallInput, *install.Plan, error) {
	o := opts(cmd)
	in, err := app.LoadInstall(o, req)

	if err != nil {
		return nil, nil, err
	}

	if sel.Mode, err = installMode(f, in.Lock); err != nil {
		return nil, nil, err
	}

	plan, err := withSpinner(cmd, o, func(ctx context.Context, o app.Options) (*install.Plan, error) {
		return app.BuildPlan(ctx, o, in, sel)
	})

	return in, plan, err
}

// installMode: the flag wins; in a terminal the user picks when other nodes recorded versions; otherwise latest.
func installMode(f installFlags, lk *lock.Lock) (string, error) {
	switch {
	case f.version != "":
		return f.version, nil
	case len(lk.Apps) == 0 || f.yes || flags.json || !interactive():
		return install.ModeLatest, nil
	}

	choice, err := tui.Menu("Which versions?", []tui.MenuItem{
		{Key: install.ModeSame, Title: "Same", Help: "the versions the other nodes have, where the manager can install a fixed one"},
		{Key: install.ModeLatest, Title: "Latest", Help: "the newest versions available"},
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
		if s.Status == install.StatusUnavailable {
			cmd.Printf("  - %-28s %s\n", s.App.ID, s.Reason)
		}

		if s.Status == install.StatusInstall {
			name, pkg := s.Spec.Manager()
			version := ""

			if s.Pin != (lock.Entry{}) {
				version = " @" + pinLabel(s.Pin)
			}

			if s.PinNote != "" {
				version += " (latest: " + s.PinNote + ")"
			}

			if s.Mapped != "" {
				version += " (auto-mapped: " + s.Mapped + ")"
			}

			if from := plan.From[s.App.ID]; from != "" {
				version += " from " + from
			}

			cmd.Printf("  + %-28s %s %s%s\n", s.App.ID, name, pkg, version)
		}
	}

	for _, c := range plan.Skipped {
		cmd.Printf("  - %-28s %s (on %s)\n", c.ID, c.Reason, c.Node)
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
func installJSON(cmd *cobra.Command, plan *install.Plan, options install.Options, req app.InstallRequest) error {
	if flags.dryRun {
		return report(cmd, app.PlanJSON(plan, opts(cmd), req), "")
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
