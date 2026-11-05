package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
)

func TestPlanJSON(t *testing.T) {
	plan := &install.Plan{Mode: install.ModeSame, Steps: []install.Step{
		{App: config.App{ID: "cowsay"}, Spec: config.InstallSpec{Npm: "cowsay"}, Status: install.StatusInstall, Pin: lock.Entry{Version: "1.5.0"}},
		{App: config.App{ID: "plugin"}, Spec: config.InstallSpec{Script: "git clone x y"}, Status: install.StatusInstalled, Upgrade: lock.Entry{Commit: "abc"}},
	}}
	data, err := json.Marshal(PlanJSON(plan, Options{}, InstallRequest{}))

	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`"mode":"same"`, `"pin":"1.5.0"`, `"manager":"npm"`, `"upgrade":"abc"`, `"run_as":"rehome install --all --yes --version same"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %s in %s", want, data)
		}
	}
}

func TestScanReportKeys(t *testing.T) {
	data, err := json.Marshal(ScanReport{})

	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"config", "written", "apps", "unknown", "settings_files", "redacted_secrets", "needs_attention", "node"} {
		if !strings.Contains(string(data), `"`+key+`"`) {
			t.Errorf("missing key %s in %s", key, data)
		}
	}
}

func TestPlanCommandKeepsSessionOptions(t *testing.T) {
	plan := &install.Plan{Mode: install.ModeSame}
	result := PlanJSON(plan, Options{Config: "/config folder/rehome.yml", HarnessData: true, Sessions: true, SessionPaths: map[string]string{"/old project": "/new project", "/old,other": "/new,other"}}, InstallRequest{})
	for _, flag := range []string{"--config '/config folder/rehome.yml'", "--harness-data", "--sessions", "--session-path-map '/old project=/new project'", `--session-path-map '"/old,other=/new,other"'`} {
		if !strings.Contains(result.RunAs, flag) {
			t.Errorf("missing %s in %s", flag, result.RunAs)
		}
	}
}
