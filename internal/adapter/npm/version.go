package npm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
)

// Version reads package.json of the newest global install found.
func (Npm) Version(_ context.Context, _ *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	best := ""

	for _, root := range globalRoots {
		matches, _ := filepath.Glob(adapter.Home(root) + "/" + s.Npm + "/package.json")

		for _, m := range matches {
			if v := packageVersion(m); v != "" && (best == "" || lock.Compare("npm", v, best) > 0) {
				best = v
			}
		}
	}

	return lock.Entry{Version: best}, nil
}

func (Npm) InstallVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) error {
	_, err := r.Run(ctx, runner.Cmd{Shell: SourceNvm + "npm install -g -- " + runner.Quote(s.Npm+"@"+e.Version)})

	return err
}

func packageVersion(path string) string {
	data, err := os.ReadFile(path)

	if err != nil {
		return ""
	}

	var pkg struct {
		Version string `json:"version"`
	}

	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}

	return pkg.Version
}
