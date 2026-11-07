package app

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/all"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/install"
)

// RemovePlan is one app to take off this machine, and optionally off every node.
type RemovePlan struct {
	ID         string
	Spec       config.InstallSpec
	Adapter    adapter.Adapter
	Installed  bool
	Everywhere bool
	// Tombstone is the other node's everywhere-removal that led to this plan, if any.
	Tombstone *config.Tombstone
	Workspace *Workspace
}

// Uninstalls reports whether running the plan uninstalls anything, not only edits the node.
func (p *RemovePlan) Uninstalls() bool {
	_, ok := p.Adapter.(adapter.Remover)

	return p.Installed && ok
}

// Command is the manager and package being removed, for the confirmation.
func (p *RemovePlan) Command() string {
	name, pkg := p.Spec.Manager()

	return name + " " + pkg
}

// PrepareRemove finds the app in the recipes or this node and checks whether it is installed.
func PrepareRemove(ctx context.Context, o Options, id string, everywhere bool) (*RemovePlan, error) {
	w, err := LoadWorkspace(o)

	if err != nil {
		return nil, err
	}

	return w.prepareRemove(ctx, o, id, everywhere)
}

func (w *Workspace) prepareRemove(ctx context.Context, o Options, id string, everywhere bool) (*RemovePlan, error) {
	a, known := w.Config.App(id)
	_, recorded := w.Self.Apps[id]

	if !known && !recorded {
		ids := slices.Collect(maps.Keys(w.Self.Apps))

		for _, app := range w.Config.Apps {
			ids = append(ids, app.ID)
		}

		if near := install.Suggest(id, ids); near != "" {
			return nil, fmt.Errorf("unknown app %q; did you mean %s?", id, near)
		}

		return nil, fmt.Errorf("unknown app %q", id)
	}

	p := &RemovePlan{ID: id, Everywhere: everywhere, Workspace: w}

	if !known {
		return p, nil
	}

	spec, ok := a.Spec(w.Platform.Key())

	if !ok {
		return p, nil
	}

	ad, err := all.ByManager(spec)

	if err != nil {
		return p, nil
	}

	p.Spec, p.Adapter = spec, ad
	p.Installed, _ = p.Adapter.Installed(ctx, o.Runner, spec)

	return p, nil
}

// Run uninstalls the app, drops it from this node and, with Everywhere, records the tombstone.
// Notes say what could not be done automatically.
func (p *RemovePlan) Run(ctx context.Context, o Options) ([]string, error) {
	notes := []string{}
	w := p.Workspace

	switch r, ok := p.Adapter.(adapter.Remover); {
	case p.Installed && ok:
		if err := r.Remove(ctx, o.Runner, p.Spec); err != nil {
			return nil, err
		}
	case p.Installed:
		notes = append(notes, fmt.Sprintf("%s was installed by %s, which rehome cannot undo; remove it yourself", p.ID, p.Command()))
	}

	if o.Runner.DryRun {
		return notes, nil
	}

	if _, ok := w.Self.Apps[p.ID]; ok && w.Root != "" {
		delete(w.Self.Apps, p.ID)

		if err := SaveNode(w.Self); err != nil {
			return notes, err
		}
	}

	if p.Everywhere && w.Root != "" {
		t := config.Tombstone{ID: p.ID, By: w.Self.Name, At: time.Now().UTC().Truncate(time.Second)}

		if err := config.AddRemoved(config.FilePath(w.Root), t); err != nil {
			return notes, err
		}
	}

	return notes, nil
}

// Tombstoned are apps another node removed everywhere that are still on this machine.
func (w *Workspace) Tombstoned(ctx context.Context, o Options) []*RemovePlan {
	var plans []*RemovePlan

	for _, t := range w.Config.Removed {
		if t.By == w.Self.Name {
			continue
		}

		p, err := w.prepareRemove(ctx, o, t.ID, false)

		if err != nil {
			continue
		}

		if _, recorded := w.Self.Apps[t.ID]; p.Installed || recorded {
			p.Tombstone = &t
			plans = append(plans, p)
		}
	}

	return plans
}
