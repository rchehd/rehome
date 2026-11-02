package binaries

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
)

// Plugin directories that are usually git clones, with the app they extend.
var cloneDirs = map[string]string{
	"~/.oh-my-zsh/custom/plugins/*": "oh-my-zsh",
	"~/.oh-my-zsh/custom/themes/*":  "oh-my-zsh",
	"~/.tmux/plugins/*":             "tmux",
}

// GitClones turns cloned plugins into script installs that clone them again.
func GitClones(ctx context.Context, r *runner.Runner) []adapter.Detected {
	var found []adapter.Detected

	for pattern, parent := range cloneDirs {
		dirs, _ := filepath.Glob(adapter.Home(pattern) + "/.git")

		for _, gitDir := range dirs {
			dir := filepath.Dir(gitDir)
			url, err := r.Output(ctx, runner.Cmd{Name: "git", Args: []string{"-C", dir, "remote", "get-url", "origin"}})

			if err != nil || strings.TrimSpace(url) == "" {
				continue
			}

			dest := adapter.Tilde(dir)
			clean := secrets.StripUserinfo(strings.TrimSpace(url))
			script := "git clone --depth 1 " + runner.Quote(clean) + ` "$HOME"/` + runner.Quote(strings.TrimPrefix(dest, "~/"))
			found = append(found, adapter.Detected{
				Manager:  "script",
				Name:     parent + "-" + filepath.Base(dir),
				Spec:     config.InstallSpec{Script: script, Bin: dest},
				Requires: []string{parent},
			})
		}
	}

	return found
}
