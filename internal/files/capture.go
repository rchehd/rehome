package files

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/secrets"
)

const maxFileSize = 1 << 20

// Change is a settings file whose redacted system copy differs from files/.
type Change struct {
	// Src is the absolute system path, Dst the config-relative path.
	Src, Dst string
	Old, New []byte
	Mode     fs.FileMode
	Findings []secrets.Finding
	// Shared marks a file kept at the config root for every node, such as an apt keyring.
	Shared bool
}

// Capture collects an app's settings files by its catalog rules.
func Capture(e *catalog.Entry, fileKey, root string) ([]Change, []config.FileMap, error) {
	return CaptureRules(e.ID, e.Files, fileKey, root, false)
}

// CaptureRules collects settings files by rules per file key: changes to write and the
// file mappings for the config.
func CaptureRules(id string, rules map[string]config.FileRule, fileKey, root string, harnessData bool) ([]Change, []config.FileMap, error) {
	rule, ok := rules[fileKey]
	rule = rule.WithAutoDefaults()

	if !ok || rule.Base == "" {
		return nil, nil, nil
	}

	base := adapter.Home(rule.Base)
	rels := (&walker{}).walk(base, rule)

	var changes []Change
	var maps []config.FileMap
	limit := int64(maxFileSize)

	if rule.MaxSizeMB > 0 {
		limit = int64(rule.MaxSizeMB) << 20
	}

	for _, rel := range rels {
		src := filepath.Join(base, rel)
		harnessFile := isHarnessData(src)

		if harnessFile && !harnessData {
			continue
		}

		info, err := os.Stat(src)

		if err != nil || info.Size() > limit {
			continue
		}

		raw, err := os.ReadFile(src)

		if err != nil {
			return nil, nil, err
		}

		if rule.TextOnly && secrets.IsBinary(raw) {
			continue
		}

		dst := path.Join("files", id, filepath.ToSlash(rel))
		maps = append(maps, fileMap(rules, dst, rel, info.Mode().Perm(), harnessFile))
		redacted, findings := secrets.Redact(adapter.Tilde(src), raw)
		old, _ := os.ReadFile(filepath.Join(root, dst))

		if old != nil && bytes.Equal(old, redacted) {
			continue
		}

		changes = append(changes, Change{Src: src, Dst: dst, Old: old, New: redacted, Mode: info.Mode().Perm(), Findings: findings})
	}

	return changes, maps, nil
}

// CopyChange builds a change for a single file outside any catalog rule, e.g. apt sources.
func CopyChange(src, dst, root string) (*Change, error) {
	info, err := os.Stat(src)

	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(src)

	if err != nil {
		return nil, err
	}

	redacted, findings := secrets.Redact(src, raw)
	old, _ := os.ReadFile(filepath.Join(root, dst))

	if old != nil && bytes.Equal(old, redacted) {
		return nil, nil
	}

	return &Change{Src: src, Dst: dst, Old: old, New: redacted, Mode: info.Mode().Perm(), Findings: findings}, nil
}

func WriteChanges(root string, cs []Change) error {
	for _, c := range cs {
		dst := filepath.Join(root, c.Dst)

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		if err := os.WriteFile(dst, c.New, c.Mode|0o600); err != nil {
			return err
		}
	}

	return nil
}

func fileMap(rules map[string]config.FileRule, dst, rel string, perm fs.FileMode, harnessFile bool) config.FileMap {
	m := config.FileMap{Src: dst, Dest: map[string]string{}}

	if perm&0o077 == 0 || harnessFile && perm&0o111 != 0 {
		m.Mode = fmt.Sprintf("%04o", perm)
	}

	for _, key := range []string{"linux", "macos"} {
		if r, ok := rules[key]; ok && r.Base != "" {
			m.Dest[key] = path.Join(r.Base, filepath.ToSlash(rel))
		}
	}

	return m
}

// Select lists the files a rule picks under base, relative to it.
func Select(base string, rule config.FileRule) ([]string, error) {
	w := &walker{}

	return w.walk(base, rule), nil
}

// SelectWithin is Select that stops once the picked files of up to 1 MB each exceed limit bytes.
func SelectWithin(base string, rule config.FileRule, limit int64) (rels []string, over bool) {
	w := &walker{limit: limit}
	rels = w.walk(base, rule)

	return rels, w.over()
}

// walk visits only the directories an include pattern can reach. It follows symlinks,
// so dotfiles linked in by stow and similar tools are found; a loop is walked once.
func (w *walker) walk(base string, rule config.FileRule) []string {
	w.base, w.rule, w.seen = base, rule.WithAutoDefaults(), map[string]bool{}

	for _, inc := range rule.Include {
		if w.over() {
			break
		}

		start := staticPrefix(inc)
		info, err := os.Stat(filepath.Join(base, start))

		if err != nil {
			continue
		}

		if !info.IsDir() {
			w.add(start, info.Size())

			continue
		}

		w.visited = map[string]bool{}
		w.dir(inc, start)
	}

	return w.rels
}

type walker struct {
	base    string
	rule    config.FileRule
	seen    map[string]bool
	visited map[string]bool
	rels    []string
	// limit stops the walk once size exceeds it; zero means no limit.
	limit, size int64
}

func (w *walker) over() bool {
	return w.limit > 0 && w.size > w.limit
}

func (w *walker) add(rel string, size int64) {
	name := filepath.Base(rel)

	if strings.Contains(name, BackupSuffix) || strings.HasPrefix(name, ".rehome-secret-") {
		return
	}

	if w.rule.Matches(rel) && !w.seen[rel] {
		w.seen[rel] = true
		w.rels = append(w.rels, rel)

		if size <= 1<<20 {
			w.size += size
		}
	}
}

func (w *walker) dir(inc, rel string) {
	abs := filepath.Join(w.base, rel)
	real, err := filepath.EvalSymlinks(abs)

	if err != nil || w.visited[real] {
		return
	}

	w.visited[real] = true
	entries, err := os.ReadDir(abs)

	if err != nil {
		return
	}

	for _, e := range entries {
		if w.over() {
			return
		}

		child := path.Join(rel, e.Name())
		info, err := os.Stat(filepath.Join(w.base, child))

		if err != nil {
			continue
		}

		if !info.IsDir() {
			if info.Mode().IsRegular() {
				w.add(child, info.Size())
			}

			continue
		}

		// A nested .git would become an embedded repo (gitlink) in the config repo.
		if e.Name() != ".git" && couldContain(inc, child) && !excludedDir(w.rule, child) {
			w.dir(inc, child)
		}
	}
}

// StaticPrefix is the leading part of a glob without wildcards.
func StaticPrefix(pattern string) string {
	return staticPrefix(pattern)
}

// staticPrefix is the leading part of a glob without wildcards: "skills/**" → "skills".
func staticPrefix(pattern string) string {
	segs := strings.Split(pattern, "/")
	var static []string

	for _, s := range segs {
		if strings.ContainsAny(s, "*?[") {
			break
		}

		static = append(static, s)
	}

	return strings.Join(static, "/")
}

// couldContain reports whether files under dir can match the pattern.
func couldContain(pattern, dir string) bool {
	psegs := strings.Split(pattern, "/")
	dsegs := strings.Split(dir, "/")

	for i, d := range dsegs {
		if i >= len(psegs)-1 {
			return i < len(psegs) && psegs[i] == "**"
		}

		if psegs[i] == "**" {
			return true
		}

		if ok, _ := path.Match(psegs[i], d); !ok {
			return false
		}
	}

	return true
}

func excludedDir(rule config.FileRule, dir string) bool {
	return !rule.Matches(dir+"/.probe") && !rule.Matches(dir+"/x/.probe") && rule.Excludes(dir+"/.probe")
}
