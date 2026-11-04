package files

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/secrets"
)

func put(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)

	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func captureFixture(t *testing.T) (*catalog.Entry, string, string) {
	t.Helper()

	base, root := t.TempDir(), t.TempDir()

	for rel, body := range map[string]string{
		"a.conf":           "a=1\n",
		"b.txt":            "b\n",
		"sub/x.json":       "{}\n",
		"sub/skip/y":       "y\n",
		"sub/.git/config":  "[core]\n",
		"secret.conf":      "export API_TOKEN=abcdef1234567890\n",
		"big.conf":         strings.Repeat("x", 1<<20+1),
		"sub/skip/z.conf":  "z\n",
		"sub/nested/n.txt": "n\n",
	} {
		put(t, filepath.Join(base, rel), body)
	}

	e := &catalog.Entry{ID: "app", Files: map[string]catalog.FileRule{
		"linux": {Base: base, Include: []string{"*.conf", "sub/**"}, Exclude: []string{"sub/skip/**"}},
	}}

	return e, base, root
}

func TestCaptureSelectsFiles(t *testing.T) {
	e, base, root := captureFixture(t)

	changes, maps, err := Capture(e, "linux", root)

	if err != nil {
		t.Fatal(err)
	}

	var dsts, srcs []string

	for _, c := range changes {
		dsts = append(dsts, c.Dst)
	}

	for _, m := range maps {
		srcs = append(srcs, m.Src)

		if rel := strings.TrimPrefix(m.Src, "files/app/"); m.Dest["linux"] != filepath.Join(base, rel) {
			t.Errorf("dest = %q", m.Dest["linux"])
		}
	}

	want := []string{"files/app/a.conf", "files/app/secret.conf", "files/app/sub/nested/n.txt", "files/app/sub/x.json"}
	slices.Sort(dsts)
	slices.Sort(srcs)

	if !slices.Equal(dsts, want) || !slices.Equal(srcs, want) {
		t.Errorf("dsts = %v\nsrcs = %v\nwant %v", dsts, srcs, want)
	}
}

func TestCaptureRedactsSecrets(t *testing.T) {
	e, _, root := captureFixture(t)

	changes, _, err := Capture(e, "linux", root)

	if err != nil {
		t.Fatal(err)
	}

	for _, c := range changes {
		if c.Dst != "files/app/secret.conf" {
			continue
		}

		if !strings.Contains(string(c.New), secrets.Placeholder) || strings.Contains(string(c.New), "abcdef1234567890") {
			t.Errorf("new = %q", c.New)
		}

		if len(c.Findings) != 1 {
			t.Errorf("findings = %+v", c.Findings)
		}

		return
	}

	t.Error("secret.conf change missing")
}

func TestCaptureIsStableAfterWrite(t *testing.T) {
	e, _, root := captureFixture(t)

	changes, _, err := Capture(e, "linux", root)

	if err != nil {
		t.Fatal(err)
	}

	if err := WriteChanges(root, changes); err != nil {
		t.Fatal(err)
	}

	again, maps, err := Capture(e, "linux", root)

	if err != nil {
		t.Fatal(err)
	}

	if len(again) != 0 {
		t.Errorf("second capture has %d changes", len(again))
	}

	if len(maps) != len(changes) {
		t.Errorf("maps = %d, want %d", len(maps), len(changes))
	}
}

func TestCaptureNoRuleForKey(t *testing.T) {
	e, _, root := captureFixture(t)

	changes, maps, err := Capture(e, "macos", root)

	if err != nil || changes != nil || maps != nil {
		t.Errorf("got %v, %v, %v", changes, maps, err)
	}
}

func TestCopyChange(t *testing.T) {
	src := filepath.Join(t.TempDir(), "s.list")
	root := t.TempDir()
	put(t, src, "deb https://x.test noble main\n")

	c, err := CopyChange(src, "files/apt/s.list", root)

	if err != nil || c == nil {
		t.Fatalf("got %v, %v", c, err)
	}

	if err := WriteChanges(root, []Change{*c}); err != nil {
		t.Fatal(err)
	}

	if again, err := CopyChange(src, "files/apt/s.list", root); err != nil || again != nil {
		t.Errorf("unchanged copy returned %v, %v", again, err)
	}
}

func TestStaticPrefix(t *testing.T) {
	tests := map[string]string{"skills/**": "skills", "*.md": "", "a/b/c.txt": "a/b/c.txt", "a/*/c": "a"}

	for in, want := range tests {
		if got := staticPrefix(in); got != want {
			t.Errorf("staticPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyCopiesAndDetectsIdentical(t *testing.T) {
	root, dest := t.TempDir(), filepath.Join(t.TempDir(), "sub", "out.conf")
	put(t, filepath.Join(root, "files/out.conf"), "a=1\n")
	maps := []config.FileMap{{Src: "files/out.conf", Dest: map[string]string{"linux": dest}}}

	res, err := Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux"})

	if err != nil || res[0].Status != StatusCopied || read(t, dest) != "a=1\n" {
		t.Fatalf("copy: %+v, %v", res, err)
	}

	res, err = Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux"})

	if err != nil || res[0].Status != StatusIdentical {
		t.Errorf("identical: %+v, %v", res, err)
	}
}

func TestApplyIdenticalWhenOnlySecretDiffers(t *testing.T) {
	root, dest := t.TempDir(), filepath.Join(t.TempDir(), "env.conf")
	put(t, filepath.Join(root, "files/env.conf"), "export API_TOKEN="+secrets.Placeholder+"\n")
	put(t, dest, "export API_TOKEN=abcdef1234567890\n")
	maps := []config.FileMap{{Src: "files/env.conf", Dest: map[string]string{"linux": dest}}}

	res, err := Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux"})

	if err != nil || res[0].Status != StatusIdentical || res[0].Redacted {
		t.Fatalf("got %+v, %v", res, err)
	}

	if read(t, dest) != "export API_TOKEN=abcdef1234567890\n" {
		t.Error("local secret was touched")
	}
}

func TestApplyKeepsLocalThenOverwrites(t *testing.T) {
	root, dest := t.TempDir(), filepath.Join(t.TempDir(), "out.conf")
	put(t, filepath.Join(root, "files/out.conf"), "new\n")
	put(t, dest, "old\n")
	maps := []config.FileMap{{Src: "files/out.conf", Dest: map[string]string{"linux": dest}}}

	res, err := Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux"})

	if err != nil || res[0].Status != StatusKeptLocal {
		t.Fatalf("kept: %+v, %v", res, err)
	}

	if string(res[0].Local) != "old\n" || string(res[0].Repo) != "new\n" || read(t, dest) != "old\n" {
		t.Errorf("kept-local result = %+v", res[0])
	}

	res, err = Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux", Overwrite: true})

	if err != nil || res[0].Status != StatusOverwritten {
		t.Fatalf("overwrite: %+v, %v", res, err)
	}

	if read(t, dest) != "new\n" || read(t, dest+BackupSuffix) != "old\n" {
		t.Error("dest or backup content wrong")
	}
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	root, tmp := t.TempDir(), t.TempDir()
	put(t, filepath.Join(root, "files/a"), "new\n")
	put(t, filepath.Join(root, "files/b"), "new\n")
	missing, existing := filepath.Join(tmp, "a"), filepath.Join(tmp, "b")
	put(t, existing, "old\n")
	maps := []config.FileMap{
		{Src: "files/a", Dest: map[string]string{"linux": missing}},
		{Src: "files/b", Dest: map[string]string{"linux": existing}},
	}

	res, err := Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux", Overwrite: true, DryRun: true})

	if err != nil || res[0].Status != StatusCopied || res[1].Status != StatusOverwritten {
		t.Fatalf("got %+v, %v", res, err)
	}

	if _, err := os.Stat(missing); err == nil {
		t.Error("dry-run created a file")
	}

	if read(t, existing) != "old\n" {
		t.Error("dry-run changed a file")
	}

	if _, err := os.Stat(existing + BackupSuffix); err == nil {
		t.Error("dry-run wrote a backup")
	}
}

func TestApplyMissingSourceAndDestination(t *testing.T) {
	root := t.TempDir()
	maps := []config.FileMap{
		{Src: "files/none", Dest: map[string]string{"linux": filepath.Join(t.TempDir(), "x")}},
		{Src: "files/none", Dest: map[string]string{"macos": "/x"}},
	}

	res, err := Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux"})

	if err != nil {
		t.Fatal(err)
	}

	if res[0].Status != StatusNoSource || res[1].Status != StatusNoDest {
		t.Errorf("statuses = %s, %s", res[0].Status, res[1].Status)
	}
}

func TestApplyFlagsRedactedCopy(t *testing.T) {
	root, dest := t.TempDir(), filepath.Join(t.TempDir(), "env")
	put(t, filepath.Join(root, "files/env"), "export API_TOKEN="+secrets.Placeholder+"\n")
	maps := []config.FileMap{{Src: "files/env", Dest: map[string]string{"linux": dest}}}

	res, err := Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux"})

	if err != nil || res[0].Status != StatusCopied || !res[0].Redacted {
		t.Errorf("got %+v, %v", res, err)
	}
}

func TestApplyExpandsHome(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	put(t, filepath.Join(root, "files/rc"), "x\n")
	maps := []config.FileMap{{Src: "files/rc", Dest: map[string]string{"linux": "~/.rc"}}}

	if _, err := Apply(t.Context(), root, maps, ApplyOptions{FileKey: "linux"}); err != nil {
		t.Fatal(err)
	}

	if read(t, filepath.Join(home, ".rc")) != "x\n" {
		t.Error("~ was not expanded to HOME")
	}
}

func TestOverwriteWritesBackup(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out.conf")
	put(t, dest, "old\n")

	err := Overwrite(ApplyResult{Dest: dest, Local: []byte("old\n"), Repo: []byte("new\n")})

	if err != nil {
		t.Fatal(err)
	}

	if read(t, dest) != "new\n" || read(t, dest+BackupSuffix) != "old\n" {
		t.Error("dest or backup content wrong")
	}
}

func TestUnifiedShowsChanges(t *testing.T) {
	got := Unified([]byte("a\nold\nc\n"), []byte("a\nnew\nc\n"), "f")

	for _, want := range []string{"--- f (current)", "+++ f (new)", "-old", "+new"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestUnifiedNewFileIsAllAdditions(t *testing.T) {
	got := Unified(nil, []byte("x\ny\n"), "f")

	if !strings.Contains(got, "+x\n+y\n") || strings.Contains(got, "\n-") {
		t.Errorf("diff = %q", got)
	}
}

func TestUnifiedTooLarge(t *testing.T) {
	big := []byte(strings.Repeat("line\n", maxDiffLines))

	got := Unified(big, []byte("x\n"), "f")

	if !strings.Contains(got, "too large to show") {
		t.Errorf("diff = %q", got[:min(len(got), 80)])
	}
}

func TestCaptureRulesUserRule(t *testing.T) {
	base := t.TempDir()
	root := t.TempDir()
	put(t, filepath.Join(base, "config.fish"), "set -g fish_greeting\n")
	rules := map[string]config.FileRule{"linux": {Base: base, Include: []string{"*.fish"}}}
	changes, maps, err := CaptureRules("fish", rules, "linux", root)

	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 1 || changes[0].Dst != "files/fish/config.fish" || maps[0].Dest["linux"] != filepath.Join(base, "config.fish") {
		t.Errorf("changes %v maps %v", changes, maps)
	}
}
