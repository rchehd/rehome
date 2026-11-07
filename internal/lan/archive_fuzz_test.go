package lan

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func FuzzExtract(f *testing.F) {
	f.Add([]byte{})
	f.Add(archiveOfFuzzSeed())

	f.Fuzz(func(t *testing.T, data []byte) {
		root := t.TempDir()
		dst := filepath.Join(root, "dst")

		if err := os.Mkdir(dst, 0o755); err != nil {
			t.Fatal(err)
		}

		_ = Extract(bytes.NewReader(data), dst, Limits{FileBytes: 1 << 16, TotalBytes: 1 << 18, Entries: 64})
		entries, err := os.ReadDir(root)

		if err != nil || len(entries) != 1 {
			t.Fatalf("wrote outside the destination: %v", entries)
		}
	})
}

func archiveOfFuzzSeed() []byte {
	dir, err := os.MkdirTemp("", "rehome-fuzz-seed-*")

	if err != nil {
		return nil
	}

	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "node.yml"), []byte("id: a\n"), 0o644); err != nil {
		return nil
	}

	data, _ := Pack(dir)

	return data
}
