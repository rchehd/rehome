package scan

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
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
	"github.com/rchehd/rehome/internal/discover"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/progress"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
	"github.com/rchehd/rehome/internal/sessions"
)

type Result struct {
	Config      *config.Config
	FileChanges []files.Change
	Findings    []secrets.Finding
	// Unknown are binaries no recipe or catalog entry knows, with where they were found.
	Unknown     map[string]map[string]string
	Warnings    []string
	SkipMissing map[string]bool
	// Attention lists what scan could not take by itself, each with an action for the user.
	Attention []discover.Warning
}

type Options struct {
	HarnessData  bool
	Sessions     bool
	SessionPaths map[string]string
	// FilesRoot is the node folder settings files are compared with; empty means root.
	FilesRoot string
}

type scanner struct {
	ctx  context.Context
	r    *runner.Runner
	p    platform.Platform
	cat  *catalog.Catalog
	root string
	apps map[string]*config.App
	// unknown are binaries without a recipe: not apps, but their settings still go to this node.
	unknown map[string]*config.App
	res     *Result
	// hints are extra names (package, binaries) used to find an app's settings.
	hints map[string][]string
	// existing is the config already in root; its capture rules are honoured.
	existing           *config.Config
	options            Options
	secretTemplates    map[string]bool
	secretDestinations []os.FileInfo
}

// Run detects installed software and settings; nothing is written.
func Run(ctx context.Context, r *runner.Runner, p platform.Platform, cat *catalog.Catalog, root string, existing *config.Config, options Options) (*Result, error) {
	if err := sessions.ValidatePaths(options.SessionPaths); err != nil {
		return nil, err
	}
	s := &scanner{ctx: ctx, r: r, p: p, cat: cat, root: root, apps: map[string]*config.App{}, unknown: map[string]*config.App{}, res: &Result{}, hints: map[string][]string{}, existing: existing, options: options}
	s.protectSecretFiles()

	if p.WSL {
		s.res.SkipMissing = map[string]bool{}

		for _, app := range existing.Apps {
			spec, _ := app.Spec(p.Key())

			if cat.WSLReason(p, app, spec) != "" {
				s.res.SkipMissing[app.ID] = true
			}
		}
	}

	for _, a := range all.For(p) {
		progress.Step(ctx, "scanning %s", a.Name())
		found, err := a.Scan(ctx, r)

		if err != nil {
			s.warn("%s scan: %v", a.Name(), err)
		}

		s.addAll(found)
	}

	if p.Key() == "debian" && adapter.HasCmd("apt-mark") {
		if reason := apt.BaselineWarning(ctx, r); reason != "" {
			s.res.Attention = append(s.res.Attention, discover.Warning{
				Path: "/var/log/installer/initial-status.gz", Reason: reason,
				Action: "check the new apps before you push",
			})
		}
	}

	known := map[string]bool{}

	for _, e := range cat.Present(p.Key()) {
		if e.HarnessOnly && !options.HarnessData {
			continue
		}

		s.add(adapter.Detected{Manager: "script", Name: e.ID})

		for _, b := range e.Detect.Bins {
			known[b] = true
		}
	}
	if options.Sessions {
		for _, entry := range cat.Entries {
			if entry.SessionFormat != "" {
				if info, err := os.Stat(sessions.Root(entry.SessionFormat)); err == nil && info.IsDir() {
					s.add(adapter.Detected{Manager: "script", Name: entry.ID})
				}
			}
		}
	}

	s.addScriptRecipes()
	progress.Step(ctx, "looking for installed tools")
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

	s.dropUnknownRepos()

	progress.Step(ctx, "reading settings")

	if err := s.captureFiles(); err != nil {
		return nil, err
	}

	cfg := s.res.Config
	s.res.Config = &config.Config{Version: config.CurrentVersion, Apps: s.sortedApps(), AptRepos: cfg.AptRepos, AptKeys: cfg.AptKeys, Files: cfg.Files, Capture: cfg.Capture}

	return s.res, nil
}

func (s *scanner) filesRoot() string {
	if s.options.FilesRoot != "" {
		return s.options.FilesRoot
	}

	return s.root
}

func (s *scanner) addAll(found []adapter.Detected) {
	for _, d := range found {
		s.add(d)
	}
}

func (s *scanner) add(d adapter.Detected) {
	entry, ok := s.cat.Match(d)
	app := &config.App{ID: appID(d), Group: d.Manager, Requires: d.Requires}
	defer func() { s.hints[app.ID] = append(s.hints[app.ID], d.Name, d.Meta["bin"]) }()

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

	if recipe, has := s.recipeFor(d); has && !ok {
		app.ID, app.Group, app.Install = recipe.ID, recipe.Group, maps.Clone(recipe.Install)
	} else if d.Manager == "unknown" && !ok {
		id := "unknown-" + appID(d)

		app.ID, app.Group = id, config.UnknownGroup

		// Only a recipe the user wrote for it makes it an app; otherwise it is this node's note.
		if _, known := s.existing.App(id); !known {
			if s.res.Unknown == nil {
				s.res.Unknown = map[string]map[string]string{}
			}

			s.res.Unknown[id] = d.Meta
			s.unknown[id] = app

			return
		}
	}

	spec, _ := app.Spec(s.p.Key())

	if s.res.SkipMissing[app.ID] || s.cat.WSLReason(s.p, *app, spec) != "" {
		return
	}

	if prev, dup := s.apps[app.ID]; dup {
		mergeInto(prev, app, s.p.Key())

		return
	}

	s.apps[app.ID] = app
}

// addScriptRecipes records the user's own script recipes whose binary is here; no package manager reports them.
func (s *scanner) addScriptRecipes() {
	for _, app := range s.existing.Apps {
		spec, ok := app.Spec(s.p.Key())

		if !ok || spec.Script == "" || spec.Bin == "" || s.res.SkipMissing[app.ID] {
			continue
		}

		if _, known := s.cat.Get(app.ID); known {
			continue
		}

		if _, have := s.apps[app.ID]; have || !adapter.BinExists(spec.Bin) {
			continue
		}

		s.apps[app.ID] = &config.App{ID: app.ID, Group: app.Group, Install: maps.Clone(app.Install), Requires: app.Requires, PostInstall: app.PostInstall, Files: app.Files}
	}
}

// recipeFor finds the user's recipe for a package installed without a repo, e.g. a .deb or a dropped source.
func (s *scanner) recipeFor(d adapter.Detected) (*config.App, bool) {
	pkg := d.Meta["package"]

	if d.Manager != "unknown" || pkg == "" {
		return nil, false
	}

	for i := range s.existing.Apps {
		if spec, ok := s.existing.Apps[i].Spec(s.p.Key()); ok && spec.Apt == pkg {
			return &s.existing.Apps[i], true
		}
	}

	return nil, false
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

	capture, err := apt.CaptureRepos(used, s.p.Codename)

	if err != nil {
		return err
	}

	s.res.Config.AptRepos = capture.Repos
	s.res.Config.AptKeys = capture.Keys
	s.res.Warnings = append(s.res.Warnings, capture.Warnings...)

	for _, a := range s.apps {
		if spec, ok := a.Install[s.p.Key()]; ok && spec.Repo != "" && capture.Repos[spec.Repo].Source == "" {
			spec.Repo = ""
			a.Install[s.p.Key()] = spec
		}
	}

	for _, dst := range slices.Sorted(maps.Keys(capture.Copies)) {
		src := capture.Copies[dst]
		c, err := files.CopyChange(src, dst, s.root)

		if err != nil {
			s.warn("copy %s: %v", src, err)

			continue
		}

		if c != nil {
			c.Shared = true
			s.addChanges([]files.Change{*c})
		}
	}

	return nil
}

// dropUnknownRepos removes another platform's catalog spec whose apt repo this scan has no definition for.
func (s *scanner) dropUnknownRepos() {
	for _, a := range s.apps {
		for key, spec := range a.Install {
			if key != s.p.Key() && spec.Repo != "" && s.res.Config.AptRepos[spec.Repo].Source == "" {
				delete(a.Install, key)
			}
		}
	}
}

func (s *scanner) captureFiles() error {
	for _, id := range slices.Sorted(maps.Keys(s.apps)) {
		app := s.apps[id]

		if app.Files == "" {
			continue
		}

		progress.Step(s.ctx, "reading settings: %s", id)
		entry, _ := s.cat.Get(app.Files)
		if entry.SessionFormat != "" {
			changes, mappings, warnings := files.CaptureSessions(s.ctx, entry, s.p.FileKey(), s.filesRoot(), s.options.SessionPaths)
			for _, err := range warnings {
				s.warn("%s: %v", id, err)
			}
			if len(mappings) == 0 {
				delete(s.apps, id)
				continue
			}
			s.res.Config.Files[app.Files] = mappings
			s.addChanges(changes)
			continue
		}
		changes, fileMaps, err := files.CaptureRules(entry.ID, entry.Files, s.p.FileKey(), s.filesRoot(), s.options.HarnessData)

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

	progress.Step(s.ctx, "reading settings: custom rules")
	s.captureByRules()
	progress.Step(s.ctx, "reading settings: GNOME")
	s.captureDconf()
	progress.Step(s.ctx, "looking for other settings")
	s.res.Attention = append(s.res.Attention, discover.DesktopSessions()...)
	s.res.Attention = append(s.res.Attention, unclaimedConfigDirs(s.res.Config, s.cat, s.p.FileKey())...)

	if _, ok := s.apps["claude-code"]; ok {
		progress.Step(s.ctx, "reading settings: Claude MCP and plugins")
		changes, err := ai.CaptureClaude(s.filesRoot())

		if err != nil {
			s.warn("claude MCP/plugins: %v", err)
		}

		s.addChanges(changes)
	}

	return nil
}

func (s *scanner) captureDconf() {
	if s.p.OS != "linux" || s.p.WSL || !files.GNOMEAvailable() {
		return
	}

	const id = "gnome-settings"
	change, found, err := files.CaptureDconf(s.ctx, s.r, s.filesRoot())

	if err != nil {
		s.warn("GNOME settings: %v", err)

		return
	}

	if !found {
		return
	}

	s.apps[id] = &config.App{ID: id, Group: "desktop", Files: id, Skip: []string{"macos"}}
	s.res.Config.Files[id] = []config.FileMap{{Src: files.DconfSnapshot, Dconf: true}}

	if change != nil {
		s.addChanges([]files.Change{*change})
	}
}

func (s *scanner) addChanges(cs []files.Change) {
	for _, c := range cs {
		if s.secretTemplates[filepath.Clean(c.Dst)] || s.isSecretDestination(c.Src) {
			continue
		}

		s.res.FileChanges = append(s.res.FileChanges, c)
		s.res.Findings = append(s.res.Findings, c.Findings...)
	}
}

func (s *scanner) protectSecretFiles() {
	s.secretTemplates = map[string]bool{}

	for _, group := range s.existing.Files {
		for _, f := range group {
			if f.Bitwarden == "" {
				continue
			}

			if f.Src != "" {
				s.secretTemplates[filepath.Clean(f.Src)] = true
			}

			if dest := f.Dest[s.p.FileKey()]; dest != "" {
				if info, err := os.Stat(adapter.Home(dest)); err == nil {
					s.secretDestinations = append(s.secretDestinations, info)
				}
			}
		}
	}
}

func (s *scanner) isSecretDestination(path string) bool {
	if len(s.secretDestinations) == 0 {
		return false
	}

	info, err := os.Stat(adapter.Home(path))

	return err == nil && slices.ContainsFunc(s.secretDestinations, func(dest os.FileInfo) bool {
		return os.SameFile(info, dest)
	})
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
