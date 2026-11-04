package snap

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

const snapdSocket = "/run/snapd.socket"

// System snaps: bases, runtimes, themes and what Ubuntu preinstalls.
var system = regexp.MustCompile(`^(core\d*|bare|snapd.*|gnome-\d.*|gtk-common-themes|mesa-\d+|kf\d+-.*|snap-store|firmware-updater)$`)

type Snap struct{}

func (Snap) Name() string { return "snap" }

// Available does not require the binary: snapd may be installed earlier in the same run.
func (Snap) Available(p platform.Platform) bool {
	return p.OS == "linux" && (!p.WSL || p.Systemd)
}

func (Snap) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	if !adapter.HasCmd("snap") {
		return nil, nil
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "snap", Args: []string{"list"}})

	if err != nil {
		return nil, err
	}

	var found []adapter.Detected

	for i, l := range adapter.Lines(out) {
		f := strings.Fields(l)

		if i == 0 || len(f) < 6 || system.MatchString(f[0]) || f[5] == "base" {
			continue
		}

		// A snap installed from a local file has no store channel to reinstall from.
		if f[3] == "-" {
			meta := map[string]string{"origin": "installed from a local .snap file; add an install method"}
			found = append(found, adapter.Detected{Manager: "unknown", Name: f[0], Meta: meta})

			continue
		}

		spec := config.InstallSpec{Snap: f[0], Classic: strings.Contains(f[5], "classic")}

		if ch := f[3]; ch != "latest/stable" && !strings.HasSuffix(ch, "/…") {
			spec.Channel = ch
		}

		found = append(found, adapter.Detected{Manager: "snap", Name: f[0], Spec: spec})
	}

	return found, nil
}

func (Snap) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	if !running() {
		return false, nil
	}

	_, err := r.Output(ctx, runner.Cmd{Name: "snap", Args: []string{"list", s.Snap}})

	return err == nil, nil
}

func (Snap) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	if !running() && !r.DryRun {
		return errors.New("snapd is not running (containers and minimal systems have no snapd)")
	}

	args := []string{"install", s.Snap}

	if s.Classic {
		args = append(args, "--classic")
	}

	if s.Channel != "" {
		args = append(args, "--channel="+s.Channel)
	}

	_, err := r.Run(ctx, runner.Cmd{Name: "snap", Args: args, Sudo: true})

	return err
}

// running avoids the client's long timeout when snapd is absent.
func running() bool {
	_, err := os.Stat(snapdSocket)

	return err == nil
}
