package cargo

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
)

func (Cargo) Version(ctx context.Context, r *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "cargo", Args: []string{"install", "--list"}})

	if err != nil {
		return lock.Entry{}, nil
	}

	return lock.Entry{Version: crateVersion(out, s.Cargo)}, nil
}

func (Cargo) InstallVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) error {
	_, err := r.Run(ctx, runner.Cmd{Shell: `[ -s "$HOME/.cargo/env" ] && . "$HOME/.cargo/env"; cargo install --version ` + runner.Quote(e.Version) + " -- " + runner.Quote(s.Cargo)})

	return err
}

// crateVersion reads "name v1.2.3:" lines of `cargo install --list`.
func crateVersion(out, name string) string {
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)

		if len(f) >= 2 && f[0] == name {
			return strings.TrimSuffix(strings.TrimPrefix(f[1], "v"), ":")
		}
	}

	return ""
}
