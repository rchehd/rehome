package scan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
)

func TestUnclaimedConfigDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for rel, body := range map[string]string{
		".config/mystery/settings.json":      "{}",
		".config/kitty/kitty.conf":           "font_size 12",
		".config/empty/Cache/x":              "cache",
		".config/google-chrome/Default/Pref": "{}",
		".config/pulse/cookie":               "x",
		".config/fish/config.fish":           "set x",
	} {
		path := filepath.Join(home, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(body), 0o644)
	}

	cat, err := catalog.Load()

	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Capture: map[string]map[string]config.FileRule{
		"fish": {"linux": {Base: "~/.config/fish", Include: []string{"**"}}},
	}}
	got := map[string]string{}

	for _, w := range unclaimedConfigDirs(cfg, cat, "linux") {
		got[w.Path] = w.Reason
	}

	if got["~/.config/mystery"] != "no app claims it" {
		t.Errorf("mystery dir not reported: %v", got)
	}

	if got["~/.config/google-chrome"] != "browser profile, not copied" {
		t.Errorf("browser not reported as browser: %v", got)
	}

	for _, quiet := range []string{"~/.config/kitty", "~/.config/empty", "~/.config/pulse", "~/.config/fish"} {
		if _, ok := got[quiet]; ok {
			t.Errorf("%s should not be reported: %v", quiet, got)
		}
	}
}
