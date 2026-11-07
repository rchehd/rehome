package snap

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
)

// Version records the snap revision: that is what `snap install --revision` accepts.
func (Snap) Version(ctx context.Context, r *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	if !running() {
		return lock.Entry{}, nil
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "snap", Args: []string{"list", s.Snap}})

	if err != nil {
		return lock.Entry{}, nil
	}

	return lock.Entry{Version: revision(out, s.Snap)}, nil
}

func (Snap) Versions(ctx context.Context, r *runner.Runner) map[string]lock.Entry {
	entries := map[string]lock.Entry{}

	if !running() {
		return entries
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "snap", Args: []string{"list"}})

	if err != nil {
		return nil
	}

	for _, l := range adapter.Lines(out) {
		if f := strings.Fields(l); len(f) >= 3 && f[0] != "Name" {
			entries[f[0]] = lock.Entry{Version: f[2]}
		}
	}

	return entries
}

func (Snap) InstallVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) error {
	args := []string{"install", s.Snap, "--revision=" + e.Version}

	if s.Classic {
		args = append(args, "--classic")
	}

	_, err := r.Run(ctx, runner.Cmd{Name: "snap", Args: args, Sudo: true})

	return err
}

// revision reads the Rev column of `snap list <name>`.
func revision(out, name string) string {
	for _, l := range adapter.Lines(out) {
		if f := strings.Fields(l); len(f) >= 3 && f[0] == name {
			return f[2]
		}
	}

	return ""
}
