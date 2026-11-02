package install

import (
	"testing"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
)

func TestNeedsBrew(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	mac := platform.Platform{OS: "darwin", ID: "macos"}
	withCask := &executor{plan: &Plan{Platform: mac, Steps: []Step{{Status: StatusInstall, Spec: config.InstallSpec{Cask: "rectangle"}}}}}
	scriptOnly := &executor{plan: &Plan{Platform: mac, Steps: []Step{{Status: StatusInstall, Spec: config.InstallSpec{Script: "x"}}}}}
	linux := &executor{plan: &Plan{Platform: platform.Platform{OS: "linux"}, Steps: withCask.plan.Steps}}

	if !withCask.needsBrew() || scriptOnly.needsBrew() || linux.needsBrew() {
		t.Errorf("needsBrew: cask=%v script=%v linux=%v", withCask.needsBrew(), scriptOnly.needsBrew(), linux.needsBrew())
	}
}
