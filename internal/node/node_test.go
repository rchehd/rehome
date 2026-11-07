package node

import "testing"

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
