package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseDefaultsVersion(t *testing.T) {
	cfg, err := Parse([]byte("apps: []\n"))

	if err != nil {
		t.Fatal(err)
	}

	if cfg.Version != CurrentVersion {
		t.Errorf("version = %d", cfg.Version)
	}
}

func TestParseRejectsUnknownVersion(t *testing.T) {
	if _, err := Parse(fmt.Appendf(nil, "version: %d\n", CurrentVersion+1)); err == nil {
		t.Errorf("expected error for version %d", CurrentVersion+1)
	}
}

func TestParseRejectsBadYAML(t *testing.T) {
	if _, err := Parse([]byte("apps: [")); err == nil {
		t.Error("expected yaml error")
	}
}

func TestIsURL(t *testing.T) {
	tests := map[string]bool{
		"https://x.test/a.yml": true,
		"http://x.test":        true,
		"/etc/rehome.yml":      false,
		"ftp://x.test":         false,
	}

	for in, want := range tests {
		if got := IsURL(in); got != want {
			t.Errorf("IsURL(%q) = %v", in, got)
		}
	}
}

func TestLoadExample(t *testing.T) {
	cfg, root, err := Load("../../examples/rehome.example.yml")

	if err != nil {
		t.Fatal(err)
	}

	if root == "" {
		t.Error("empty root")
	}

	if errs := cfg.Validate(); len(errs) != 0 {
		t.Errorf("example invalid: %v", errs)
	}
}

func TestLoadRefusesPlainHTTP(t *testing.T) {
	if _, _, err := Load("http://example.com/rehome.yml"); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("err = %v", err)
	}
}
