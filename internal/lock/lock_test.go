package lock

import (
	"slices"
	"testing"
)

func node(v string) Entry {
	return Entry{Manager: "script", Version: v}
}

func lockWith(id, platform string, e Entry) *Lock {
	l := New()
	l.Set(id, platform, e)

	return l
}

func TestSyncDoesNotRollBack(t *testing.T) {
	repo := lockWith("node", "debian", node("22.12"))
	local := lockWith("node", "debian", node("22.10"))
	merged, changed := Sync(repo, local, map[string]Entry{"node": node("22.10")}, "debian")

	if e, _ := merged.Get("node", "debian"); e.Version != "22.12" || len(changed) != 0 {
		t.Errorf("got %v changed %v", e, changed)
	}
}

func TestSyncLocalUpgradeBehindRepo(t *testing.T) {
	repo := lockWith("node", "debian", node("22.12"))
	local := lockWith("node", "debian", node("22.10"))
	merged, changed := Sync(repo, local, map[string]Entry{"node": node("22.11")}, "debian")

	if e, _ := merged.Get("node", "debian"); e.Version != "22.12" || len(changed) != 0 {
		t.Errorf("got %v changed %v", e, changed)
	}
}

func TestSyncWritesNewerLocal(t *testing.T) {
	repo := lockWith("node", "debian", node("22.12"))
	local := lockWith("node", "debian", node("22.10"))
	merged, changed := Sync(repo, local, map[string]Entry{"node": node("22.13")}, "debian")

	if e, _ := merged.Get("node", "debian"); e.Version != "22.13" || !slices.Equal(changed, []string{"node"}) {
		t.Errorf("got %v changed %v", e, changed)
	}
}

func TestSyncNewApp(t *testing.T) {
	merged, changed := Sync(New(), New(), map[string]Entry{"htop": {Manager: "apt", Version: "3.3"}}, "debian")

	if e, ok := merged.Get("htop", "debian"); !ok || e.Version != "3.3" || !slices.Equal(changed, []string{"htop"}) {
		t.Errorf("got %v %v changed %v", e, ok, changed)
	}
}

func TestSyncKeepsOtherPlatforms(t *testing.T) {
	repo := lockWith("node", "macos", Entry{Manager: "brew", Version: "22.12"})
	merged, _ := Sync(repo, New(), map[string]Entry{"node": node("22.10")}, "debian")

	if e, _ := merged.Get("node", "macos"); e.Version != "22.12" {
		t.Errorf("macos entry changed: %v", e)
	}

	if e, _ := merged.Get("node", "debian"); e.Version != "22.10" {
		t.Errorf("debian entry = %v", e)
	}
}

func TestSyncUnsupportedAndCommitChanges(t *testing.T) {
	repo := lockWith("steam", "debian", Entry{Manager: "apt", Version: "1.0"})
	repo.Set("plugin", "debian", Entry{Manager: "script", Commit: "aaa"})
	current := map[string]Entry{"steam": {Unsupported: true}, "plugin": {Manager: "script", Commit: "bbb"}}
	merged, changed := Sync(repo, New(), current, "debian")

	if e, _ := merged.Get("steam", "debian"); !e.Unsupported {
		t.Errorf("steam = %v", e)
	}

	if e, _ := merged.Get("plugin", "debian"); e.Commit != "bbb" {
		t.Errorf("plugin = %v", e)
	}

	if !slices.Equal(changed, []string{"plugin", "steam"}) {
		t.Errorf("changed = %v", changed)
	}
}

func TestSyncNewerWithoutLocalBaseline(t *testing.T) {
	repo := lockWith("node", "debian", node("22.12"))
	merged, changed := Sync(repo, New(), map[string]Entry{"node": node("22.14")}, "debian")

	if e, _ := merged.Get("node", "debian"); e.Version != "22.14" || len(changed) != 1 {
		t.Errorf("got %v changed %v", e, changed)
	}
}

func TestSyncDoesNotRollBackCommit(t *testing.T) {
	clone := func(c string) Entry { return Entry{Manager: "script", Commit: c} }
	repo := lockWith("plugin", "debian", clone("new"))
	local := lockWith("plugin", "debian", clone("old"))
	merged, changed := Sync(repo, local, map[string]Entry{"plugin": clone("old")}, "debian")

	if e, _ := merged.Get("plugin", "debian"); e.Commit != "new" || len(changed) != 0 {
		t.Errorf("got %v changed %v", e, changed)
	}
}
