// Package app holds the operations shared by the CLI, its --json output and the MCP server.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

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

// InstallInput is everything needed to build an install plan: settings come from the freshest
// node with each group, versions from the live nodes.
type InstallInput struct {
	Config    *config.Config
	Root      string
	FileRoots map[string]string
	Platform  platform.Platform
	Lock      *lock.Lock
	Workspace *Workspace
}

func LoadInstall(o Options) (*InstallInput, error) {
	w, err := LoadWorkspace(o)

	if err != nil {
		return nil, err
	}

	cfg, roots := w.WithFiles(w.FreshestFiles)

	return &InstallInput{Config: cfg, Root: w.Root, FileRoots: roots, Platform: w.Platform, Lock: node.Versions(w.Live()), Workspace: w}, nil
}

func BuildPlan(ctx context.Context, o Options, in *InstallInput, sel install.Selection) (*install.Plan, error) {
	o.logf("checking what is installed …")
	plan, err := install.Build(ctx, o.Runner, in.Config, in.Root, in.Platform, sel, in.Lock)

	if err != nil {
		return nil, err
	}

	plan.FileRoots = in.FileRoots
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
