package install

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/all"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

const (
	StatusInstall     = "install"
	StatusFilesOnly   = "files-only"
	StatusInstalled   = "skip-installed"
	StatusUnavailable = "unavailable"
	StatusDisabled    = "disabled"
	StatusIgnored     = "ignored"
)

type Step struct {
	App     config.App
	Spec    config.InstallSpec
	Adapter adapter.Adapter
	Status  string
	Reason  string
	// Pin is the exact version to install; empty means the latest.
	Pin lock.Entry
	// Installed is the version present now; Upgrade the newer one the lock offers.
	Installed string
	Upgrade   lock.Entry
}

type Plan struct {
	Steps    []Step
	Config   *config.Config
	Root     string
	Platform platform.Platform
	Lock     *lock.Lock
	Mode     string
}

const (
	ModeLatest = "latest"
	ModeLocked = "locked"
)

// Selection limits the plan to IDs (nil means every enabled app); All reinstalls what is present;
// Mode picks lock versions or the latest; Latest lists apps that take the latest in locked mode.
type Selection struct {
	IDs    []string
	All    bool
	Mode   string
	Latest []string
}

// Build resolves every app to a step in dependency order.
func Build(ctx context.Context, r *runner.Runner, cfg *config.Config, root string, p platform.Platform, sel Selection, lk *lock.Lock) (*Plan, error) {
	if lk == nil {
		lk = lock.New()
	}

	plan := &Plan{Config: cfg, Root: root, Platform: p, Lock: lk, Mode: sel.Mode}

	for _, app := range cfg.Apps {
		if sel.IDs != nil && !slices.Contains(sel.IDs, app.ID) {
			continue
		}

		step := resolve(ctx, r, app, p, sel)

		if cfg.Ignored(app, p.Key(), config.Hostname()) {
			step.Status, step.Reason = StatusIgnored, "ignored on this machine or OS"
		}

		plan.Steps = append(plan.Steps, withVersions(ctx, r, step, lk, p.Key(), sel))
	}

	steps, err := order(plan.Steps)

	if err != nil {
		return nil, err
	}

	plan.Steps = steps

	return plan, nil
}

func resolve(ctx context.Context, r *runner.Runner, app config.App, p platform.Platform, sel Selection) Step {
	step := Step{App: app}

	if sel.IDs == nil && !app.IsEnabled() {
		step.Status = StatusDisabled

		return step
	}

	spec, ok := app.Spec(p.Key())

	if !ok {
		step.Status, step.Reason = StatusUnavailable, "no install method for "+p.Key()

		if len(app.Install) == 0 && app.Files != "" {
			step.Status, step.Reason = StatusFilesOnly, ""
		}

		return step
	}

	a, err := all.ByManager(spec)

	if err != nil || !a.Available(p) {
		step.Status, step.Reason = StatusUnavailable, fmt.Sprintf("manager %s is not available here", managerName(spec))

		return step
	}

	step.Spec, step.Adapter, step.Status = spec, a, StatusInstall

	if !sel.All {
		if installed, _ := a.Installed(ctx, r, spec); installed {
			step.Status = StatusInstalled
		}
	}

	return step
}

// order sorts steps so requirements come first, otherwise by adapter install order.
func order(steps []Step) ([]Step, error) {
	index := map[string]int{}

	for i, s := range steps {
		index[s.App.ID] = i
	}

	rank := func(i int) int { return all.Order(managerName(steps[i].Spec)) }
	indegree := make([]int, len(steps))
	dependents := make([][]int, len(steps))

	for i, s := range steps {
		for _, dep := range s.App.Requires {
			if j, ok := index[dep]; ok {
				indegree[i]++
				dependents[j] = append(dependents[j], i)
			}
		}
	}

	var ready, out []int

	for i := range steps {
		if indegree[i] == 0 {
			ready = append(ready, i)
		}
	}

	for len(ready) > 0 {
		sort.SliceStable(ready, func(a, b int) bool {
			if rank(ready[a]) != rank(ready[b]) {
				return rank(ready[a]) < rank(ready[b])
			}

			return ready[a] < ready[b]
		})

		i := ready[0]
		ready = ready[1:]
		out = append(out, i)

		for _, d := range dependents[i] {
			if indegree[d]--; indegree[d] == 0 {
				ready = append(ready, d)
			}
		}
	}

	if len(out) != len(steps) {
		return nil, fmt.Errorf("requires cycle among selected apps")
	}

	sorted := make([]Step, len(out))

	for k, i := range out {
		sorted[k] = steps[i]
	}

	return sorted, nil
}

func managerName(s config.InstallSpec) string {
	name, _ := s.Manager()

	return name
}

// Counts tallies steps by status.
func (p *Plan) Counts() map[string]int {
	counts := map[string]int{}

	for _, s := range p.Steps {
		counts[s.Status]++
	}

	return counts
}

// Commands lists every script and post-install command the plan would run (spec §15.7).
func (p *Plan) Commands() []string {
	var cmds []string

	for _, s := range p.Steps {
		if s.Status != StatusInstall && s.Status != StatusFilesOnly {
			continue
		}

		if s.Spec.Script != "" {
			cmds = append(cmds, s.App.ID+": "+s.Spec.Script)
		}

		for _, c := range s.App.PostInstall {
			cmds = append(cmds, s.App.ID+" (post-install): "+c)
		}
	}

	return cmds
}
