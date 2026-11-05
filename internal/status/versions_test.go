package status

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
)

func TestStatusShowsActualAndRecordedVersions(t *testing.T) {
	repo, local := lock.New(), lock.New()
	repo.Set("older", "debian", lock.Entry{Manager: "apt", Version: "3.0"})
	repo.Set("newer", "debian", lock.Entry{Manager: "apt", Version: "1.0"})
	repo.Set("plugin", "debian", lock.Entry{Manager: "script", Commit: "aaaaaaaaaaaaaaaa"})
	repo.Set("changed-manager", "debian", lock.Entry{Manager: "apt", Version: "1.0"})
	current := map[string]lock.Entry{
		"older":           {Manager: "apt", Version: "2.0"},
		"newer":           {Manager: "apt", Version: "2.0"},
		"plugin":          {Manager: "script", Commit: "bbbbbbbbbbbbbbbb"},
		"unrecorded":      {Manager: "apt", Version: "1.5"},
		"changed-manager": {Manager: "brew", Version: "1.0"},
	}
	steps := []install.Step{{App: config.App{ID: "older"}, Status: install.StatusInstalled, Installed: "2.0", Upgrade: lock.Entry{Manager: "apt", Version: "3.0"}}}
	s := &Status{}
	s.compareVersions(steps, current, repo, local, "debian")
	if len(s.Behind) != 1 || len(s.NotSynced) != 4 {
		t.Fatalf("comparison changed sync semantics: %+v", s)
	}
	sections := s.Sections()
	if older := sections[1].Items[0]; older.Installed != "2.0" || older.Recorded != "3.0" {
		t.Fatalf("upgrade direction is unclear: %+v", older)
	}
	for _, item := range sections[4].Items {
		switch item.Name {
		case "newer":
			if item.Installed != "2.0" || item.Recorded != "1.0" {
				t.Errorf("local update direction is unclear: %+v", item)
			}
		case "plugin":
			if item.Installed != "@bbbbbbbbbbbb" || item.Recorded != "@aaaaaaaaaaaa" {
				t.Errorf("commit comparison missing: %+v", item)
			}
		case "unrecorded":
			if item.Recorded != "not recorded" {
				t.Errorf("invented a recorded version: %+v", item)
			}
		case "changed-manager":
			if !strings.Contains(strings.Join(item.Detail, " "), "apt → brew") {
				t.Errorf("install method change hidden: %+v", item)
			}
		}
	}
	var out bytes.Buffer
	s.PrintDetails(&out)
	for _, want := range []string{"ON MACHINE", "IN CONFIG", "3.0", "2.0", "1.0", "@bbbbbbbbbbbb", "@aaaaaaaaaaaa"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("details missing %s", want)
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Status
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Versions["plugin"].Installed.Commit != "bbbbbbbbbbbbbbbb" || len(decoded.NotSynced) != 4 || len(decoded.Behind) != 1 {
		t.Fatalf("JSON lost full versions or existing arrays: %s", data)
	}
	entry, _ := repo.Get("older", "debian")
	if entry.Version != "3.0" || len(local.Apps) != 0 {
		t.Fatal("status modified a lock")
	}
}

func TestVersionPinAndRecordedVersionAreDistinct(t *testing.T) {
	repo := lock.New()
	repo.Set("app", "debian", lock.Entry{Manager: "apt", Version: "1.0"})
	s := &Status{}
	step := install.Step{App: config.App{ID: "app", Version: "3.0"}, Status: install.StatusInstalled, Installed: "2.0", Upgrade: lock.Entry{Manager: "apt", Version: "3.0"}}
	s.compareVersions([]install.Step{step}, map[string]lock.Entry{"app": {Manager: "apt", Version: "2.0"}}, repo, lock.New(), "debian")
	sections := s.Sections()
	if sections[1].Items[0].Recorded != "3.0 (pin)" || sections[4].Items[0].Recorded != "1.0" {
		t.Fatalf("pin replaced recorded history: %+v", sections)
	}
}

func TestUnavailableAndUnknownVersionLabels(t *testing.T) {
	if got := versionText(lock.Entry{}, "unknown"); got != "unknown" {
		t.Fatalf("invented a version: %s", got)
	}
	if got := versionText(lock.Entry{Unsupported: true}, "unknown"); got != "unavailable" {
		t.Fatalf("unsupported version = %s", got)
	}
}
