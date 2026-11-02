package gotool

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

const binDir = "~/go/bin"

type GoTool struct{}

func (GoTool) Name() string { return "go" }

func (GoTool) Available(p platform.Platform) bool {
	return p.OS == "linux" || p.OS == "darwin"
}

// Scan reads the main package path embedded in each binary of ~/go/bin.
func (GoTool) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	if !adapter.HasCmd("go") {
		return nil, nil
	}

	bins, _ := filepath.Glob(adapter.Home(binDir + "/*"))
	var found []adapter.Detected

	for _, bin := range bins {
		if info, err := os.Stat(bin); err != nil || info.IsDir() {
			continue
		}

		out, err := r.Output(ctx, runner.Cmd{Name: "go", Args: []string{"version", "-m", bin}})

		if err != nil {
			continue
		}

		for _, l := range strings.Split(out, "\n") {
			f := strings.Fields(l)

			if len(f) >= 2 && f[0] == "path" {
				name := filepath.Base(bin)
				spec := config.InstallSpec{GoPkg: f[1], Bin: binDir + "/" + name}
				found = append(found, adapter.Detected{Manager: "go", Name: name, Spec: spec, Requires: []string{"go"}})

				break
			}
		}
	}

	return found, nil
}

func (GoTool) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	return adapter.BinExists(s.Bin), nil
}

func (GoTool) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Shell: `export PATH="$PATH:/usr/local/go/bin"; go install ` + runner.Quote(s.GoPkg+"@latest")})

	return err
}
