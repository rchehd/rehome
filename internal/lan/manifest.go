package lan

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/rchehd/rehome/internal/node"
)

// SigFile holds a node folder's signed manifest, next to node.yml.
const SigFile = "manifest.sig"

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest lists every file of a node folder but the signature itself, sorted by slash path.
type Manifest struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Files    []File `json:"files"`
}

type signedManifest struct {
	Manifest Manifest `json:"manifest"`
	Sig      string   `json:"sig"`
}

func BuildManifest(dir, id string, revision int) (Manifest, error) {
	m := Manifest{ID: id, Revision: revision, Files: []File{}}
	root, err := os.OpenRoot(dir)

	if err != nil {
		return m, err
	}

	defer root.Close()

	fsys := root.FS()
	err = fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			return fmt.Errorf("%s: only regular files can be signed", rel)
		case rel == SigFile:
			return nil
		}

		data, err := fs.ReadFile(fsys, rel)

		if err != nil {
			return err
		}

		sum := sha256.Sum256(data)
		m.Files = append(m.Files, File{Path: rel, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])})

		return nil
	})

	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })

	return m, err
}

// Canonical is the exact bytes a manifest's signature covers.
func (m Manifest) Canonical() []byte {
	data, _ := json.Marshal(m)

	return data
}

// SignFolder signs n's folder with this machine's key.
func SignFolder(dir string, n *node.Node, id *Identity) error {
	m, err := BuildManifest(dir, n.ID, n.Revision)

	if err != nil {
		return err
	}

	data, err := json.Marshal(signedManifest{Manifest: m, Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(id.Private, m.Canonical()))})

	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, SigFile), data, 0o644) // #nosec G306 -- node folders are world-readable, like the rest of the config repo
}

// ReadSigned returns a folder's signed manifest without checking it.
func ReadSigned(dir string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, SigFile)) // #nosec G304 -- a node folder inside the config repo
}

// VerifySigned checks a signed manifest against key and returns the manifest it covers.
func VerifySigned(data []byte, key ed25519.PublicKey) (Manifest, error) {
	var s signedManifest

	if err := json.Unmarshal(data, &s); err != nil {
		return Manifest{}, fmt.Errorf("signed manifest: %w", err)
	}

	sig, err := base64.StdEncoding.DecodeString(s.Sig)

	if err != nil || !ed25519.Verify(key, s.Manifest.Canonical(), sig) {
		return Manifest{}, errors.New("manifest signature does not match the node's paired key")
	}

	return s.Manifest, nil
}

// VerifyFolder checks that dir is exactly what its owner signed with key, node.yml included.
func VerifyFolder(dir string, key ed25519.PublicKey) (Manifest, error) {
	data, err := ReadSigned(dir)

	if err != nil {
		return Manifest{}, err
	}

	m, err := VerifySigned(data, key)

	if err != nil {
		return Manifest{}, err
	}

	disk, err := BuildManifest(dir, m.ID, m.Revision)

	if err != nil {
		return Manifest{}, err
	}

	if !reflect.DeepEqual(disk, m) {
		return Manifest{}, errors.New("node folder differs from its signed manifest")
	}

	n, err := node.Load(dir)

	if err != nil {
		return Manifest{}, err
	}

	if n.ID != m.ID || n.Revision != m.Revision {
		return Manifest{}, errors.New("node.yml does not match its signed manifest")
	}

	return m, nil
}
