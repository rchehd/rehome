package lan

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type entry struct {
	name string
	kind byte
	size int64
	link string
}

func archiveOf(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.kind, Size: e.size, Linkname: e.link, Mode: 0o644}

		if e.kind != tar.TypeReg {
			h.Size = 0
		}

		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}

		if e.kind == tar.TypeReg {
			if _, err := tw.Write(make([]byte, e.size)); err != nil {
				t.Fatal(err)
			}
		}
	}

	tw.Close()
	gz.Close()

	return buf.Bytes()
}

func TestPackExtractRoundTrip(t *testing.T) {
	id := testIdentity(t)
	src := signedNode(t, t.TempDir(), id, "a", 1)
	data, err := Pack(src)

	if err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()

	if err := Extract(bytes.NewReader(data), dst, DefaultLimits); err != nil {
		t.Fatal(err)
	}

	if _, err := VerifyFolder(dst, id.Public()); err != nil {
		t.Fatalf("extracted folder does not verify: %v", err)
	}
}

func TestExtractRejects(t *testing.T) {
	small := Limits{FileBytes: 10, TotalBytes: 15, Entries: 3}
	cases := map[string][]entry{
		"parent":     {{name: "../x", kind: tar.TypeReg, size: 1}},
		"absolute":   {{name: "/etc/x", kind: tar.TypeReg, size: 1}},
		"nested":     {{name: "a/../../x", kind: tar.TypeReg, size: 1}},
		"symlink":    {{name: "l", kind: tar.TypeSymlink, link: "/etc/passwd"}},
		"hardlink":   {{name: "h", kind: tar.TypeLink, link: "x"}},
		"device":     {{name: "d", kind: tar.TypeChar}},
		"big file":   {{name: "f", kind: tar.TypeReg, size: 11}},
		"big total":  {{name: "f1", kind: tar.TypeReg, size: 8}, {name: "f2", kind: tar.TypeReg, size: 8}},
		"many files": {{name: "1", kind: tar.TypeReg}, {name: "2", kind: tar.TypeReg}, {name: "3", kind: tar.TypeReg}, {name: "4", kind: tar.TypeReg}},
	}

	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dst := filepath.Join(root, "dst")

			if err := os.Mkdir(dst, 0o755); err != nil {
				t.Fatal(err)
			}

			if err := Extract(bytes.NewReader(archiveOf(t, entries...)), dst, small); err == nil {
				t.Fatal("extracted a bad archive")
			}

			if _, err := os.Stat(filepath.Join(root, "x")); err == nil {
				t.Fatal("wrote outside the destination")
			}
		})
	}
}
