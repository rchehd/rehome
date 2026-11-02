package config

import (
	"slices"
	"testing"
)

func TestAppSpecPrefersPlatformKey(t *testing.T) {
	app := App{Install: map[string]InstallSpec{
		"any":    {Script: "curl x"},
		"debian": {Apt: "x"},
	}}

	if s, ok := app.Spec("debian"); !ok || s.Apt != "x" {
		t.Errorf("debian spec = %+v, %v", s, ok)
	}

	if s, ok := app.Spec("macos"); !ok || s.Script != "curl x" {
		t.Errorf("fallback spec = %+v, %v", s, ok)
	}

	if _, ok := (App{}).Spec("debian"); ok {
		t.Error("empty app should have no spec")
	}
}

func TestInstallSpecManager(t *testing.T) {
	tests := []struct {
		spec      InstallSpec
		name, pkg string
	}{
		{InstallSpec{Apt: "htop"}, "apt", "htop"},
		{InstallSpec{UvTool: "ruff"}, "uvtool", "ruff"},
		{InstallSpec{GoPkg: "x/y"}, "go", "x/y"},
		{InstallSpec{Script: "curl x"}, "script", "curl x"},
		{InstallSpec{Classic: true}, "", ""},
	}

	for _, tt := range tests {
		if name, pkg := tt.spec.Manager(); name != tt.name || pkg != tt.pkg {
			t.Errorf("Manager(%+v) = %q, %q", tt.spec, name, pkg)
		}
	}
}

func TestAptRepoAllKeyrings(t *testing.T) {
	tests := []struct {
		repo AptRepo
		want []string
	}{
		{AptRepo{Keyring: "a"}, []string{"a"}},
		{AptRepo{Keyring: "a", Keyrings: []string{"b", "c"}}, []string{"a", "b", "c"}},
		{AptRepo{Keyrings: []string{"b"}}, []string{"b"}},
		{AptRepo{}, nil},
	}

	for _, tt := range tests {
		if got := tt.repo.AllKeyrings(); !slices.Equal(got, tt.want) {
			t.Errorf("AllKeyrings(%+v) = %v", tt.repo, got)
		}
	}
}

func TestAppIsEnabled(t *testing.T) {
	off := false

	if !(App{}).IsEnabled() || (App{Enabled: &off}).IsEnabled() {
		t.Error("nil means enabled, false means disabled")
	}
}
