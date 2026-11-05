package discover

import (
	"os"

	"github.com/rchehd/rehome/internal/config"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func put(t *testing.T, home, rel string, size int) {
	t.Helper()
	path := filepath.Join(home, rel)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(strings.Repeat("a", size)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestForAppFindsConfigDirFilesAndRc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	put(t, home, ".config/mc/ini", 100)
	put(t, home, ".config/starship.toml", 50)
	put(t, home, ".lessrc", 10)

	rule, warn := ForApp("mc", []string{"mc", "starship", "less"}, nil)

	if warn != nil || rule == nil {
		t.Fatalf("rule %v warn %v", rule, warn)
	}

	for _, want := range []string{".config/mc/**", ".config/starship.toml", ".lessrc"} {
		if !slices.Contains(rule.Include, want) {
			t.Errorf("include %v misses %s", rule.Include, want)
		}
	}

	if rule.Origin != "auto" || !rule.TextOnly || rule.Base != "~" {
		t.Errorf("rule = %+v", rule)
	}
}

func TestForAppIgnoresCacheOnlyDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	put(t, home, ".config/app/Cache/data", 100)
	put(t, home, ".config/app/logs/x.log", 100)

	if rule, warn := ForApp("app", []string{"app"}, nil); rule != nil || warn != nil {
		t.Errorf("rule %v warn %v", rule, warn)
	}
}

func TestForAppWarnsWhenTooLarge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for i := 0; i < 12; i++ {
		put(t, home, filepath.Join(".config/big", "f"+string(rune('a'+i))), 1<<20-10)
	}

	rule, warn := ForApp("big", []string{"big"}, nil)

	if rule != nil || warn == nil || !strings.Contains(warn.Reason, "MB") || warn.Action == "" {
		t.Errorf("rule %v warn %+v", rule, warn)
	}
}

func TestForAppSkipsNoisyDotDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	put(t, home, ".docker/config.json", 100)

	if rule, _ := ForApp("docker", []string{"docker"}, nil); rule != nil {
		t.Errorf("took ~/.docker: %v", rule.Include)
	}
}

func TestForAppNothingFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if rule, warn := ForApp("x", []string{"x"}, nil); rule != nil || warn != nil {
		t.Errorf("rule %v warn %v", rule, warn)
	}
}

func TestForAppSandboxedApps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	put(t, home, ".var/app/org.gnome.Evolution/config/evolution/settings.ini", 100)
	put(t, home, "snap/telegram-desktop/current/.config/tg/settings.json", 100)

	rule, _ := ForApp("x", []string{"org.gnome.Evolution", "telegram-desktop"}, nil)

	if rule == nil || !slices.Contains(rule.Include, ".var/app/org.gnome.Evolution/config/**") ||
		!slices.Contains(rule.Include, "snap/telegram-desktop/current/.config/**") {
		t.Errorf("rule = %+v", rule)
	}
}

func TestForAppSkipsClaimedPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	put(t, home, ".claude/settings.json", 100)

	if rule, _ := ForApp("claude-desktop", []string{"claude"}, []string{filepath.Join(home, ".claude")}); rule != nil {
		t.Errorf("took a claimed path: %v", rule.Include)
	}
}

func TestClaimedNeverClaimsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := Claimed([]config.FileRule{{Base: "~", Include: []string{".zshrc", "*.md"}}, {Base: "~/.claude", Include: []string{"**"}}})

	if slices.Contains(got, home) || !slices.Contains(got, filepath.Join(home, ".zshrc")) || !slices.Contains(got, filepath.Join(home, ".claude")) {
		t.Errorf("claimed = %v", got)
	}

	if Covered(filepath.Join(home, ".config/mc"), got) {
		t.Error("unrelated dir counted as covered")
	}
}

func TestForAppMacApplicationSupport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	put(t, home, "Library/Application Support/Rectangle/config.json", 100)

	rule, _ := ForApp("rectangle", []string{"rectangle"}, nil)

	if rule == nil || !slices.Contains(rule.Include, "Library/Application Support/Rectangle/**") {
		t.Errorf("rule = %+v", rule)
	}
}

func TestDesktopSessionWarningAndRuntimeOnlyDiscovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, parent := range []string{".config/Claude", "Library/Application Support/Claude"} {
		put(t, home, parent+"/claude-code-sessions/account/local_123.json", 100)
		put(t, home, parent+"/Crashpad/client_id", 100)
		put(t, home, parent+"/LOG", 100)
	}
	if rule, _ := ForApp("claude-desktop", []string{"claude"}, nil); rule != nil {
		t.Fatalf("runtime-only folder produced capture rule: %+v", rule)
	}
	if warnings := DesktopSessions(); len(warnings) != 2 {
		t.Fatalf("expected Linux and macOS Desktop warnings, got %+v", warnings)
	}
	put(t, home, ".config/Claude/settings.json", 100)
	if rule, _ := ForApp("claude-desktop", []string{"claude"}, nil); rule == nil {
		t.Fatal("ordinary settings were excluded")
	}
}
