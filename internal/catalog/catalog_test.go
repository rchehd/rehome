package catalog

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
)

func TestLoadUniqueIDs(t *testing.T) {
	c, err := Load()

	if err != nil {
		t.Fatal(err)
	}

	if len(c.Entries) == 0 {
		t.Fatal("empty catalog")
	}

	seen := map[string]bool{}

	for _, e := range c.Entries {
		if e.ID == "" || seen[e.ID] {
			t.Errorf("empty or duplicate id %q", e.ID)
		}

		seen[e.ID] = true
	}
}

func TestEntriesHaveValidSpecs(t *testing.T) {
	c, err := Load()

	if err != nil {
		t.Fatal(err)
	}

	for _, e := range c.Entries {
		app := config.App{ID: e.ID, Install: e.Install}
		cfg := &config.Config{Apps: []config.App{app}}

		for _, err := range cfg.Validate() {
			if strings.Contains(err.Error(), "unknown apt repo") {
				continue
			}

			t.Errorf("%s: %v", e.ID, err)
		}
	}
}

func TestGlobRegexp(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"**/x", "x", true},
		{"**/x", "a/b/x", true},
		{"**/x", "a/b/y", false},
		{"*.md", "a.md", true},
		{"*.md", "a/b.md", false},
		{"skills/**", "skills/a/b", true},
		{"skills/**", "other/a", false},
		{"a?c", "abc", true},
		{"a?c", "a/c", false},
		{"a.b", "axb", false},
	}

	for _, tt := range tests {
		if got := globRegexp(tt.pattern).MatchString(tt.path); got != tt.want {
			t.Errorf("%q ~ %q = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestFileRuleMatches(t *testing.T) {
	rule := FileRule{Include: []string{"**"}, Exclude: []string{"cache/**", "*.bak"}}

	tests := []struct {
		rel  string
		want bool
	}{
		{"settings.json", true},
		{"a/b/c.txt", true},
		{"cache/x", false},
		{"old.bak", false},
		{"sub/old.bak", true},
	}

	for _, tt := range tests {
		if got := rule.Matches(tt.rel); got != tt.want {
			t.Errorf("Matches(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}

func TestFileRuleExcludeWins(t *testing.T) {
	rule := FileRule{Include: []string{"skills/**"}, Exclude: []string{"skills/tmp/**"}}

	if !rule.Matches("skills/a") || rule.Matches("skills/tmp/a") {
		t.Error("exclude must beat include")
	}

	if !rule.Excludes("skills/tmp/a") || rule.Excludes("skills/a") {
		t.Error("Excludes mismatch")
	}
}

func TestMatch(t *testing.T) {
	c := &Catalog{Entries: []Entry{
		{ID: "chrome", Detect: Detect{Packages: []string{"google-chrome-stable"}}},
		{ID: "codex", Detect: Detect{Bins: []string{"codex"}}},
	}}

	tests := []struct {
		name string
		d    adapter.Detected
		want string
	}{
		{"by id", adapter.Detected{Name: "chrome"}, "chrome"},
		{"by package", adapter.Detected{Name: "google-chrome-stable"}, "chrome"},
		{"by bin", adapter.Detected{Name: "@openai/codex", Meta: map[string]string{"bin": "codex"}}, "codex"},
		{"none", adapter.Detected{Name: "zzz", Meta: map[string]string{"bin": "zzz"}}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ok := c.Match(tt.d)

			if tt.want == "" {
				if ok {
					t.Errorf("matched %s", e.ID)
				}

				return
			}

			if !ok || e.ID != tt.want {
				t.Errorf("got %v, %v", e, ok)
			}
		})
	}
}

func TestEntryRule(t *testing.T) {
	e := Entry{Files: map[string]FileRule{"linux": {Base: "~/x"}, "macos": {}}}

	if _, ok := e.Rule("linux"); !ok {
		t.Error("linux rule missing")
	}

	if _, ok := e.Rule("macos"); ok {
		t.Error("rule without base should not count")
	}
}

func TestEveryDebianEntryHasAMacOSAnswer(t *testing.T) {
	cat, err := Load()

	if err != nil {
		t.Fatal(err)
	}

	for _, e := range cat.Entries {
		_, debian := e.Install["debian"]
		_, macos := e.Install["macos"]
		_, any := e.Install["any"]

		if debian && !macos && !any && !slices.Contains(e.Skip, "macos") {
			t.Errorf("%s: has install.debian but no install.macos, install.any or skip: [macos]", e.ID)
		}
	}
}

func TestPresentChecksExtraPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, "Library/Fonts"), 0o755)
	os.WriteFile(filepath.Join(home, "Library/Fonts/Hack.ttf"), []byte("font"), 0o644)
	cat, _ := Load()

	found := false

	for _, e := range cat.Present("macos") {
		found = found || e.ID == "fonts"
	}

	if !found {
		t.Error("fonts in ~/Library/Fonts not detected")
	}
}
