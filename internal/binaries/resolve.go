package binaries

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/runner"
)

var binDirs = []string{"~/.local/bin", "/usr/local/bin"}

// Symlink targets under these prefixes belong to a package manager another adapter scans.
var managed = []string{
	"~/.local/share/pipx/", "~/.local/share/uv/", "~/.local/lib/node_modules/", "~/.nvm/",
	"~/.cargo/", "~/go/", "~/.hermes/node/", "/usr/lib/node_modules/", "/snap/",
}

// Resolve identifies binaries that no package manager owns: catalog matches become
// script installs, the rest are reported as unknown. known holds bins already covered.
func Resolve(ctx context.Context, r *runner.Runner, cat *catalog.Catalog, known map[string]bool) ([]adapter.Detected, error) {
	type candidate struct{ name, path, target string }
	var cands []candidate

	for _, dir := range binDirs {
		entries, _ := os.ReadDir(adapter.Home(dir))

		for _, e := range entries {
			path := filepath.Join(adapter.Home(dir), e.Name())
			target, err := filepath.EvalSymlinks(path)

			if err != nil || known[e.Name()] || isManaged(target) {
				continue
			}

			if info, err := os.Stat(target); err != nil || info.IsDir() {
				continue
			}

			cands = append(cands, candidate{e.Name(), path, target})
		}
	}

	targets := make([]string, len(cands))

	for i, c := range cands {
		targets[i] = c.target
	}

	owned := dpkgOwned(ctx, r, targets)
	byID := map[string]adapter.Detected{}

	for _, c := range cands {
		if owned[c.target] {
			continue
		}

		d := adapter.Detected{Manager: "unknown", Name: unknownName(c.name, c.target), Meta: map[string]string{"bin": c.name, "path": adapter.Tilde(c.path)}}

		if e, ok := cat.Match(d); ok {
			d = adapter.Detected{Manager: "script", Name: e.ID, Meta: map[string]string{"bin": c.name}}
		} else if c.target != c.path {
			d.Meta["target"] = adapter.Tilde(c.target)
		}

		if _, dup := byID[d.Name]; !dup {
			byID[d.Name] = d
		}
	}

	opt, err := optApps(ctx, r)

	if err != nil {
		return nil, err
	}

	for _, d := range opt {
		byID[d.Name] = d
	}

	found := make([]adapter.Detected, 0, len(byID))

	for _, d := range byID {
		found = append(found, d)
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })

	return found, nil
}

// unknownName groups binaries of one tool: a target inside ~/.tool/ is named "tool".
func unknownName(bin, target string) string {
	rel := strings.TrimPrefix(adapter.Tilde(target), "~/")

	if rel == adapter.Tilde(target) || strings.HasPrefix(rel, ".local/") {
		return bin
	}

	top, _, _ := strings.Cut(rel, "/")

	return strings.TrimPrefix(top, ".")
}

// optApps reports /opt applications that no package owns.
func optApps(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	dirs, _ := filepath.Glob("/opt/*")
	owned := dpkgOwned(ctx, r, dirs)
	var found []adapter.Detected

	for _, d := range dirs {
		if info, err := os.Stat(d); err != nil || !info.IsDir() || owned[d] {
			continue
		}

		name := strings.ToLower(strings.ReplaceAll(filepath.Base(d), " ", "-"))
		found = append(found, adapter.Detected{Manager: "unknown", Name: "opt-" + name, Meta: map[string]string{"path": d}})
	}

	return found, nil
}

func dpkgOwned(ctx context.Context, r *runner.Runner, paths []string) map[string]bool {
	owned := map[string]bool{}

	if len(paths) == 0 || !adapter.HasCmd("dpkg") {
		return owned
	}

	// dpkg -S exits non-zero when any path is unowned; the owned ones are still printed.
	out, _ := r.Output(ctx, runner.Cmd{Name: "dpkg", Args: append([]string{"-S"}, paths...)})

	for _, l := range adapter.Lines(out) {
		if _, path, ok := strings.Cut(l, ": "); ok {
			for _, p := range strings.Split(path, ", ") {
				owned[p] = true
			}
		}
	}

	return owned
}

func isManaged(target string) bool {
	for _, m := range managed {
		if strings.HasPrefix(target, adapter.Home(m)) || strings.Contains(target, "/node_modules/") {
			return true
		}
	}

	return false
}
