// Package app holds the operations shared by the CLI, its --json output and the MCP server.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/node"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

type Options struct {
	// Config is the --config value (path, folder or URL); empty uses the linked repo.
	Config string
	// Version is this rehome's version, recorded in the node.
	Version      string
	Runner       *runner.Runner
	HarnessData  bool
	Sessions     bool
	SessionPaths map[string]string
	// Log receives progress lines and warnings; nil discards them.
	Log io.Writer
}

func (o Options) logf(format string, args ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, format+"\n", args...)
	}
}

// LoadConfig resolves, loads and validates the config, warning when it comes from a URL.
func LoadConfig(o Options) (*config.Config, string, error) {
	source, err := config.Resolve(o.Config)

	if err != nil {
		return nil, "", err
	}

	if config.IsURL(source) {
		o.logf("warning: this config is not from your repo; review every script before installing")
	}

	cfg, root, err := config.Load(source)

	if err != nil {
		return nil, "", err
	}

	if errs := cfg.Validate(); len(errs) > 0 {
		return nil, "", fmt.Errorf("invalid config: %w", errors.Join(errs...))
	}

	return cfg, root, nil
}

// InstallRequest is what install was asked for, with nodes named by name or id.
type InstallRequest struct {
	IDs          []string
	From         string
	Prefer       []string
	IncludeStale bool
	NoSettings   bool
	// Choose takes an app from the named node (a choice in the picker).
	Choose map[string]string
}

// Args are the install arguments that repeat the request in a terminal.
func (r InstallRequest) Args() []string {
	args := slices.Clone(r.IDs)

	if len(args) == 0 && r.From == "" {
		args = []string{"--all"}
	}

	if r.From != "" {
		args = append(args, "--from", runner.Quote(r.From))
	}

	if len(r.Prefer) > 0 {
		args = append(args, "--prefer", runner.Quote(strings.Join(r.Prefer, ",")))
	}

	if r.IncludeStale {
		args = append(args, "--include-stale")
	}

	if r.NoSettings {
		args = append(args, "--no-settings")
	}

	return args
}

// InstallInput is everything needed to build an install plan: the apps picked from the nodes,
// with their versions and settings.
type InstallInput struct {
	Config    *config.Config
	Root      string
	FileRoots map[string]string
	Platform  platform.Platform
	Lock      *lock.Lock
	Pick      *install.Pick
	Workspace *Workspace
}

func LoadInstall(o Options, req InstallRequest) (*InstallInput, error) {
	w, err := LoadWorkspace(o)

	if err != nil {
		return nil, err
	}

	nodes := w.Live()

	if req.IncludeStale {
		nodes = w.All()
	}

	r := install.Request{IDs: req.IDs, NoSettings: req.NoSettings}

	if req.From != "" {
		if r.From = node.Find(w.All(), req.From); r.From == nil {
			return nil, fmt.Errorf("unknown node %q (rehome nodes lists them)", req.From)
		}
	}

	for _, ref := range req.Prefer {
		n := node.Find(nodes, ref)

		if n == nil {
			return nil, fmt.Errorf("unknown or stale node %q in --prefer", ref)
		}

		r.Prefer = append(r.Prefer, n)
	}

	for id, ref := range req.Choose {
		n := node.Find(nodes, ref)

		if n == nil {
			return nil, fmt.Errorf("unknown or stale node %q chosen for %s", ref, id)
		}

		if r.Choose == nil {
			r.Choose = map[string]*node.Node{}
		}

		r.Choose[id] = n
	}

	pick, err := install.Select(w.Config, nodes, w.Platform.Key(), w.Platform.FileKey(), r)

	if err != nil {
		return nil, err
	}

	cfg, roots := w.WithFiles(pick.Files)

	return &InstallInput{Config: cfg, Root: w.Root, FileRoots: roots, Platform: w.Platform, Lock: pick.Lock, Pick: pick, Workspace: w}, nil
}

// BuildPlan plans the picked apps; sel.IDs narrows them further (a checklist choice).
func BuildPlan(ctx context.Context, o Options, in *InstallInput, sel install.Selection) (*install.Plan, error) {
	o.logf("checking what is installed …")

	if sel.IDs == nil {
		sel.IDs = append([]string{}, in.Pick.IDs...)
	}

	plan, err := install.Build(ctx, o.Runner, in.Config, in.Root, in.Platform, sel, in.Lock)

	if err != nil {
		return nil, err
	}

	plan.FileRoots = in.FileRoots
	plan.From = in.Pick.From()
	plan.Conflicts = in.Pick.Conflicts
	plan.Skipped = in.Pick.Skipped
	plan.Recorded = in.Workspace.Record

	return plan, nil
}

// ConfigPath is the local config file commands edit: an explicit .yml path, else rehome.yml
// in the config folder (the linked repo by default).
func ConfigPath(o Options) (string, error) {
	if ext := filepath.Ext(o.Config); ext == ".yml" || ext == ".yaml" {
		return o.Config, nil
	}

	root, err := config.ResolveRoot(o.Config)

	if err != nil {
		return "", err
	}

	return filepath.Join(root, config.FileName), nil
}
