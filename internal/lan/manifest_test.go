package lan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rchehd/rehome/internal/node"
)

// signedNode writes a node folder with one settings file and signs it with id.
func signedNode(t *testing.T, root string, id *Identity, nodeID string, revision int) string {
	t.Helper()
	dir := filepath.Join(root, node.Dir, nodeID)
	n := &node.Node{ID: nodeID, Name: nodeID, Path: dir, Key: EncodeKey(id.Public()), Revision: revision}

	if err := n.Save(); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "files", "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "files", "zsh", ".zshrc"), []byte("export A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SignFolder(dir, n, id); err != nil {
		t.Fatal(err)
	}

	return dir
}

func testIdentity(t *testing.T) *Identity {
	t.Helper()
	id, err := LoadIdentity(t.TempDir())

	if err != nil {
		t.Fatal(err)
	}

	return id
}

func TestManifestIsSortedAndSkipsSig(t *testing.T) {
	id := testIdentity(t)
	dir := signedNode(t, t.TempDir(), id, "a", 1)
	m, err := BuildManifest(dir, "a", 1)

	if err != nil {
		t.Fatal(err)
	}

	if len(m.Files) != 2 || m.Files[0].Path != "files/zsh/.zshrc" || m.Files[1].Path != node.FileName {
		t.Fatalf("files %+v", m.Files)
	}
}

func TestVerifyFolderAcceptsUntouched(t *testing.T) {
	id := testIdentity(t)
	dir := signedNode(t, t.TempDir(), id, "a", 3)
	m, err := VerifyFolder(dir, id.Public())

	if err != nil || m.Revision != 3 || m.ID != "a" {
		t.Fatalf("verify: %+v %v", m, err)
	}
}

func TestVerifyFolderRejectsEditedAddedRemovedFile(t *testing.T) {
	for name, change := range map[string]func(dir string) error{
		"edited": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "files", "zsh", ".zshrc"), []byte("export A=2\n"), 0o644)
		},
		"added":   func(dir string) error { return os.WriteFile(filepath.Join(dir, "files", "extra"), []byte("x"), 0o644) },
		"removed": func(dir string) error { return os.Remove(filepath.Join(dir, "files", "zsh", ".zshrc")) },
	} {
		t.Run(name, func(t *testing.T) {
			id := testIdentity(t)
			dir := signedNode(t, t.TempDir(), id, "a", 1)

			if err := change(dir); err != nil {
				t.Fatal(err)
			}

			if _, err := VerifyFolder(dir, id.Public()); err == nil {
				t.Fatal("accepted a changed folder")
			}
		})
	}
}

func TestVerifyFolderRejectsOtherKey(t *testing.T) {
	dir := signedNode(t, t.TempDir(), testIdentity(t), "a", 1)

	if _, err := VerifyFolder(dir, testIdentity(t).Public()); err == nil {
		t.Fatal("accepted a folder signed by another key")
	}
}

func TestVerifyFolderRejectsChangedRevisionInNodeYml(t *testing.T) {
	id := testIdentity(t)
	dir := signedNode(t, t.TempDir(), id, "a", 1)
	n, err := node.Load(dir)

	if err != nil {
		t.Fatal(err)
	}

	n.Revision = 9

	if err := n.Save(); err != nil {
		t.Fatal(err)
	}

	if _, err := VerifyFolder(dir, id.Public()); err == nil {
		t.Fatal("accepted a bumped revision")
	}
}
