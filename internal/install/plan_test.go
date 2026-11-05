package install

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

func TestWSLPlan(t *testing.T) {
	cfg := &config.Config{Apps: []config.App{
		{ID: "fonts", Files: "fonts"},
		{ID: "fira-code", Install: map[string]config.InstallSpec{"debian": {Apt: "fonts-firacode"}}},
		{ID: "gnome-settings", Files: "gnome-settings"},
		{ID: "code", Install: map[string]config.InstallSpec{"debian": {Apt: "code"}}},
		{ID: "editor", Group: "desktop", Install: map[string]config.InstallSpec{"any": {Script: "true"}}},
		{ID: "snap-tool", Install: map[string]config.InstallSpec{"debian": {Snap: "snap-tool"}}},
		{ID: "flatpak-tool", Install: map[string]config.InstallSpec{"debian": {Flatpak: "org.example.Tool"}}},
		{ID: "flatpak", Install: map[string]config.InstallSpec{"debian": {Apt: "flatpak:amd64"}}},
		{ID: "terminal-tool", Install: map[string]config.InstallSpec{"any": {Script: "true"}}},
	}}

	for _, systemd := range []bool{false, true} {
		p := platform.Platform{OS: "linux", ID: "ubuntu", WSL: true, Systemd: systemd}
		plan, err := Build(t.Context(), &runner.Runner{}, cfg, "", p, Selection{All: true, Offline: true}, nil)

		if err != nil {
			t.Fatal(err)
		}

		for _, s := range plan.Steps {
			switch s.App.ID {
			case "fonts", "fira-code", "code", "editor":
				if s.Status != StatusUnavailable || s.Reason != "install this on Windows" {
					t.Errorf("%s: %s (%s)", s.App.ID, s.Status, s.Reason)
				}
			case "gnome-settings":
				if s.Status != StatusUnavailable {
					t.Errorf("GNOME settings must be unavailable in WSL: %s", s.Status)
				}
			case "terminal-tool":
				if s.Status != StatusInstall {
					t.Errorf("terminal tool should remain installable: %s", s.Status)
				}
			case "snap-tool", "flatpak-tool":
				if (s.Status == StatusInstall) != systemd {
					t.Errorf("%s with systemd=%t: %s (%s)", s.App.ID, systemd, s.Status, s.Reason)
				}
			case "flatpak":
				if !systemd && s.Reason != "Snap and Flatpak are unavailable in WSL without systemd" {
					t.Errorf("Flatpak backend should be blocked: %s (%s)", s.Status, s.Reason)
				}
			}
		}
	}
}

func step(id string, spec config.InstallSpec, requires ...string) Step {
	return Step{App: config.App{ID: id, Requires: requires}, Spec: spec}
}

func stepIDs(steps []Step) []string {
	var out []string

	for _, s := range steps {
		out = append(out, s.App.ID)
	}

	return out
}

func TestOrderRequirementsFirst(t *testing.T) {
	steps := []Step{
		step("app", config.InstallSpec{Apt: "app"}, "node"),
		step("node", config.InstallSpec{Script: "x"}),
	}

	got, err := order(steps)

	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"node", "app"}; !slices.Equal(stepIDs(got), want) {
		t.Errorf("order = %v, want %v", stepIDs(got), want)
	}
}

func TestOrderByAdapterRank(t *testing.T) {
	steps := []Step{
		step("s", config.InstallSpec{Script: "x"}),
		step("n", config.InstallSpec{Npm: "x"}),
		step("a", config.InstallSpec{Apt: "x"}),
	}

	got, err := order(steps)

	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"a", "n", "s"}; !slices.Equal(stepIDs(got), want) {
		t.Errorf("order = %v, want %v", stepIDs(got), want)
	}
}

func TestOrderStableForEqualRank(t *testing.T) {
	steps := []Step{
		step("c", config.InstallSpec{Apt: "c"}),
		step("a", config.InstallSpec{Apt: "a"}),
		step("b", config.InstallSpec{Apt: "b"}),
	}

	got, err := order(steps)

	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"c", "a", "b"}; !slices.Equal(stepIDs(got), want) {
		t.Errorf("order = %v, want %v", stepIDs(got), want)
	}
}

func TestOrderIgnoresUnselectedRequirement(t *testing.T) {
	got, err := order([]Step{step("a", config.InstallSpec{Apt: "a"}, "absent")})

	if err != nil || len(got) != 1 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestOrderCycle(t *testing.T) {
	steps := []Step{
		step("a", config.InstallSpec{Apt: "a"}, "b"),
		step("b", config.InstallSpec{Apt: "b"}, "a"),
	}

	if _, err := order(steps); err == nil {
		t.Error("expected cycle error")
	}
}

func TestPlanCounts(t *testing.T) {
	plan := &Plan{Steps: []Step{
		{Status: StatusInstall},
		{Status: StatusInstall},
		{Status: StatusDisabled},
		{Status: StatusInstalled},
	}}

	got := plan.Counts()

	if got[StatusInstall] != 2 || got[StatusDisabled] != 1 || got[StatusInstalled] != 1 || got[StatusUnavailable] != 0 {
		t.Errorf("counts = %v", got)
	}
}

func TestPlanCommands(t *testing.T) {
	plan := &Plan{Steps: []Step{
		{App: config.App{ID: "a", PostInstall: []string{"setup a"}}, Spec: config.InstallSpec{Script: "curl a"}, Status: StatusInstall},
		{App: config.App{ID: "b", PostInstall: []string{"setup b"}}, Status: StatusFilesOnly},
		{App: config.App{ID: "c", PostInstall: []string{"setup c"}}, Spec: config.InstallSpec{Script: "curl c"}, Status: StatusInstalled},
		{App: config.App{ID: "d"}, Spec: config.InstallSpec{Apt: "d"}, Status: StatusInstall},
		{App: config.App{ID: "e"}, Spec: config.InstallSpec{Script: "curl e"}, Status: StatusDisabled},
	}}

	want := []string{"a: curl a", "a (post-install): setup a", "b (post-install): setup b"}

	if got := plan.Commands(); !slices.Equal(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestPluginInstallLocationIsNotInterchangeableWithApt(t *testing.T) {
	for _, location := range []string{"package-only", "wrong-location", "expected-clone"} {
		t.Run(location, func(t *testing.T) {
			home, bin := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PATH", bin)
			for _, command := range []string{"dpkg-query", "apt-get"} {
				if err := os.WriteFile(filepath.Join(bin, command), []byte("#!/bin/sh\nprintf installed\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{"usr/share/zsh-syntax-highlighting", map[string]string{"package-only": "unused", "wrong-location": ".oh-my-zsh/other/plugins/zsh-syntax-highlighting", "expected-clone": ".oh-my-zsh/custom/plugins/zsh-syntax-highlighting"}[location]} {
				if err := os.MkdirAll(filepath.Join(home, path), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			aptSpec := config.InstallSpec{Apt: "zsh-syntax-highlighting"}
			pluginSpec := config.InstallSpec{Script: "git clone fixture", Bin: "~/.oh-my-zsh/custom/plugins/zsh-syntax-highlighting"}
			cfg := &config.Config{Apps: []config.App{
				{ID: "zsh-syntax-highlighting", Install: map[string]config.InstallSpec{"debian": aptSpec}},
				{ID: "oh-my-zsh-zsh-syntax-highlighting", Install: map[string]config.InstallSpec{"any": pluginSpec}},
			}}
			p := platform.Platform{OS: "linux", ID: "ubuntu"}
			plan, err := Build(t.Context(), &runner.Runner{}, cfg, "", p, Selection{Offline: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, step := range plan.Steps {
				want := StatusInstalled
				if step.App.ID == "oh-my-zsh-zsh-syntax-highlighting" && location != "expected-clone" {
					want = StatusInstall
				}
				if step.Status != want {
					t.Errorf("%s status = %s, want %s", step.App.ID, step.Status, want)
				}
			}
		})
	}
}
