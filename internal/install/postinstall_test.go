package install

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

func TestSettingsOnlyPostInstallRunsOnlyOnChange(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	marker := filepath.Join(home, "fc-cache-ran")
	os.MkdirAll(filepath.Join(root, "files/fonts"), 0o755)
	os.WriteFile(filepath.Join(root, "files/fonts/a.ttf"), []byte("font"), 0o644)
	cfg := &config.Config{
		Apps:  []config.App{{ID: "fonts", Files: "fonts", PostInstall: []string{"touch " + marker}}},
		Files: map[string][]config.FileMap{"fonts": {{Src: "files/fonts/a.ttf", Dest: map[string]string{"macos": filepath.Join(home, "a.ttf")}}}},
	}
	plan := &Plan{Config: cfg, Root: root, Platform: platform.Platform{OS: "darwin", ID: "macos"},
		Steps: []Step{{App: cfg.Apps[0], Status: StatusFilesOnly}}}
	run := func() {
		if _, err := Execute(context.Background(), &runner.Runner{}, plan, Options{}, nil); err != nil {
			t.Fatal(err)
		}
	}

	run()

	if _, err := os.Stat(marker); err != nil {
		t.Fatal("post_install did not run when the font was copied")
	}

	os.Remove(marker)
	run()

	if _, err := os.Stat(marker); err == nil {
		t.Error("post_install ran again although nothing changed")
	}
}
