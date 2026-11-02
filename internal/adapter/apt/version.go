package apt

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
)

func (Apt) Version(ctx context.Context, r *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f=${db:Status-Status} ${Version}", "--", s.Apt}})

	if err != nil {
		return lock.Entry{}, nil
	}

	status, version, _ := strings.Cut(strings.TrimSpace(out), " ")

	if status != "installed" {
		return lock.Entry{}, nil
	}

	return lock.Entry{Version: version}, nil
}

func (Apt) InstallVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) error {
	_, err := r.Run(ctx, AptGet("install", "-y", "--allow-downgrades", "--", s.Apt+"="+e.Version))

	return err
}
