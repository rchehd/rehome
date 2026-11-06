package npm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

const (
	nvmDir     = "~/.nvm"
	nvmInstall = "curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash"
	// Prefix makes nvm's node and npm available in non-interactive shells.
	SourceNvm = `[ -s "$HOME/.nvm/nvm.sh" ] && . "$HOME/.nvm/nvm.sh"; `
)

var bundled = []string{"npm", "corepack"}

type Npm struct{}

func (Npm) Name() string { return "npm" }

func (Npm) Available(p platform.Platform) bool {
	return p.OS == "linux" || p.OS == "darwin"
}

// Scan reports nvm, its default node and every global npm package.
func (Npm) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	var found []adapter.Detected
	var requires []string

	if version := defaultNode(); version != "" {
		// Aliases like lts/* or node name no version directory; any installed node then counts.
		bin := nvmDir + "/versions/node/*/bin/node"

		if strings.Trim(version, "0123456789.") == "" {
			bin = nvmDir + "/versions/node/v" + version + "*/bin/node"
		}

		found = append(found,
			adapter.Detected{Manager: "script", Name: "nvm", Spec: config.InstallSpec{Script: nvmInstall, Bin: nvmDir + "/nvm.sh"}},
			adapter.Detected{
				Manager:  "script",
				Name:     "node",
				Spec:     config.InstallSpec{Script: SourceNvm + fmt.Sprintf("nvm install %s && nvm alias default %s", runner.Quote(version), runner.Quote(version)), Bin: bin},
				Requires: []string{"nvm"},
			},
		)
		requires = []string{"node"}
	}

	pkgs, err := globals(ctx, r)

	if err != nil {
		return found, err
	}

	for _, p := range pkgs {
		found = append(found, adapter.Detected{Manager: "npm", Name: p, Spec: config.InstallSpec{Npm: p}, Requires: requires})
	}

	return found, nil
}

// Installed looks for the package in every global node_modules npm may use.
func (Npm) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	if adapter.BinExists(s.Bin) {
		return true, nil
	}

	for _, root := range globalRoots {
		if matches, _ := filepath.Glob(adapter.Home(root) + "/" + s.Npm + "/package.json"); len(matches) > 0 {
			return true, nil
		}
	}

	return false, nil
}

var globalRoots = []string{
	"~/.local/lib/node_modules", "~/.nvm/versions/node/*/lib/node_modules",
	"/usr/lib/node_modules", "/usr/local/lib/node_modules", "/opt/homebrew/lib/node_modules",
}

func (Npm) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Shell: SourceNvm + "npm install -g -- " + runner.Quote(s.Npm)})

	return err
}

// globals merges the packages of the npm on PATH with those of every nvm node version.
func globals(ctx context.Context, r *runner.Runner) ([]string, error) {
	set := map[string]bool{}

	if adapter.HasCmd("npm") {
		out, err := r.Output(ctx, runner.Cmd{Name: "npm", Args: []string{"ls", "-g", "--depth=0", "--json"}})

		if err == nil {
			var tree struct {
				Dependencies map[string]json.RawMessage `json:"dependencies"`
			}

			if err := json.Unmarshal([]byte(out), &tree); err != nil {
				return nil, err
			}

			for name := range tree.Dependencies {
				set[name] = true
			}
		}
	}

	versions, _ := filepath.Glob(adapter.Home(nvmDir + "/versions/node/*/lib/node_modules"))

	for _, dir := range versions {
		for _, name := range moduleNames(dir) {
			set[name] = true
		}
	}

	var pkgs []string

	for name := range set {
		if !slices.Contains(bundled, name) {
			pkgs = append(pkgs, name)
		}
	}

	slices.Sort(pkgs)

	return pkgs, nil
}

func moduleNames(dir string) []string {
	entries, err := os.ReadDir(dir)

	if err != nil {
		return nil
	}

	var names []string

	for _, e := range entries {
		name := e.Name()

		if strings.HasPrefix(name, ".") {
			continue
		}

		if !strings.HasPrefix(name, "@") {
			names = append(names, name)

			continue
		}

		scoped, _ := os.ReadDir(filepath.Join(dir, name))

		for _, s := range scoped {
			names = append(names, name+"/"+s.Name())
		}
	}

	return names
}

// defaultNode returns nvm's default alias (e.g. "22"), or "" without nvm.
func defaultNode() string {
	data, err := os.ReadFile(adapter.Home(nvmDir + "/alias/default"))

	if err != nil {
		return ""
	}

	return strings.TrimPrefix(strings.TrimSpace(string(data)), "v")
}

func (Npm) Remove(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Shell: SourceNvm + "npm uninstall -g -- " + runner.Quote(s.Npm)})

	return err
}
