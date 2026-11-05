package config

import "testing"

func TestIgnored(t *testing.T) {
	cfg := &Config{Ignore: map[string][]string{"mac-mini": {"docker"}}}
	steam := App{ID: "steam", Skip: []string{"macos"}}
	docker := App{ID: "docker"}

	if !cfg.Ignored(steam, "macos", "any-host") || cfg.Ignored(steam, "debian", "any-host") {
		t.Error("skip by platform")
	}

	if !cfg.Ignored(docker, "debian", "mac-mini") || cfg.Ignored(docker, "debian", "other") {
		t.Error("ignore by host")
	}
}

func TestValidateSkipPlatform(t *testing.T) {
	cfg := &Config{Apps: []App{{ID: "a", Skip: []string{"windows"}}}}

	if errs := cfg.Validate(); len(errs) != 1 {
		t.Errorf("errors = %v", errs)
	}
}

func TestMergeDoesNotFlagIgnoredApps(t *testing.T) {
	existing := "version: 2\napps:\n  - id: steam\n    skip: [debian]\n    install:\n      debian: {apt: steam}\n"
	out, report, err := Merge([]byte(existing), &Config{Version: 2}, "debian", nil)

	if err != nil {
		t.Fatal(err)
	}

	if len(report.MissingOnScan) != 0 || string(out) != existing {
		t.Errorf("ignored app flagged: %v\n%s", report.MissingOnScan, out)
	}
}
