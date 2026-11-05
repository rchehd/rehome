package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/node"
)

// marker records what the working copy last matched, so a pull keeps local recipe edits.
const marker = ".rehome-archive"

type mark struct {
	Archive string `yaml:"archive"`
	Recipes string `yaml:"recipes"`
}

// Archive is a .tar.gz laid out like a repo, unpacked into a working copy; an https archive is read-only.
type Archive struct {
	Ref  string
	Path string
	// Own is this machine's node id; a pull never overwrites that node.
	Own    string
	DryRun bool
}

func (a *Archive) Dir() string {
	return a.Path
}

func (a *Archive) remote() bool {
	return strings.HasPrefix(a.Ref, "https://") || strings.HasPrefix(a.Ref, "http://")
}

// Pull takes every other node and the shared files from the archive, and its recipes unless
// they were edited here since the last pull.
func (a *Archive) Pull(ctx context.Context) error {
	data, err := a.read(ctx)

	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	m := a.mark()
	sum := hash(data)

	if m.Archive == sum || a.DryRun {
		return nil
	}

	tmp, err := os.MkdirTemp(filepath.Dir(a.Path), ".rehome-pull-*")

	if err != nil {
		return err
	}

	defer os.RemoveAll(tmp)

	if err := extract(data, tmp); err != nil {
		return err
	}

	local, _ := os.ReadFile(config.FilePath(a.Path))
	keepRecipes := local != nil && hash(local) != m.Recipes
	entries, err := os.ReadDir(tmp)

	if err != nil {
		return err
	}

	for _, e := range entries {
		switch e.Name() {
		case config.FileName:
			if keepRecipes {
				continue
			}
		case node.Dir:
			if err := a.pullNodes(filepath.Join(tmp, node.Dir)); err != nil {
				return err
			}

			continue
		}

		if err := replace(filepath.Join(tmp, e.Name()), filepath.Join(a.Path, e.Name())); err != nil {
			return err
		}
	}

	recipes, _ := os.ReadFile(config.FilePath(a.Path))

	return a.save(mark{Archive: sum, Recipes: hash(recipes)})
}

func (a *Archive) pullNodes(from string) error {
	entries, err := os.ReadDir(from)

	if err != nil {
		return err
	}

	for _, e := range entries {
		if n, err := node.Load(filepath.Join(from, e.Name())); err == nil && n.ID == a.Own {
			continue
		}

		if err := replace(filepath.Join(from, e.Name()), filepath.Join(a.Path, node.Dir, e.Name())); err != nil {
			return err
		}
	}

	return nil
}

// Publish packs the working copy over the archive through a temporary file.
func (a *Archive) Publish(ctx context.Context, _ string) error {
	if a.remote() {
		return fmt.Errorf("%s is read-only; publish to a local archive or a git repo", a.Ref)
	}

	if err := a.Pull(ctx); err != nil {
		return err
	}

	if a.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] pack %s into %s\n", a.Path, a.Ref)

		return nil
	}

	data, err := pack(a.Path)

	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(a.Ref), ".rehome-*.tar.gz")

	if err != nil {
		return err
	}

	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()

		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmp.Name(), a.Ref); err != nil {
		return err
	}

	recipes, _ := os.ReadFile(config.FilePath(a.Path))

	return a.save(mark{Archive: hash(data), Recipes: hash(recipes)})
}

func (a *Archive) Status(ctx context.Context) Status {
	data, err := a.read(ctx)

	if err != nil {
		return gitsync.SourceUnknown
	}

	if hash(data) != a.mark().Archive {
		return gitsync.SourceChanged
	}

	info, err := os.Stat(filepath.Join(a.Path, marker))

	if err != nil {
		return gitsync.SourceDirty
	}

	changed := false
	_ = filepath.WalkDir(a.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || changed {
			return err
		}

		if fi, err := d.Info(); err == nil && !d.IsDir() && fi.ModTime().After(info.ModTime()) {
			changed = true
		}

		return nil
	})

	if changed {
		return gitsync.SourceDirty
	}

	return gitsync.SourceCurrent
}

func (a *Archive) Backup(ctx context.Context) (string, error) {
	if a.remote() {
		dir := backupDir(a.Path)

		if err := os.RemoveAll(dir); err != nil {
			return "", err
		}

		return dir, copyTree(a.Path, dir)
	}

	data, err := os.ReadFile(a.Ref)

	if err != nil {
		return "", err
	}

	path := strings.TrimSuffix(strings.TrimSuffix(a.Ref, ".tgz"), ".tar.gz") + ".v1.bak.tar.gz"

	return path, os.WriteFile(path, data, 0o600)
}

func (a *Archive) read(ctx context.Context) ([]byte, error) {
	if !a.remote() {
		return os.ReadFile(a.Ref)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Ref, nil)

	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", a.Ref, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func (a *Archive) mark() mark {
	var m mark

	if data, err := os.ReadFile(filepath.Join(a.Path, marker)); err == nil {
		_ = yaml.Unmarshal(data, &m)
	}

	return m
}

func (a *Archive) save(m mark) error {
	data, err := yaml.Marshal(m)

	if err != nil {
		return err
	}

	if err := os.MkdirAll(a.Path, 0o755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(a.Path, marker), data, 0o644)
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// replace swaps dst for src: the old copy is removed only once the new one is in place.
func replace(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	old := dst + ".rehome-old"

	if err := os.RemoveAll(old); err != nil {
		return err
	}

	if err := os.Rename(dst, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := os.Rename(src, dst); err != nil {
		return err
	}

	return os.RemoveAll(old)
}

func extract(data []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))

	if err != nil {
		return err
	}

	tr := tar.NewReader(gz)

	for {
		h, err := tr.Next()

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		name := filepath.FromSlash(h.Name)

		if !filepath.IsLocal(name) {
			return fmt.Errorf("archive entry %q leaves the config folder", h.Name)
		}

		target := filepath.Join(dir, name)

		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}

			body, err := io.ReadAll(tr)

			if err != nil {
				return err
			}

			if err := os.WriteFile(target, body, fs.FileMode(h.Mode).Perm()); err != nil {
				return err
			}
		}
	}
}

// pack archives every regular file under dir except the marker, in walk order.
func pack(dir string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, p)

		if err != nil || rel == "." || rel == marker || !d.Type().IsRegular() && !d.IsDir() {
			return err
		}

		info, err := d.Info()

		if err != nil {
			return err
		}

		h, err := tar.FileInfoHeader(info, "")

		if err != nil {
			return err
		}

		h.Name = filepath.ToSlash(rel)

		if d.IsDir() {
			h.Name += "/"
		}

		if err := tw.WriteHeader(h); err != nil || d.IsDir() {
			return err
		}

		data, err := os.ReadFile(p)

		if err != nil {
			return err
		}

		_, err = tw.Write(data)

		return err
	})

	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	if err := gz.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
