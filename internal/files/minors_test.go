package files

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rchehd/rehome/internal/config"
)

func TestSelectFollowsSymlinkedDirs(t *testing.T) {
	home := t.TempDir()
	dotfiles := filepath.Join(home, "dotfiles", "nvim")
	put(t, filepath.Join(dotfiles, "init.lua"), "vim.o.number = true")
	put(t, filepath.Join(dotfiles, "lua", "plugins.lua"), "return {}")
	os.MkdirAll(filepath.Join(home, ".config"), 0o755)
	os.Symlink(dotfiles, filepath.Join(home, ".config", "nvim"))
	// A loop must not hang the walk.
	os.Symlink(dotfiles, filepath.Join(dotfiles, "loop"))

	rels, err := Select(home, config.FileRule{Include: []string{".config/nvim/**"}})

	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{".config/nvim/init.lua", ".config/nvim/lua/plugins.lua"} {
		if !slices.Contains(rels, want) {
			t.Errorf("rels %v miss %s", rels, want)
		}
	}
}

func TestPrivateModeTravelsThroughConfig(t *testing.T) {
	base, root, home := t.TempDir(), t.TempDir(), t.TempDir()
	src := filepath.Join(base, ".npmrc")
	os.WriteFile(src, []byte("//registry/:_authToken=abcdef1234567890\n"), 0o600)
	rules := map[string]config.FileRule{"linux": {Base: base, Include: []string{".npmrc"}}}
	changes, maps, err := CaptureRules("npm", rules, "linux", root, false)

	if err != nil || len(maps) != 1 || maps[0].Mode != "0600" {
		t.Fatalf("maps %v err %v", maps, err)
	}

	WriteChanges(root, changes)
	// git checkouts come back 0644; the mode must come from the config, not the repo file.
	os.Chmod(filepath.Join(root, maps[0].Src), 0o644)
	dest := filepath.Join(home, ".npmrc")
	maps[0].Dest["linux"] = dest

	if _, err := Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux"}); err != nil {
		t.Fatal(err)
	}

	if info, _ := os.Stat(dest); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
}

func TestBackupsAreNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "conf")
	os.WriteFile(dest, []byte("v1"), 0o644)

	for _, local := range []string{"v1", "v2"} {
		if err := Overwrite(ApplyResult{Dest: dest, Local: []byte(local), Repo: []byte("repo")}); err != nil {
			t.Fatal(err)
		}
	}

	entries, _ := os.ReadDir(dir)
	backups := 0

	for _, e := range entries {
		if strings.Contains(e.Name(), BackupSuffix) {
			backups++
		}
	}

	if backups != 2 {
		t.Errorf("backups = %d, want 2", backups)
	}
}
