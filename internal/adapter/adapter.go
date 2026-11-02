package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

// Detected is one piece of software a scan found.
type Detected struct {
	Manager string
	Name    string
	// Spec is how to install it again; empty for unknown software.
	Spec     config.InstallSpec
	Requires []string
	Meta     map[string]string
}

type Adapter interface {
	// Name matches config.InstallSpec.Manager().
	Name() string
	Available(p platform.Platform) bool
	Scan(ctx context.Context, r *runner.Runner) ([]Detected, error)
	Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error)
	Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error
}

func HasCmd(name string) bool {
	_, err := runner.LookPath(name)

	return err == nil
}

// BinExists checks a binary name on PATH and in ~/.local/bin, or a path glob with ~.
func BinExists(bin string) bool {
	if bin == "" {
		return false
	}

	if !strings.Contains(bin, "/") {
		if HasCmd(bin) {
			return true
		}

		bin = "~/.local/bin/" + bin
	}

	matches, _ := filepath.Glob(Home(bin))

	return len(matches) > 0
}

// Home expands a leading ~ to the user's home directory.
func Home(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}

	home, err := os.UserHomeDir()

	if err != nil {
		return path
	}

	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// Tilde replaces the home directory prefix with ~ so paths stay portable.
func Tilde(path string) string {
	home, err := os.UserHomeDir()

	if err != nil || !strings.HasPrefix(path, home+"/") {
		return path
	}

	return "~" + strings.TrimPrefix(path, home)
}

// Lines splits command output into trimmed non-empty lines.
func Lines(out string) []string {
	var lines []string

	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}

	return lines
}
