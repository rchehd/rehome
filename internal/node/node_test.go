package node

import (
	"path/filepath"
	"testing"
)

func TestNodeKeepsKeyAndRevision(t *testing.T) {
	n := &Node{ID: "id", Name: "a", Path: t.TempDir(), Key: "a2V5", Revision: 7}

	if err := n.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load(n.Path)

	if err != nil {
		t.Fatal(err)
	}

	if got.Key != "a2V5" || got.Revision != 7 {
		t.Fatalf("round trip lost fields: %+v", got)
	}
}

func TestListSkipsDotFoldersAndFindIDIgnoresNames(t *testing.T) {
	root := t.TempDir()

	for _, n := range []*Node{
		{ID: "id-a", Name: "a", Path: filepath.Join(root, Dir, "a")},
		{ID: "id-a", Name: "a", Path: filepath.Join(root, Dir, ".lan-1")},
		{ID: "id-b", Name: "id-a", Path: filepath.Join(root, Dir, "id-a")},
	} {
		if err := n.Save(); err != nil {
			t.Fatal(err)
		}
	}

	nodes, err := List(root)

	if err != nil || len(nodes) != 2 {
		t.Fatalf("listed %d nodes: %v", len(nodes), err)
	}

	if n := FindID(nodes, "id-a"); n == nil || n.Name != "a" {
		t.Fatalf("FindID took a node named like the id: %+v", n)
	}
}
