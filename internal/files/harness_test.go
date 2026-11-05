package files

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/secrets"
)

func TestHarnessDataRoundTrip(t *testing.T) {
	from, to, root := t.TempDir(), t.TempDir(), t.TempDir()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}

	fixtures := []struct {
		id, path string
	}{
		{"claude-code", ".claude/agents/review.md"},
		{"claude-code", ".claude/skills/check/SKILL.md"},
		{"codex", ".codex/agents/review.toml"},
		{"codex", ".codex/skills/check/SKILL.md"},
		{"cursor-agent-data", ".cursor/agents/review.md"},
		{"cursor-agent-data", ".cursor/skills/check/SKILL.md"},
		{"shared-agent-skills", ".agents/skills/check/SKILL.md"},
		{"shared-agent-skills", ".agents/skills/check/scripts/check.sh"},
	}
	token := "sk-ant-abcdefghijklmnopqrstuvwxyz"
	for _, f := range fixtures {
		writeHarnessFixture(t, from, f.path, token, 0o755)
	}
	writeHarnessFixture(t, from, ".codex/config.toml", "model = 'test'", 0o644)
	writeHarnessFixture(t, from, ".codex/auth.json", token, 0o600)
	writeHarnessFixture(t, from, ".codex/sessions/example.jsonl", token, 0o600)
	writeHarnessFixture(t, from, ".codex/skills/.system/builtin/SKILL.md", "built-in", 0o644)

	t.Setenv("HOME", from)
	var allMaps []config.FileMap
	for _, id := range []string{"claude-code", "codex", "cursor-agent-data", "shared-agent-skills"} {
		entry, ok := cat.Get(id)
		if !ok {
			t.Fatalf("missing catalog entry %s", id)
		}

		changes, _, err := CaptureRules(id, entry.Files, "linux", root, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			if c.Dst != "files/codex/config.toml" {
				t.Fatalf("default capture included %s", c.Dst)
			}
		}

		changes, mappings, err := CaptureRules(id, entry.Files, "linux", root, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteChanges(root, changes); err != nil {
			t.Fatal(err)
		}
		allMaps = append(allMaps, mappings...)
	}
	if len(allMaps) != len(fixtures)+1 {
		t.Fatalf("captured unexpected mappings: %+v", allMaps)
	}

	t.Setenv("HOME", to)
	if _, err := Apply(t.Context(), root, allMaps, ApplyOptions{FileKey: "macos"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if _, err := os.Stat(filepath.Join(to, f.path)); !os.IsNotExist(err) {
			t.Fatalf("default restore wrote %s: %v", f.path, err)
		}
	}
	if _, err := Apply(t.Context(), root, allMaps, ApplyOptions{FileKey: "macos", HarnessData: true}); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		path := filepath.Join(to, f.path)
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, []byte(secrets.Placeholder)) {
			t.Errorf("restore %s: contents = %q, err = %v", f.path, data, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Errorf("restore %s did not preserve executable mode: %v", f.path, err)
		}
	}
}

func writeHarnessFixture(t *testing.T, home, rel, body string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}
