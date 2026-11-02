package npm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rchehd/rehome/internal/config"
)

func TestVersionPicksNewestGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for dir, v := range map[string]string{
		".local/lib/node_modules/@scope/tool":                     "1.2.0",
		".nvm/versions/node/v22.1.0/lib/node_modules/@scope/tool": "1.10.0",
	} {
		os.MkdirAll(filepath.Join(home, dir), 0o755)
		os.WriteFile(filepath.Join(home, dir, "package.json"), []byte(`{"version":"`+v+`"}`), 0o644)
	}

	e, _ := Npm{}.Version(context.Background(), nil, config.InstallSpec{Npm: "@scope/tool"})

	if e.Version != "1.10.0" {
		t.Errorf("version = %q", e.Version)
	}
}
