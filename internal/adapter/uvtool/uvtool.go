package uvtool

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
)

const toolsDir = "~/.local/share/uv/tools"

type UvTool struct{}

type receipt struct {
	Tool struct {
		Requirements []struct {
			Name   string   `toml:"name"`
			Extras []string `toml:"extras"`
			Git    string   `toml:"git"`
			URL    string   `toml:"url"`
		} `toml:"requirements"`
		Python      string `toml:"python"`
		Entrypoints []struct {
			Name string `toml:"name"`
		} `toml:"entrypoints"`
	} `toml:"tool"`
}

func (UvTool) Name() string { return "uvtool" }

func (UvTool) Available(p platform.Platform) bool {
	return p.OS == "linux" || p.OS == "darwin"
}

// Scan reads uv's install receipts, which keep extras, git sources and the python pin.
func (UvTool) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	receipts, _ := filepath.Glob(adapter.Home(toolsDir + "/*/uv-receipt.toml"))
	var found []adapter.Detected

	for _, path := range receipts {
		data, err := os.ReadFile(path)

		if err != nil {
			return nil, err
		}

		var rc receipt

		if err := toml.Unmarshal(data, &rc); err != nil || len(rc.Tool.Requirements) == 0 {
			continue
		}

		spec := config.InstallSpec{UvTool: requirement(rc)}

		if len(rc.Tool.Entrypoints) > 0 {
			spec.Bin = rc.Tool.Entrypoints[0].Name
		}

		name := filepath.Base(filepath.Dir(path))
		found = append(found, adapter.Detected{Manager: "uvtool", Name: name, Spec: spec, Requires: []string{"uv"}})
	}

	return found, nil
}

func (UvTool) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	name, _, _ := strings.Cut(strings.Fields(s.UvTool)[0], "[")

	if _, err := os.Stat(adapter.Home(toolsDir + "/" + name)); err == nil {
		return true, nil
	}

	return adapter.BinExists(s.Bin), nil
}

func (UvTool) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Name: "uv", Args: append([]string{"tool", "install"}, strings.Fields(s.UvTool)...)})

	return err
}

// requirement renders the receipt back into `uv tool install` arguments.
func requirement(rc receipt) string {
	req := rc.Tool.Requirements[0]
	spec := req.Name

	if len(req.Extras) > 0 {
		spec += "[" + strings.Join(req.Extras, ",") + "]"
	}

	switch {
	case req.Git != "":
		spec = "git+" + secrets.StripUserinfo(req.Git)
	case req.URL != "":
		spec = secrets.StripUserinfo(req.URL)
	}

	// An interpreter path is machine-specific; only version pins travel.
	if rc.Tool.Python != "" && !strings.HasPrefix(rc.Tool.Python, "/") {
		spec += " --python " + rc.Tool.Python
	}

	return spec
}
