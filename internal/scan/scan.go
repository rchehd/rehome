package scan

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/all"
	"github.com/rchehd/rehome/internal/adapter/apt"
	"github.com/rchehd/rehome/internal/ai"
	"github.com/rchehd/rehome/internal/binaries"
	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
)

type Result struct {
	Config      *config.Config
	FileChanges []files.Change
	Findings    []secrets.Finding
	Unknown     []string
	Warnings    []string
}

type scanner struct {
	p    platform.Platform
	cat  *catalog.Catalog
	root string
	apps map[string]*config.App
	res  *Result
}

// Run detects installed software and settings; nothing is written.
func Run(ctx context.Context, r *runner.Runner, p platform.Platform, cat *catalog.Catalog, root string) (*Result, error) {
	s := &scanner{p: p, cat: cat, root: root, apps: map[string]*config.App{}, res: &Result{}}

	for _, a := range all.For(p) {
		found, err := a.Scan(ctx, r)

		if err != nil {
			s.warn("%s scan: %v", a.Name(), err)
		}

		s.addAll(found)
	}

	known := map[string]bool{}

	for _, e := range cat.Present(p.Key()) {
		s.add(adapter.Detected{Manager: "script", Name: e.ID})

		for _, b := range e.Detect.Bins {
			known[b] = true
		}
	}

	bins, err := binaries.Resolve(ctx, r, cat, known)

	if err != nil {
		s.warn("binaries: %v", err)
	}

	s.addAll(bins)
	s.addAll(binaries.GitClones(ctx, r))
	s.pruneRequires()

	if err := s.captureRepos(); err != nil {
		s.warn("apt repos: %v", err)
	}

	if err := s.captureFiles(); err != nil {
		return nil, err
	}

	s.res.Config = &config.Config{Version: config.CurrentVersion, Apps: s.sortedApps(), AptRepos: s.res.Config.AptRepos, Files: s.res.Config.Files}

	return s.res, nil
}

func (s *scanner) addAll(found []adapter.Detected) {
	for _, d := range found {
		s.add(d)
	}
}

func (s *scanner) add(d adapter.Detected) {
	entry, ok := s.cat.Match(d)
	app := &config.App{ID: appID(d), Group: d.Manager, Requires: d.Requires}

	if ok {
		app.ID, app.Group = entry.ID, entry.Group
		app.Install = maps.Clone(entry.Install)
		app.Requires = union(entry.Requires, d.Requires)
		app.PostInstall = entry.PostInstall

		if _, has := entry.Rule(s.p.FileKey()); has {
			app.Files = entry.ID
		}
	}

	if name, pkg := d.Spec.Manager(); name != "" && !sameAsAny(app, name, pkg) {
		if app.Install == nil {
			app.Install = map[string]config.InstallSpec{}
		}

		// What is installed here beats the catalog's default for this platform.
		app.Install[s.p.Key()] = d.Spec
	}

	if d.Manager == "unknown" && !ok {
		disabled := false
		app.ID, app.Group, app.Enabled, app.Detected = "unknown-"+appID(d), config.UnknownGroup, &disabled, d.Meta
		s.res.Unknown = append(s.res.Unknown, app.ID)
	}

	if prev, dup := s.apps[app.ID]; dup {
		mergeInto(prev, app, s.p.Key())

		return
	}

	s.apps[app.ID] = app
}

// mergeInto combines two detections of one app, e.g. a catalog path match and an apt package.
func mergeInto(prev, next *config.App, key string) {
	if _, has := prev.Install[key]; !has {
		if spec, ok := next.Install[key]; ok {
			if prev.Install == nil {
				prev.Install = map[string]config.InstallSpec{}
			}

			prev.Install[key] = spec
		}
	}

	for k, v := range next.Install {
		if _, has := prev.Install[k]; !has {
			prev.Install[k] = v
		}
	}

	prev.Requires = union(prev.Requires, next.Requires)
}

func (s *scanner) pruneRequires() {
	for _, a := range s.apps {
		kept := a.Requires[:0]

		for _, dep := range a.Requires {
			if _, ok := s.apps[dep]; ok && dep != a.ID {
				kept = append(kept, dep)
			}
		}

		a.Requires = kept
	}
}

func (s *scanner) captureRepos() error {
	s.res.Config = &config.Config{Files: map[string][]config.FileMap{}}
	used := map[string]bool{}

	for _, a := range s.apps {
		if spec, ok := a.Install[s.p.Key()]; ok && spec.Repo != "" {
			used[spec.Repo] = true
		}
	}

	if len(used) == 0 || s.p.Key() != "debian" {
		return nil
	}

	repos, copies, err := apt.CaptureRepos(used)

	if err != nil {
		return err
	}

	s.res.Config.AptRepos = repos

	for _, a := range s.apps {
		if spec, ok := a.Install[s.p.Key()]; ok && spec.Repo != "" && repos[spec.Repo].Source == "" {
			spec.Repo = ""
			a.Install[s.p.Key()] = spec
		}
	}

	for _, dst := range slices.Sorted(maps.Keys(copies)) {
		c, err := files.CopyChange(copies[dst], dst, s.root)

		if err != nil {
			s.warn("copy %s: %v", copies[dst], err)

			continue
		}

		if c != nil {
			s.res.FileChanges = append(s.res.FileChanges, *c)
		}
	}

	return nil
}

func (s *scanner) captureFiles() error {
	for _, id := range slices.Sorted(maps.Keys(s.apps)) {
		app := s.apps[id]

		if app.Files == "" {
			continue
		}

		entry, _ := s.cat.Get(app.Files)
		changes, fileMaps, err := files.Capture(entry, s.p.FileKey(), s.root)

		if err != nil {
			s.warn("%s settings: %v", id, err)

			continue
		}

		if len(fileMaps) == 0 {
			app.Files = ""

			continue
		}

		s.res.Config.Files[app.Files] = fileMaps
		s.addChanges(changes)
	}

	if _, ok := s.apps["claude-code"]; ok {
		changes, err := ai.CaptureClaude(s.root)

		if err != nil {
			s.warn("claude MCP/plugins: %v", err)
		}

		s.addChanges(changes)
	}

	return nil
}

func (s *scanner) addChanges(cs []files.Change) {
	for _, c := range cs {
		s.res.FileChanges = append(s.res.FileChanges, c)
		s.res.Findings = append(s.res.Findings, c.Findings...)
	}
}

func (s *scanner) sortedApps() []config.App {
	out := make([]config.App, 0, len(s.apps))

	for _, a := range s.apps {
		out = append(out, *a)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}

		return out[i].ID < out[j].ID
	})

	return out
}

func (s *scanner) warn(format string, args ...any) {
	s.res.Warnings = append(s.res.Warnings, fmt.Sprintf(format, args...))
}

// sameAsAny avoids a platform spec that repeats the catalog's cross-platform one.
func sameAsAny(app *config.App, name, pkg string) bool {
	anySpec, ok := app.Install[config.AnyPlatform]

	if !ok {
		return false
	}

	n, p := anySpec.Manager()

	return n == name && p == pkg
}

func appID(d adapter.Detected) string {
	id := strings.ToLower(strings.TrimPrefix(d.Name, "@"))

	return strings.NewReplacer("/", "-", " ", "-", ":", "-").Replace(id)
}

func union(a, b []string) []string {
	out := slices.Clone(a)

	for _, v := range b {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}

	return out
}
