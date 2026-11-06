package cargo

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

type Cargo struct{}

func (Cargo) Name() string { return "cargo" }

func (Cargo) Available(p platform.Platform) bool {
	return p.OS == "linux" || p.OS == "darwin"
}

// Scan parses `cargo install --list`: "name v1.2.3:" followed by indented binaries.
func (Cargo) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	if !adapter.HasCmd("cargo") {
		return nil, nil
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "cargo", Args: []string{"install", "--list"}})

	if err != nil {
		return nil, err
	}

	var found []adapter.Detected

	for _, l := range strings.Split(out, "\n") {
		if l == "" || l[0] == ' ' {
			if bin := strings.TrimSpace(l); bin != "" && len(found) > 0 && found[len(found)-1].Spec.Bin == "" {
				found[len(found)-1].Spec.Bin = bin
			}

			continue
		}

		name, _, _ := strings.Cut(l, " ")
		found = append(found, adapter.Detected{Manager: "cargo", Name: name, Spec: config.InstallSpec{Cargo: name}, Requires: []string{"rustup"}})
	}

	return found, nil
}

func (Cargo) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	return adapter.BinExists(s.Bin) || adapter.BinExists("~/.cargo/bin/"+s.Cargo), nil
}

func (Cargo) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Shell: `[ -s "$HOME/.cargo/env" ] && . "$HOME/.cargo/env"; cargo install -- ` + runner.Quote(s.Cargo)})

	return err
}

func (Cargo) Remove(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Shell: `[ -s "$HOME/.cargo/env" ] && . "$HOME/.cargo/env"; cargo uninstall -- ` + runner.Quote(s.Cargo)})

	return err
}
