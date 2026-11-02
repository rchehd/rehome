package script

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/npm"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
)

const nvmNode = "/.nvm/versions/node/"

// Version knows two kinds of script installs: nvm's node (by version dir) and git clones
// (by commit). Other installers have no version to read.
func (Script) Version(ctx context.Context, r *runner.Runner, s config.InstallSpec) (lock.Entry, error) {
	if strings.Contains(s.Bin, nvmNode) {
		return lock.Entry{Version: newestNode(s.Bin)}, nil
	}

	if dir := cloneDir(s); dir != "" {
		out, err := r.Output(ctx, runner.Cmd{Name: "git", Args: []string{"-C", dir, "rev-parse", "HEAD"}})

		if err == nil {
			return lock.Entry{Commit: strings.TrimSpace(out)}, nil
		}
	}

	return lock.Entry{}, nil
}

func (Script) InstallVersion(ctx context.Context, r *runner.Runner, s config.InstallSpec, e lock.Entry) error {
	switch {
	case strings.Contains(s.Bin, nvmNode) && e.Version != "":
		v := runner.Quote(e.Version)
		_, err := r.Run(ctx, runner.Cmd{Shell: npm.SourceNvm + "nvm install " + v + " && nvm alias default " + v})

		return err
	case e.Commit != "" && strings.HasPrefix(s.Script, "git clone"):
		if _, err := r.Run(ctx, runner.Cmd{Shell: strings.Replace(s.Script, " --depth 1", "", 1)}); err != nil {
			return err
		}

		_, err := r.Run(ctx, runner.Cmd{Name: "git", Args: []string{"-C", adapter.Home(s.Bin), "checkout", "-q", e.Commit}})

		return err
	}

	return Script{}.Install(ctx, r, s)
}

// newestNode picks the highest installed version matching the bin glob.
func newestNode(bin string) string {
	matches, _ := filepath.Glob(adapter.Home(bin))
	best := ""

	for _, m := range matches {
		rel := m[strings.Index(m, nvmNode)+len(nvmNode):]
		v := strings.TrimPrefix(strings.SplitN(rel, "/", 2)[0], "v")

		if best == "" || lock.Compare("node", v, best) > 0 {
			best = v
		}
	}

	return best
}

func cloneDir(s config.InstallSpec) string {
	if !strings.HasPrefix(s.Script, "git clone") || s.Bin == "" {
		return ""
	}

	dir := adapter.Home(s.Bin)

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return ""
	}

	return dir
}
