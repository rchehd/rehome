package install

import (
	"context"
	"strings"
	"testing"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/resolve"
	"github.com/rchehd/rehome/internal/runner"
)

type brewOnly map[string]bool

func (b brewOnly) Lookup(_ context.Context, name string, _ bool) (config.InstallSpec, bool) {
	return config.InstallSpec{Brew: name}, b[name]
}

func TestResolveStepMapsMissingPlatform(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	mac := platform.Platform{OS: "darwin", ID: "macos"}
	res := &resolve.Resolver{PlatformKey: "macos", Index: brewOnly{"htop": true}}
	debianOnly := func(id, pkg string) config.App {
		return config.App{ID: id, Install: map[string]config.InstallSpec{"debian": {Apt: pkg}}}
	}

	step := resolveStep(context.Background(), &runner.Runner{}, debianOnly("htop", "htop"), mac, Selection{}, res)

	if step.Status != StatusInstall || step.Spec.Brew != "htop" || step.Mapped != resolve.SourceSameName {
		t.Errorf("htop step = %+v", step)
	}

	step = resolveStep(context.Background(), &runner.Runner{}, debianOnly("winonly", "winonly"), mac, Selection{}, res)

	if step.Status != StatusUnavailable || !strings.Contains(step.Reason, "add install.macos") {
		t.Errorf("unmapped step = %+v", step)
	}
}
