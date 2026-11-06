package scan

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/platform"
)

func TestScanKeepsBitwardenTemplateAndIgnoresDestinationAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dest := filepath.Join(home, "settings")
	alias := filepath.Join(home, "alias")
	if err := os.WriteFile(dest, []byte("local value"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(dest, alias); err != nil {
		t.Fatal(err)
	}

	template := "files/app/settings"
	s := &scanner{res: &Result{}, existing: &config.Config{Files: map[string][]config.FileMap{
		"app": {{Src: template, Dest: map[string]string{"linux": "~/settings"}, Bitwarden: "bw:12345678-1234-1234-1234-123456789abc/token"}},
	}}}
	s.protectSecretFiles()
	s.addChanges([]files.Change{
		{Src: dest, Dst: template},
		{Src: alias, Dst: "files/other/settings"},
		{Src: filepath.Join(home, "other"), Dst: "files/app/other"},
	})

	if len(s.res.FileChanges) != 1 || s.res.FileChanges[0].Dst != "files/app/other" {
		t.Fatalf("unexpected changes: %+v", s.res.FileChanges)
	}
}

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

func TestAutoCaptureRefreshesLegacyRulesWithoutChangingManualRules(t *testing.T) {
	for _, origin := range []string{config.OriginAuto, ""} {
		t.Run("origin="+origin, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			for _, rel := range []string{"settings.json", "Crashpad/client_id", "claude-code-sessions/account/local_123.json"} {
				path := filepath.Join(home, ".config/Claude", rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			rule := config.FileRule{Base: "~", Include: []string{".config/Claude/**"}, Exclude: []string{"**/Cache/**"}, Origin: origin}
			s := &scanner{
				ctx: context.Background(),
				p:   platform.Platform{OS: "linux", ID: "ubuntu"}, cat: &catalog.Catalog{}, root: t.TempDir(),
				apps:     map[string]*config.App{"claude-desktop": {ID: "claude-desktop"}},
				existing: &config.Config{Capture: map[string]map[string]config.FileRule{"claude-desktop": {"linux": rule}}},
				res:      &Result{Config: &config.Config{Files: map[string][]config.FileMap{}}},
			}
			s.captureByRules()
			want := 3
			if origin == config.OriginAuto {
				want = 1
				if !slices.Contains(s.res.Config.Capture["claude-desktop"]["linux"].Exclude, "**/Crashpad/**") {
					t.Fatal("generated capture did not get current defaults")
				}
			}
			if len(s.res.FileChanges) != want || len(s.res.Config.Files["claude-desktop"]) != want {
				t.Fatalf("unexpected capture: %+v", s.res.FileChanges)
			}
			if len(s.existing.Capture["claude-desktop"]["linux"].Exclude) != 1 {
				t.Fatal("scan mutated existing config in memory")
			}
		})
	}
}
