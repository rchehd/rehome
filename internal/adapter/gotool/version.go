package gotool

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
)

func (GoTool) Version(ctx context.Context, r *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "go", Args: []string{"version", "-m", adapter.Home(s.Bin)}})

	if err != nil {
		return lock.Entry{}, nil
	}

	return lock.Entry{Version: moduleVersion(out)}, nil
}

func (GoTool) InstallVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) error {
	_, err := r.Run(ctx, runner.Cmd{Shell: `export PATH="$PATH:/usr/local/go/bin"; go install ` + runner.Quote(s.GoPkg+"@"+e.Version)})

	return err
}

// moduleVersion reads the "mod <path> <version>" line of `go version -m`.
func moduleVersion(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) >= 3 && f[0] == "mod" && f[2] != "(devel)" {
			return f[2]
		}
	}

	return ""
}
