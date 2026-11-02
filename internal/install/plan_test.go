package install

import (
	"slices"
	"testing"

	"github.com/rchehd/rehome/internal/config"
)

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
