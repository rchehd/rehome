package pipx

import (
	"context"
	"encoding/json"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
)

func (Pipx) Version(ctx context.Context, r *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "pipx", Args: []string{"list", "--json"}})

	if err != nil {
		return lock.Entry{}, nil
	}

	return lock.Entry{Version: pipxVersion(out, s.Pipx)}, nil
}

func (Pipx) InstallVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) error {
	_, err := r.Run(ctx, runner.Cmd{Name: "pipx", Args: []string{"install", "--", s.Pipx + "==" + e.Version}})

	return err
}

func pipxVersion(out, pkg string) string {
	var list struct {
		Venvs map[string]struct {
			Metadata struct {
				MainPackage struct {
					Package        string `json:"package"`
					PackageOrURL   string `json:"package_or_url"`
					PackageVersion string `json:"package_version"`
				} `json:"main_package"`
			} `json:"metadata"`
		} `json:"venvs"`
	}

	if json.Unmarshal([]byte(out), &list) != nil {
		return ""
	}

	for _, v := range list.Venvs {
		mp := v.Metadata.MainPackage

		if mp.Package == pkg || mp.PackageOrURL == pkg {
			return mp.PackageVersion
		}
	}

	return ""
}
