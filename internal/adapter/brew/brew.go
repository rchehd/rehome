package brew

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

// Brew handles Homebrew formulae, Cask handles casks; both share these helpers.
type Brew struct{}

type Cask struct{}

func (Brew) Name() string { return "brew" }
func (Cask) Name() string { return "cask" }

func (Brew) Available(p platform.Platform) bool { return p.OS == "darwin" }
func (Cask) Available(p platform.Platform) bool { return p.OS == "darwin" }

// Scan lists formulae installed on request (not as dependencies), with their tap.
func (Brew) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	if !adapter.HasCmd("brew") {
		return nil, nil
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "brew", Args: []string{"leaves", "--installed-on-request"}})

	if err != nil {
		return nil, err
	}

	var found []adapter.Detected

	for _, full := range adapter.Lines(out) {
		tap, name := splitTap(full)
		found = append(found, adapter.Detected{Manager: "brew", Name: name, Spec: config.InstallSpec{Brew: name, Tap: tap}})
	}

	return found, nil
}

func (Cask) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	if !adapter.HasCmd("brew") {
		return nil, nil
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "brew", Args: []string{"list", "--cask", "--full-name"}})

	if err != nil {
		return nil, err
	}

	var found []adapter.Detected

	for _, full := range adapter.Lines(out) {
		tap, name := splitTap(full)
		found = append(found, adapter.Detected{Manager: "cask", Name: name, Spec: config.InstallSpec{Cask: name, Tap: tap}})
	}

	return found, nil
}

func (Brew) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	return installedVersion(ctx, r, s.Brew, false) != "", nil
}

func (Cask) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	return installedVersion(ctx, r, s.Cask, true) != "", nil
}

func (Brew) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	return install(ctx, r, s.Tap, s.Brew, false)
}

func (Cask) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	return install(ctx, r, s.Tap, s.Cask, true)
}

// Version is the installed version; Homebrew cannot install an arbitrary old one, so
// there is no Pinner — versioned formulae (node@20) carry the version in their name.
func (Brew) Version(ctx context.Context, r *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	return lock.Entry{Version: installedVersion(ctx, r, s.Brew, false)}, nil
}

func (Cask) Version(ctx context.Context, r *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	return lock.Entry{Version: installedVersion(ctx, r, s.Cask, true)}, nil
}

func install(ctx context.Context, r *runner.Runner, tap, name string, cask bool) error {
	if tap != "" {
		if _, err := r.Run(ctx, runner.Cmd{Name: "brew", Args: []string{"tap", tap}}); err != nil {
			return err
		}
	}

	args := []string{"install"}

	if cask {
		args = append(args, "--cask")
	}

	_, err := r.Run(ctx, runner.Cmd{Name: "brew", Args: append(args, qualified(tap, name)), Env: quiet})

	return err
}

var quiet = []string{"HOMEBREW_NO_INSTALL_CLEANUP=1", "HOMEBREW_NO_ENV_HINTS=1"}

func installedVersion(ctx context.Context, r *runner.Runner, name string, cask bool) string {
	args := []string{"list", "--versions"}

	if cask {
		args = append(args, "--cask")
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "brew", Args: append(args, name)})

	if err != nil {
		return ""
	}

	return parseVersion(out, name)
}

// parseVersion reads "name 1.2.3 [1.2.2]" from `brew list --versions`; the first is current.
func parseVersion(out, name string) string {
	for _, l := range adapter.Lines(out) {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == name {
			return f[1]
		}
	}

	return ""
}

// splitTap turns "user/tap/name" into its tap and name; core formulae have no tap.
func splitTap(full string) (string, string) {
	parts := strings.Split(full, "/")

	if len(parts) == 3 {
		return parts[0] + "/" + parts[1], parts[2]
	}

	return "", full
}

func qualified(tap, name string) string {
	if tap == "" {
		return name
	}

	return tap + "/" + name
}
