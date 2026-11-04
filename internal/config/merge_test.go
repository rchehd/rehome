package config

import (
	"bytes"
	"slices"
	"testing"
)

func mustParse(t *testing.T, data []byte) *Config {
	t.Helper()

	cfg, err := Parse(data)

	if err != nil {
		t.Fatalf("parse: %v\n%s", err, data)
	}

	return cfg
}

func ids(cfg *Config) []string {
	var out []string

	for _, a := range cfg.Apps {
		out = append(out, a.ID)
	}

	return out
}

func merge(t *testing.T, existing string, scanned *Config, key string) (*Config, []byte, MergeReport) {
	t.Helper()

	out, report, err := Merge([]byte(existing), scanned, key, nil)

	if err != nil {
		t.Fatal(err)
	}

	return mustParse(t, out), out, report
}

func TestMergeIntoEmpty(t *testing.T) {
	scanned := apps(
		App{ID: "htop", Group: "tools", Install: debian(InstallSpec{Apt: "htop"})},
		App{ID: "zsh", Group: "shell", Install: debian(InstallSpec{Apt: "zsh"})},
	)

	cfg, _, report := merge(t, "  \n", scanned, "debian")

	if !slices.Equal(ids(cfg), []string{"htop", "zsh"}) {
		t.Errorf("ids = %v", ids(cfg))
	}

	if !slices.Equal(report.Added, []string{"htop", "zsh"}) {
		t.Errorf("added = %v", report.Added)
	}

	if s, _ := cfg.Apps[0].Spec("debian"); s.Apt != "htop" {
		t.Errorf("spec = %+v", s)
	}
}

func TestMergeKeepsUserEditsByteForByte(t *testing.T) {
	existing := `version: 1
apps:
  # my favourite shell
  - id: zsh
    group: shell
    enabled: false
    install:
      debian: { apt: zsh }
`
	scanned := apps(App{ID: "zsh", Group: "shell", Install: debian(InstallSpec{Apt: "zsh"})})

	_, out, report := merge(t, existing, scanned, "debian")

	if !bytes.Equal(out, []byte(existing)) {
		t.Errorf("output differs:\n%s", out)
	}

	if report.Changed() || !slices.Equal(report.Unchanged, []string{"zsh"}) {
		t.Errorf("report = %+v", report)
	}
}

func TestMergeInsertsAfterLastAppOfGroup(t *testing.T) {
	existing := `apps:
  - id: chrome
    group: browsers
    install: { debian: { apt: chrome } }
  - id: firefox
    group: browsers
    install: { debian: { snap: firefox } }
  - id: zsh
    group: shell
    install: { debian: { apt: zsh } }
`
	scanned := apps(
		App{ID: "chrome", Group: "browsers", Install: debian(InstallSpec{Apt: "chrome"})},
		App{ID: "brave", Group: "browsers", Install: debian(InstallSpec{Apt: "brave"})},
	)

	cfg, _, report := merge(t, existing, scanned, "debian")

	if want := []string{"chrome", "firefox", "brave", "zsh"}; !slices.Equal(ids(cfg), want) {
		t.Errorf("ids = %v, want %v", ids(cfg), want)
	}

	if !slices.Equal(report.Added, []string{"brave"}) {
		t.Errorf("added = %v", report.Added)
	}
}

func TestMergeFlagsMissingOnScan(t *testing.T) {
	existing := `apps:
  - id: gone
    install: { debian: { apt: gone } }
  - id: macsonly
    install: { macos: { apt: x } }
`
	cfg, _, report := merge(t, existing, apps(), "debian")

	if !cfg.Apps[0].MissingOnScan {
		t.Error("gone should be flagged")
	}

	if cfg.Apps[1].MissingOnScan {
		t.Error("macos-only app must not be flagged on debian")
	}

	if !slices.Equal(report.MissingOnScan, []string{"gone"}) {
		t.Errorf("missing = %v", report.MissingOnScan)
	}
}

func TestMergeReturnedApp(t *testing.T) {
	existing := `apps:
  - id: back
    install: { debian: { apt: back } }
    missing_on_scan: true
`
	scanned := apps(App{ID: "back", Install: debian(InstallSpec{Apt: "back"})})

	cfg, _, report := merge(t, existing, scanned, "debian")

	if cfg.Apps[0].MissingOnScan {
		t.Error("flag should be removed")
	}

	if !slices.Equal(report.Returned, []string{"back"}) {
		t.Errorf("returned = %v", report.Returned)
	}
}

func TestMergeAddsFilesKeepsComment(t *testing.T) {
	existing := `apps:
  - id: kitty
    install: { debian: { apt: kitty } } # keep me
`
	scanned := apps(App{ID: "kitty", Files: "kitty", Install: debian(InstallSpec{Apt: "kitty"})})

	cfg, out, report := merge(t, existing, scanned, "debian")

	if cfg.Apps[0].Files != "kitty" {
		t.Errorf("files = %q", cfg.Apps[0].Files)
	}

	if !slices.Equal(report.Updated, []string{"kitty"}) {
		t.Errorf("updated = %v", report.Updated)
	}

	if !bytes.Contains(out, []byte("# keep me")) {
		t.Errorf("comment lost:\n%s", out)
	}
}

func TestMergeAddsPlatformSpec(t *testing.T) {
	existing := `apps:
  - id: git
    install:
      debian: { apt: git }
`
	scanned := apps(App{ID: "git", Install: map[string]InstallSpec{"macos": {Apt: "git-mac"}}})

	cfg, _, report := merge(t, existing, scanned, "macos")

	if s, ok := cfg.Apps[0].Install["debian"]; !ok || s.Apt != "git" {
		t.Errorf("debian spec lost: %+v", cfg.Apps[0].Install)
	}

	if s, ok := cfg.Apps[0].Install["macos"]; !ok || s.Apt != "git-mac" {
		t.Errorf("macos spec missing: %+v", cfg.Apps[0].Install)
	}

	if !slices.Equal(report.Updated, []string{"git"}) {
		t.Errorf("updated = %v", report.Updated)
	}
}

func TestMergeAptKeysUnion(t *testing.T) {
	existing := "apps: []\napt_keys: [a.gpg, b.gpg]\n"
	scanned := &Config{AptKeys: []string{"b.gpg", "c.gpg"}}

	cfg, _, _ := merge(t, existing, scanned, "debian")

	if want := []string{"a.gpg", "b.gpg", "c.gpg"}; !slices.Equal(cfg.AptKeys, want) {
		t.Errorf("keys = %v, want %v", cfg.AptKeys, want)
	}
}

func TestMergeCompletesAptRepo(t *testing.T) {
	existing := `apps: []
apt_repos:
  docker:
    source: files/apt/sources/docker.list
    custom_note: mine
`
	scanned := &Config{AptRepos: map[string]AptRepo{
		"docker": {Source: "other", Release: "noble", Keyrings: []string{"files/apt/keyrings/d.gpg"}},
	}}

	_, out, _ := merge(t, existing, scanned, "debian")
	cfg := mustParse(t, out)
	repo := cfg.AptRepos["docker"]

	if repo.Release != "noble" || repo.Source != "files/apt/sources/docker.list" {
		t.Errorf("repo = %+v", repo)
	}

	if !slices.Equal(repo.Keyrings, []string{"files/apt/keyrings/d.gpg"}) {
		t.Errorf("keyrings = %v", repo.Keyrings)
	}

	if !bytes.Contains(out, []byte("custom_note: mine")) {
		t.Errorf("user field lost:\n%s", out)
	}
}

func TestMergeRejectsNonMappingRoot(t *testing.T) {
	if _, _, err := Merge([]byte("- a\n- b\n"), apps(), "debian", nil); err == nil {
		t.Error("expected error")
	}
}
