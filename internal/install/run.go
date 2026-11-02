package install

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rchehd/rehome/internal/adapter/apt"
	"github.com/rchehd/rehome/internal/ai"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/runner"
)

var prerequisites = []string{"git", "curl", "ca-certificates", "gnupg"}

type Options struct {
	OverwriteFiles bool
	// Upgrade raises every installed app older than the lock; UpgradeIDs only those listed.
	Upgrade    bool
	UpgradeIDs []string
}

type Event struct {
	Index, Total int
	ID, Phase    string
}

type Outcome struct {
	ID         string `json:"id"`
	Reason     string `json:"reason,omitempty"`
	StderrTail string `json:"stderr_tail,omitempty"`
}

type Report struct {
	Done          []Outcome           `json:"installed"`
	Skipped       []Outcome           `json:"skipped"`
	Failed        []Outcome           `json:"failed"`
	KeptLocal     []files.ApplyResult `json:"kept_local"`
	ManualSecrets []string            `json:"fill_in_secrets"`
	Notes         []string            `json:"notes"`
	// Upgraded were raised to the lock version; Upgradable could be (rehome install --upgrade).
	Upgraded     []Outcome `json:"upgraded"`
	Upgradable   []Outcome `json:"upgradable"`
	VersionNotes []string  `json:"version_notes"`
}

type executor struct {
	ctx      context.Context
	r        *runner.Runner
	plan     *Plan
	opts     Options
	progress func(Event)
	report   *Report
	// state per app id: done, installed, disabled, skipped, failed.
	state map[string]string
	// brokenBefore holds packages dpkg already had broken; rehome only cleans up its own.
	brokenBefore map[string]bool
}

// Execute runs the plan; a failing step never stops the others.
func Execute(ctx context.Context, r *runner.Runner, plan *Plan, opts Options, progress func(Event)) (*Report, error) {
	if progress == nil {
		progress = func(Event) {}
	}

	x := &executor{ctx: ctx, r: r, plan: plan, opts: opts, progress: progress, report: &Report{}, state: map[string]string{}}
	missing := x.missingPrerequisites()

	if !r.DryRun && x.needsSudo(missing) {
		stop, err := runner.StartSudo(ctx)

		if err != nil {
			return nil, err
		}

		defer stop()
	}

	x.recordBroken()
	failedRepos := x.prepareApt(missing)
	x.installAll(failedRepos)
	x.upgrade()
	x.restoreClaude()
	x.postInstall()
	x.recordVersions()

	return x.report, nil
}

func (x *executor) missingPrerequisites() []string {
	var missing []string

	if x.plan.Platform.Key() != "debian" {
		return nil
	}

	for _, pkg := range prerequisites {
		if ok, _ := (apt.Apt{}).Installed(x.ctx, x.r, config.InstallSpec{Apt: pkg}); !ok {
			missing = append(missing, pkg)
		}
	}

	return missing
}

func (x *executor) needsSudo(missing []string) bool {
	if len(missing) > 0 || len(x.usedRepos()) > 0 {
		return true
	}

	for _, s := range x.plan.Steps {
		if s.Status != StatusInstall && s.Status != StatusFilesOnly {
			continue
		}

		if s.Spec.Apt != "" || s.Spec.Snap != "" || s.Spec.Flatpak != "" || strings.Contains(s.Spec.Script, "sudo") {
			return true
		}

		for _, c := range s.App.PostInstall {
			if strings.Contains(c, "sudo") {
				return true
			}
		}
	}

	return false
}

// prepareApt never aborts the run: a broken mirror or repo becomes a note and failed steps.
func (x *executor) prepareApt(missing []string) map[string]bool {
	failed := map[string]bool{}

	if x.plan.Platform.Key() != "debian" {
		return failed
	}

	repos := x.usedRepos()
	needUpdate := len(missing) > 0

	for _, s := range x.plan.Steps {
		needUpdate = needUpdate || (s.Status == StatusInstall && s.Spec.Apt != "")
	}

	if needUpdate && len(repos) == 0 {
		x.progress(Event{Phase: "apt update"})

		if _, err := x.r.Run(x.ctx, apt.AptGet("update")); err != nil {
			x.note("apt-get update: %v", err)
		}
	}

	if len(missing) > 0 {
		x.progress(Event{Phase: "prerequisites"})

		if _, err := x.r.Run(x.ctx, apt.AptGet(append([]string{"install", "-y"}, missing...)...)); err != nil {
			x.note("install prerequisites %v: %v", missing, err)
		}
	}

	if len(repos) == 0 {
		return failed
	}

	x.progress(Event{Phase: "apt repositories"})
	reasons, err := apt.ApplyRepos(x.ctx, x.r, x.plan.Root, repos, x.plan.Config.AptKeys, x.plan.Platform)

	for name, reason := range reasons {
		failed[name] = true
		x.note("apt repo %s: %s", name, reason)
	}

	if err != nil {
		x.note("apt-get update: %v", err)
	}

	return failed
}

func (x *executor) usedRepos() map[string]config.AptRepo {
	used := map[string]config.AptRepo{}

	for _, s := range x.plan.Steps {
		if s.Status == StatusInstall && s.Spec.Repo != "" {
			used[s.Spec.Repo] = x.plan.Config.AptRepos[s.Spec.Repo]
		}
	}

	return used
}

// installAll walks the steps in order; each app's settings land right after its own step,
// so installers never meet a half-prepared directory and later ones see the user's rc files.
func (x *executor) installAll(failedRepos map[string]bool) {
	x.batchApt(failedRepos)

	for i, s := range x.plan.Steps {
		if _, seen := x.state[s.App.ID]; !seen {
			x.installStep(i, s, failedRepos)
		}

		if st := x.state[s.App.ID]; st == "done" || st == "installed" {
			// A settings-only app counts as done (and runs post_install) only when files changed.
			if x.applyFiles(s) && s.Status == StatusFilesOnly {
				x.state[s.App.ID] = "done"
			}
		}
	}
}

func (x *executor) installStep(i int, s Step, failedRepos map[string]bool) {
	id := s.App.ID

	switch s.Status {
	case StatusInstalled:
		x.state[id] = "installed"

		return
	case StatusFilesOnly:
		x.state[id] = "installed"

		return
	case StatusIgnored:
		x.state[id] = "disabled"

		return
	case StatusDisabled:
		x.state[id] = "disabled"
		x.report.Skipped = append(x.report.Skipped, Outcome{ID: id})

		return
	case StatusUnavailable:
		x.skip(id, s.Reason)

		return
	}

	if dep := x.failedDep(s.App); dep != "" {
		x.skip(id, "requires "+dep+", which did not install")

		return
	}

	if failedRepos[s.Spec.Repo] {
		x.fail(id, "apt repo "+s.Spec.Repo+" is unavailable", nil)

		return
	}

	x.progress(Event{Index: i + 1, Total: len(x.plan.Steps), ID: id, Phase: "install"})

	if err := x.install(s); err != nil {
		reason := "install failed"

		if removed := x.unbreakApt(); s.Spec.Apt != "" && len(removed) > 0 {
			reason += fmt.Sprintf("; its setup script failed, so %v were removed to keep apt working", removed)
		}

		x.fail(id, reason, err)

		return
	}

	if ok, _ := s.Adapter.Installed(x.ctx, x.r, s.Spec); !ok && !x.r.DryRun {
		x.fail(id, "installer finished but the app is still not detected", nil)

		return
	}

	x.done(id)
}

// batchApt installs plain repo packages in one apt-get call. Packages apt cannot find fail
// up front (one unknown name would reject the whole call); a setup failure removes the broken
// package and retries the batch once; only what still fails is retried one by one.
func (x *executor) batchApt(failedRepos map[string]bool) {
	byPkg := map[string]string{}
	var pkgs []string

	for _, s := range x.plan.Steps {
		if s.Status == StatusInstall && s.Spec.Apt != "" && s.Spec.DebURL == "" && len(s.App.Requires) == 0 && !failedRepos[s.Spec.Repo] {
			byPkg[s.Spec.Apt] = s.App.ID
			pkgs = append(pkgs, s.Spec.Apt)
		}
	}

	if len(pkgs) < 2 || x.r.DryRun {
		return
	}

	missing, err := apt.Unavailable(x.ctx, x.r, pkgs)

	if err == nil {
		for _, p := range missing {
			x.fail(byPkg[p], "package not found in the configured apt repositories", nil)
		}

		pkgs = without(pkgs, missing)
	}

	for attempt := 0; attempt < 2 && len(pkgs) > 0; attempt++ {
		x.progress(Event{Phase: fmt.Sprintf("apt: %d packages", len(pkgs))})

		if _, err := x.r.Run(x.ctx, apt.AptGet(append([]string{"install", "-y", "--"}, pkgs...)...)); err == nil {
			for _, p := range pkgs {
				x.done(byPkg[p])
			}

			return
		}

		removed := x.unbreakApt()

		if len(removed) == 0 {
			return
		}

		for _, p := range removed {
			if id, ok := byPkg[p]; ok {
				x.fail(id, fmt.Sprintf("its setup script failed, so %v were removed to keep apt working", removed), nil)
			}
		}

		pkgs = without(pkgs, removed)
	}
}

func without(list, drop []string) []string {
	skip := map[string]bool{}

	for _, d := range drop {
		skip[d] = true
	}

	var kept []string

	for _, v := range list {
		if !skip[v] {
			kept = append(kept, v)
		}
	}

	return kept
}

// applyFiles reports whether any file was written.
func (x *executor) applyFiles(s Step) bool {
	if s.App.Files == "" {
		return false
	}

	x.progress(Event{ID: s.App.ID, Phase: "settings"})
	results, err := files.Apply(x.plan.Root, x.plan.Config.Files[s.App.Files], files.ApplyOptions{
		FileKey:   x.plan.Platform.FileKey(),
		Overwrite: x.opts.OverwriteFiles,
		DryRun:    x.r.DryRun,
	})

	if err != nil {
		x.note("%s settings: %v", s.App.ID, err)
	}

	changed := false

	for _, res := range results {
		changed = changed || res.Status == files.StatusCopied || res.Status == files.StatusOverwritten

		if res.Status == files.StatusKeptLocal {
			x.report.KeptLocal = append(x.report.KeptLocal, res)
		}

		if res.Redacted {
			x.report.ManualSecrets = append(x.report.ManualSecrets, res.Dest)
		}
	}

	return changed
}

func (x *executor) recordBroken() {
	x.brokenBefore = map[string]bool{}

	if x.plan.Platform.Key() != "debian" {
		return
	}

	broken, _ := apt.Broken(x.ctx, x.r)

	for _, p := range broken {
		x.brokenBefore[p] = true
	}

	if len(broken) > 0 {
		x.note("dpkg already had broken packages %v; apt installs may fail until `sudo dpkg --configure -a` succeeds", broken)
	}
}

// unbreakApt removes packages an apt step left half-configured (e.g. a postinst that needs
// systemd), so one bad package does not fail every later apt install.
func (x *executor) unbreakApt() []string {
	if x.plan.Platform.Key() != "debian" || x.r.DryRun {
		return nil
	}

	broken, err := apt.Broken(x.ctx, x.r)

	if err != nil {
		return nil
	}

	var fresh []string

	for _, p := range broken {
		if !x.brokenBefore[p] {
			fresh = append(fresh, p)
		}
	}

	if len(fresh) == 0 {
		return nil
	}

	if err := apt.RemoveBroken(x.ctx, x.r, fresh); err != nil {
		x.note("could not remove half-configured packages %v: %v", fresh, err)

		// Give up on them once: retrying after every later failure only repeats this note.
		for _, p := range fresh {
			x.brokenBefore[p] = true
		}

		return nil
	}

	return fresh
}

func (x *executor) note(format string, args ...any) {
	x.report.Notes = append(x.report.Notes, fmt.Sprintf(format, args...))
}

func (x *executor) restoreClaude() {
	if st := x.state["claude-code"]; st != "done" && st != "installed" {
		return
	}

	x.progress(Event{ID: "claude-code", Phase: "plugins and MCP"})
	notes, errs := ai.RestoreClaude(x.ctx, x.r, x.plan.Root)
	x.report.ManualSecrets = append(x.report.ManualSecrets, notes...)

	for _, err := range errs {
		x.report.Notes = append(x.report.Notes, err.Error())
	}
}

// postInstall runs only for apps installed in this run, so reruns stay side-effect free.
func (x *executor) postInstall() {
	for _, s := range x.plan.Steps {
		if x.state[s.App.ID] != "done" {
			continue
		}

		for _, c := range s.App.PostInstall {
			x.progress(Event{ID: s.App.ID, Phase: "post-install"})

			if _, err := x.r.Run(x.ctx, runner.Cmd{Shell: c}); err != nil {
				x.report.Notes = append(x.report.Notes, fmt.Sprintf("%s post-install %q: %v", s.App.ID, c, err))
			}
		}
	}
}

func (x *executor) failedDep(app config.App) string {
	for _, dep := range app.Requires {
		// A disabled dependency is the user's call, not a failure.
		if st, ok := x.state[dep]; ok && st != "done" && st != "installed" && st != "disabled" {
			return dep
		}
	}

	return ""
}

func (x *executor) done(id string) {
	x.state[id] = "done"
	x.report.Done = append(x.report.Done, Outcome{ID: id})
}

func (x *executor) skip(id, reason string) {
	x.state[id] = "skipped"
	x.report.Skipped = append(x.report.Skipped, Outcome{ID: id, Reason: reason})
}

func (x *executor) fail(id, reason string, err error) {
	x.state[id] = "failed"
	o := Outcome{ID: id, Reason: reason}
	var ce *runner.CmdError

	if errors.As(err, &ce) {
		o.StderrTail = ce.StderrTail
	} else if err != nil {
		o.Reason += ": " + err.Error()
	}

	x.report.Failed = append(x.report.Failed, o)
}
