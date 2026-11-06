package install

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/all"
	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/resolve"
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
	// Code is the reason code of an unavailable step (ReasonNoRecipe, …).
	Code string
	// Pin is the exact version to install; empty means the latest.
	Pin lock.Entry
	// Installed is the version present now; Upgrade the newer one the source nodes have.
	Installed string
	Upgrade   lock.Entry
	// PinNote says why the same version as the source node cannot be installed.
	PinNote string
	// Mapped says where an installer for this OS was found when the config had none.
	Mapped string
}

type Plan struct {
	Steps  []Step
	Config *config.Config
	// Root holds rehome.yml and the shared apt files; FileRoots maps a settings group to its node folder.
	Root      string
	FileRoots map[string]string
	Platform  platform.Platform
	Lock      *lock.Lock
	Mode      string
	// Reinstall and Offline repeat the selection, so a narrowed plan can be built again.
	Reinstall, Offline bool
	// From maps an app to the node it comes from; Skipped are apps the nodes have that cannot be planned.
	From    map[string]string
	Skipped []Choice
	// Conflicts are apps several nodes have with different versions or settings, mapped to those nodes.
	Conflicts map[string][]string
	// Recorded receives the version of each app installed or found by the run.
	Recorded func(id string, e lock.Entry)
}

// FilesRoot is the folder a settings group's files/ paths resolve against.
func (p *Plan) FilesRoot(group string) string {
	if root, ok := p.FileRoots[group]; ok {
		return root
	}

	return p.Root
}

const (
	ModeLatest = "latest"
	ModeSame   = "same"
)

// Selection limits the plan to IDs (nil means every enabled app); All reinstalls what is present;
// Mode picks the source nodes' versions or the latest; Latest lists apps that take the latest in same mode.
type Selection struct {
	IDs    []string
	All    bool
	Mode   string
	Latest []string
	// Offline skips network lookups (Homebrew API, Repology) when mapping installers.
	Offline bool
	// NoSettings are apps installed without their settings files.
	NoSettings []string
}

// Build resolves every app to a step in dependency order.
func Build(ctx context.Context, r *runner.Runner, cfg *config.Config, root string, p platform.Platform, sel Selection, lk *lock.Lock) (*Plan, error) {
	if lk == nil {
		lk = lock.New()
	}

	plan := &Plan{Config: cfg, Root: root, Platform: p, Lock: lk, Mode: sel.Mode, Reinstall: sel.All, Offline: sel.Offline}
	res := newResolver(r, p, sel.Offline)

	for _, app := range cfg.Apps {
		if sel.IDs != nil && !slices.Contains(sel.IDs, app.ID) {
			continue
		}

		if slices.Contains(sel.NoSettings, app.ID) {
			app.Files = ""
		}

		step := resolveStep(ctx, r, cfg, app, p, sel, res)

		if cfg.Ignored(app, p.Key(), config.ThisNode()) {
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

func resolveStep(ctx context.Context, r *runner.Runner, cfg *config.Config, app config.App, p platform.Platform, sel Selection, res *resolve.Resolver) Step {
	step := Step{App: app}

	if sel.IDs == nil && !app.IsEnabled() {
		step.Status = StatusDisabled

		return step
	}

	spec, a, mapped, why := compatible(ctx, cfg, app, p, res)
	step.Mapped = mapped

	if why.Code != "" {
		step.Status, step.Code, step.Reason = StatusUnavailable, why.Code, why.Text

		if why.Code == ReasonNoRecipe && len(app.Install) == 0 && app.Files != "" {
			step.Status, step.Code, step.Reason = StatusFilesOnly, "", ""
		}

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

func newResolver(r *runner.Runner, p platform.Platform, offline bool) *resolve.Resolver {
	res := &resolve.Resolver{PlatformKey: p.Key()}

	if cat, err := catalog.Load(); err == nil {
		res.Catalog = cat
	}

	switch p.Key() {
	case "macos":
		res.Index = resolve.BrewIndex{Runner: r, Offline: offline}
	case "debian":
		res.Index = resolve.AptIndex{Runner: r}
	}

	if !offline {
		res.Repology = resolve.NewRepology()
	}

	return res
}
