package config

import (
	"strings"
	"testing"
)

func TestMergeAddsAutoCaptureButKeepsUserRule(t *testing.T) {
	existing := "version: 2\napps: []\ncapture:\n  fish: # mine\n    linux: {base: ~/.config/fish, include: [config.fish]}\n"
	scanned := &Config{Version: 2, Capture: map[string]map[string]FileRule{
		"fish": {"linux": {Base: "~/.config/fish", Include: []string{"**"}, Origin: OriginAuto}},
		"mc":   {"linux": {Base: "~/.config/mc", Include: []string{"**"}, Origin: OriginAuto}},
	}}
	out, _, err := Merge([]byte(existing), scanned, "debian")

	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Parse(out)

	if err != nil {
		t.Fatal(err)
	}

	if got := cfg.Capture["fish"]["linux"].Include; len(got) != 1 || got[0] != "config.fish" {
		t.Errorf("user rule changed: %v", got)
	}

	if cfg.Capture["mc"]["linux"].Origin != OriginAuto {
		t.Errorf("auto rule missing: %v", cfg.Capture["mc"])
	}

	if !strings.Contains(string(out), "# mine") {
		t.Errorf("comment lost:\n%s", out)
	}
}

func TestValidateCapture(t *testing.T) {
	cfg := &Config{Capture: map[string]map[string]FileRule{
		"a": {"windows": {Base: "~/x", Include: []string{"**"}}},
		"b": {"linux": {Base: "", Include: []string{"**"}}},
	}}

	if errs := cfg.Validate(); len(errs) != 2 {
		t.Errorf("errors = %v", errs)
	}
}
