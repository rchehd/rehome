package apt

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
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

func (Apt) Versions(ctx context.Context, r *runner.Runner) map[string]lock.Entry {
	out, err := r.Output(ctx, runner.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f=${binary:Package}\t${Package}\t${db:Status-Status}\t${Version}\n"}})

	if err != nil {
		return nil
	}

	entries := map[string]lock.Entry{}

	for _, l := range adapter.Lines(out) {
		if f := strings.Split(l, "\t"); len(f) == 4 && f[2] == "installed" {
			entries[f[0]] = lock.Entry{Version: f[3]}
			entries[f[1]] = lock.Entry{Version: f[3]}
		}
	}

	return entries
}

func (Apt) InstallVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) error {
	_, err := r.Run(ctx, AptGet("install", "-y", "--allow-downgrades", "--", s.Apt+"="+e.Version))

	return err
}

// HasVersion looks the version up in apt-cache madison; an unreadable index counts as available.
func (Apt) HasVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) bool {
	out, err := r.Output(ctx, runner.Cmd{Name: "apt-cache", Args: []string{"madison", "--", s.Apt}})

	if err != nil {
		return true
	}

	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Split(line, "|"); len(fields) > 1 && strings.TrimSpace(fields[1]) == e.Version {
			return true
		}
	}

	return false
}
