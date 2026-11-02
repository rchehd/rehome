package script

import (
	"context"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

// Script runs installer commands; detection is done by the catalog and the binaries resolver.
type Script struct{}

func (Script) Name() string { return "script" }

func (Script) Available(platform.Platform) bool { return true }

func (Script) Scan(context.Context, *runner.Runner) ([]adapter.Detected, error) {
	return nil, nil
}

func (Script) Installed(_ context.Context, _ *runner.Runner, s config.InstallSpec) (bool, error) {
	return adapter.BinExists(s.Bin), nil
}

func (Script) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Shell: s.Script})

	return err
}
