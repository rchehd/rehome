package pipx

import (
	"context"
	"encoding/json"
	"os"
	"sort"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
)

type Pipx struct{}

func (Pipx) Name() string { return "pipx" }

func (Pipx) Available(p platform.Platform) bool {
	return p.OS == "linux" || p.OS == "darwin"
}

func (Pipx) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	if !adapter.HasCmd("pipx") {
		return nil, nil
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "pipx", Args: []string{"list", "--json"}})

	if err != nil {
		return nil, err
	}

	var list struct {
		Venvs map[string]struct {
			Metadata struct {
				MainPackage struct {
					Package      string   `json:"package"`
					PackageOrURL string   `json:"package_or_url"`
					Apps         []string `json:"apps"`
				} `json:"main_package"`
			} `json:"metadata"`
		} `json:"venvs"`
	}

	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, err
	}

	var found []adapter.Detected

	for name, v := range list.Venvs {
		mp := v.Metadata.MainPackage
		spec := config.InstallSpec{Pipx: secrets.StripUserinfo(mp.PackageOrURL)}

		if len(mp.Apps) > 0 {
			spec.Bin = mp.Apps[0]
		}

		found = append(found, adapter.Detected{Manager: "pipx", Name: name, Spec: spec, Requires: []string{"pipx"}})
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })

	return found, nil
}

func (Pipx) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	if _, err := os.Stat(adapter.Home("~/.local/share/pipx/venvs/" + s.Pipx)); err == nil {
		return true, nil
	}

	return adapter.BinExists(s.Bin), nil
}

func (Pipx) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Name: "pipx", Args: []string{"install", s.Pipx}})

	return err
}

func (Pipx) Remove(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Name: "pipx", Args: []string{"uninstall", s.Pipx}})

	return err
}
