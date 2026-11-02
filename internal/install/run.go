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
}

type Event struct {
	Index, Total int
	ID, Phase    string
}

type Outcome struct {
	ID, Reason, StderrTail string
}

type Report struct {
	Done, Skipped, Failed []Outcome
	KeptLocal             []files.ApplyResult
	ManualSecrets         []string
	Notes                 []string
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
	x.restoreClaude()
	x.postInstall()

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
			x.applyFiles(s)
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
		x.state[id] = "done"

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

	if err := s.Adapter.Install(x.ctx, x.r, s.Spec); err != nil {
		reason := "install failed"

		if removed := x.unbreakApt(s); len(removed) > 0 {
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

// batchApt installs plain repo packages in one apt-get call; on failure each is retried alone.
func (x *executor) batchApt(failedRepos map[string]bool) {
	var pkgs, ids []string

	for _, s := range x.plan.Steps {
		if s.Status == StatusInstall && s.Spec.Apt != "" && s.Spec.DebURL == "" && len(s.App.Requires) == 0 && !failedRepos[s.Spec.Repo] {
			pkgs = append(pkgs, s.Spec.Apt)
			ids = append(ids, s.App.ID)
		}
	}

	if len(pkgs) < 2 {
		return
	}

	x.progress(Event{Phase: fmt.Sprintf("apt: %d packages", len(pkgs))})

	// On failure every package is retried alone, after removing what the batch left broken.
	if _, err := x.r.Run(x.ctx, apt.AptGet(append([]string{"install", "-y", "--"}, pkgs...)...)); err != nil {
		if removed := x.unbreakApt(Step{Spec: config.InstallSpec{Apt: "batch"}}); len(removed) > 0 {
			x.note("removed half-configured packages %v after their setup failed", removed)
		}

		return
	}

	for _, id := range ids {
		x.done(id)
	}
}

func (x *executor) applyFiles(s Step) {
	if s.App.Files == "" {
		return
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

	for _, res := range results {
		if res.Status == files.StatusKeptLocal {
			x.report.KeptLocal = append(x.report.KeptLocal, res)
		}

		if res.Redacted {
			x.report.ManualSecrets = append(x.report.ManualSecrets, res.Dest)
		}
	}
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
func (x *executor) unbreakApt(s Step) []string {
	if s.Spec.Apt == "" || x.r.DryRun {
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
