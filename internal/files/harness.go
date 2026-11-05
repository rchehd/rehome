package files

import (
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
)

func isHarnessData(dest string) bool {
	rel := adapter.Tilde(filepath.Clean(adapter.Home(dest)))

	for _, dir := range []string{"~/.claude/", "~/.codex/", "~/.cursor/"} {
		if rest, ok := strings.CutPrefix(rel, dir); ok {
			return strings.HasPrefix(rest, "skills/") || strings.HasPrefix(rest, "agents/")
		}
	}

	return strings.HasPrefix(rel, "~/.agents/skills/")
}
