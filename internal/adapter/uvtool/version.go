package uvtool

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
)

func (UvTool) Version(ctx context.Context, r *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "uv", Args: []string{"tool", "list"}})

	if err != nil {
		return lock.Entry{}, nil
	}

	return lock.Entry{Version: toolVersion(out, toolName(s))}, nil
}

// InstallVersion pins only registry tools; git and URL sources carry their own revision.
func (UvTool) InstallVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) error {
	args := strings.Fields(s.UvTool)

	if !strings.Contains(args[0], "://") && !strings.HasPrefix(args[0], "git+") {
		args[0] += "==" + e.Version
	}

	_, err := r.Run(ctx, runner.Cmd{Name: "uv", Args: append([]string{"tool", "install"}, args...)})

	return err
}

func toolName(s config.InstallSpec) string {
	name, _, _ := strings.Cut(strings.Fields(s.UvTool)[0], "[")

	return name
}

// toolVersion reads "name v1.2.3" header lines of `uv tool list`.
func toolVersion(out, name string) string {
	for _, l := range adapter.Lines(out) {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == name {
			return strings.TrimPrefix(f[1], "v")
		}
	}

	return ""
}
