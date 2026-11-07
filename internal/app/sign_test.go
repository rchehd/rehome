package app

import (
	"path/filepath"
	"testing"

	"github.com/rchehd/rehome/internal/lan"
	"github.com/rchehd/rehome/internal/node"
)

func TestSaveNodeBumpsRevisionAndSigns(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := lan.Dir()

	if err != nil {
		t.Fatal(err)
	}

	id, err := lan.LoadIdentity(dir)

	if err != nil {
		t.Fatal(err)
	}

	n := &node.Node{ID: "a", Name: "a", Path: filepath.Join(t.TempDir(), "a"), Revision: 4}

	if err := SaveNode(n); err != nil {
		t.Fatal(err)
	}

	m, err := lan.VerifyFolder(n.Path, id.Public())

	if err != nil || m.Revision != 5 || n.Key != lan.EncodeKey(id.Public()) {
		t.Fatalf("revision %d key %q: %v", m.Revision, n.Key, err)
	}
}

func TestSaveNodeWithoutLANLeavesRevision(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	n := &node.Node{ID: "a", Name: "a", Path: filepath.Join(t.TempDir(), "a"), Revision: 4}

	if err := SaveNode(n); err != nil {
		t.Fatal(err)
	}

	if n.Revision != 4 || n.Key != "" {
		t.Fatalf("git-only save changed the node: %+v", n)
	}
}
