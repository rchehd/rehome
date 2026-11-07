package lan

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Limits bound what one node archive may unpack to.
type Limits struct {
	FileBytes  int64
	TotalBytes int64
	Entries    int
}

var DefaultLimits = Limits{FileBytes: 20 << 20, TotalBytes: 200 << 20, Entries: 20000}

// Pack makes a tar.gz of dir's regular files with slash paths relative to dir.
func Pack(dir string) ([]byte, error) {
	root, err := os.OpenRoot(dir)

	if err != nil {
		return nil, err
	}

	defer root.Close()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	fsys := root.FS()
	err = fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: only regular files can be shared", rel)
		}

		data, err := fs.ReadFile(fsys, rel)

		if err != nil {
			return err
		}

		if err := tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
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

// Extract unpacks a node archive into the empty dir dst, refusing anything outside it or over l.
func Extract(r io.Reader, dst string, l Limits) error {
	root, err := os.OpenRoot(dst)

	if err != nil {
		return err
	}

	defer root.Close()

	gz, err := gzip.NewReader(io.LimitReader(r, l.TotalBytes+1<<20))

	if err != nil {
		return err
	}

	defer gz.Close()

	tr := tar.NewReader(gz)
	var total int64

	for entries := 0; ; entries++ {
		h, err := tr.Next()

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		if entries >= l.Entries {
			return fmt.Errorf("node archive has more than %d entries", l.Entries)
		}

		name := filepath.FromSlash(h.Name)

		if !filepath.IsLocal(name) {
			return fmt.Errorf("archive entry %q leaves the node folder", h.Name)
		}

		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}

			continue
		case tar.TypeReg:
		default:
			return fmt.Errorf("archive entry %q is not a regular file", h.Name)
		}

		if h.Size > l.FileBytes {
			return fmt.Errorf("archive entry %q is larger than %d bytes", h.Name, l.FileBytes)
		}

		if total += h.Size; total > l.TotalBytes {
			return fmt.Errorf("node archive is larger than %d bytes", l.TotalBytes)
		}

		if err := writeEntry(root, tr, name, h.Size); err != nil {
			return err
		}
	}
}

func writeEntry(root *os.Root, r io.Reader, name string, size int64) error {
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}

	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)

	if err != nil {
		return err
	}

	_, err = io.CopyN(f, r, size)

	if closeErr := f.Close(); err == nil {
		err = closeErr
	}

	return err
}
