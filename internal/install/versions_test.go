package install

import (
	"context"
	"testing"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

// fakeAdapter is an npm-like adapter that reports a fixed installed version.
type fakeAdapter struct{ installed string }

func (fakeAdapter) Name() string                     { return "npm" }
func (fakeAdapter) Available(platform.Platform) bool { return true }

func (fakeAdapter) Scan(context.Context, *runner.Runner) ([]adapter.Detected, error) {
	return nil, nil
}

func (f fakeAdapter) Installed(context.Context, *runner.Runner, config.InstallSpec) (bool, error) {
	return f.installed != "", nil
}

func (fakeAdapter) Install(context.Context, *runner.Runner, config.InstallSpec) error {
	return nil
}

func (fakeAdapter) InstallVersion(context.Context, *runner.Runner, config.InstallSpec, lock.Entry) error {
	return nil
}

func (f fakeAdapter) Version(context.Context, *runner.Runner, config.InstallSpec) (lock.Entry, error) {
	return lock.Entry{Version: f.installed}, nil
}

func versionStep(status, installed, pinned string) Step {
	return Step{
		App:     config.App{ID: "tool", Version: pinned},
		Spec:    config.InstallSpec{Npm: "tool"},
		Adapter: fakeAdapter{installed: installed},
		Status:  status,
	}
}

func lockAt(v string) *lock.Lock {
	l := lock.New()
	l.Set("tool", "debian", lock.Entry{Manager: "npm", Version: v})

	return l
}

func TestWithVersions(t *testing.T) {
	cases := []struct {
		name               string
		step               Step
		mode               string
		latest             []string
		wantPin, wantUpTo  string
		wantInstalledLabel string
	}{
		{"missing, same → pin", versionStep(StatusInstall, "", ""), ModeSame, nil, "1.5.0", "", ""},
		{"missing, latest → no pin", versionStep(StatusInstall, "", ""), ModeLatest, nil, "", "", ""},
		{"missing, same but toggled latest", versionStep(StatusInstall, "", ""), ModeSame, []string{"tool"}, "", "", ""},
		{"missing, permanent version wins in latest mode", versionStep(StatusInstall, "", "2.0.0"), ModeLatest, nil, "2.0.0", "", ""},
		{"installed older → upgrade offered", versionStep(StatusInstalled, "1.2.0", ""), ModeLatest, nil, "", "1.5.0", "1.2.0"},
		{"installed newer → nothing (no downgrade)", versionStep(StatusInstalled, "1.9.0", ""), ModeSame, nil, "", "", "1.9.0"},
		{"installed equal → nothing", versionStep(StatusInstalled, "1.5.0", ""), ModeSame, nil, "", "", "1.5.0"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := withVersions(context.Background(), nil, c.step, lockAt("1.5.0"), "debian", Selection{Mode: c.mode, Latest: c.latest})

			if got.Pin.Version != c.wantPin {
				t.Errorf("pin = %q, want %q", got.Pin.Version, c.wantPin)
			}

			if got.Upgrade.Version != c.wantUpTo {
				t.Errorf("upgrade = %q, want %q", got.Upgrade.Version, c.wantUpTo)
			}

			if got.Installed != c.wantInstalledLabel {
				t.Errorf("installed = %q, want %q", got.Installed, c.wantInstalledLabel)
			}
		})
	}
}

func TestWithVersionsIgnoresOtherManagerAndUnsupported(t *testing.T) {
	l := lock.New()
	l.Set("tool", "debian", lock.Entry{Manager: "apt", Version: "1.5.0"})
	got := withVersions(context.Background(), nil, versionStep(StatusInstall, "", ""), l, "debian", Selection{Mode: ModeSame})

	if got.Pin != (lock.Entry{}) {
		t.Errorf("pinned across managers: %v", got.Pin)
	}

	l.Set("tool", "debian", lock.Entry{Unsupported: true})
	got = withVersions(context.Background(), nil, versionStep(StatusInstall, "", ""), l, "debian", Selection{Mode: ModeSame})

	if got.Pin != (lock.Entry{}) {
		t.Errorf("pinned an unsupported entry: %v", got.Pin)
	}
}
