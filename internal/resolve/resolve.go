// Package resolve finds how to install an app on a platform its config has no spec for.
package resolve

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
)

const (
	SourceCatalog  = "catalog"
	SourceSameName = "same name"
	SourceRename   = "known rename"
	SourceRepology = "repology"
	SourcePortable = "same package as on another OS"
)

// Index answers whether a package with this exact name exists on this machine's managers.
type Index interface {
	Lookup(ctx context.Context, name string, preferGUI bool) (config.InstallSpec, bool)
}

// Names gives candidate package names for this platform from another platform's name.
type Names interface {
	Names(ctx context.Context, name, platformKey string) []string
}

type Resolver struct {
	PlatformKey string
	Catalog     *catalog.Catalog
	Index       Index
	// Repology is nil when offline.
	Repology Names
}

// Renames between Debian/Ubuntu and Homebrew packages that do not share a name.
var renames = map[string]string{
	"fd-find": "fd", "golang": "go", "nodejs": "node", "python3": "python@3", "neovim-qt": "neovim-qt",
	"silversearcher-ag": "the_silver_searcher", "build-essential": "", "p7zip-full": "p7zip",
	"openssh-client": "openssh", "netcat-openbsd": "netcat", "imagemagick": "imagemagick",
}

// Resolve returns a spec for this platform and where it came from.
func (r *Resolver) Resolve(ctx context.Context, app config.App) (config.InstallSpec, string, bool) {
	if spec, ok := r.fromCatalog(app); ok {
		return spec, SourceCatalog, true
	}

	if spec, ok := portable(app); ok {
		return spec, SourcePortable, true
	}

	if r.skipped(app) {
		return config.InstallSpec{}, "", false
	}

	if r.Index == nil {
		return config.InstallSpec{}, "", false
	}

	gui := isGUI(app)

	for _, n := range candidates(app) {
		if spec, ok := r.Index.Lookup(ctx, n, gui); ok {
			return spec, SourceSameName, true
		}

		if renamed, ok := rename(n); ok && renamed != "" {
			if spec, ok := r.Index.Lookup(ctx, renamed, gui); ok {
				return spec, SourceRename, true
			}
		}
	}

	if r.Repology == nil {
		return config.InstallSpec{}, "", false
	}

	for _, n := range candidates(app) {
		for _, alt := range r.Repology.Names(ctx, n, r.PlatformKey) {
			if spec, ok := r.Index.Lookup(ctx, alt, gui); ok {
				return spec, SourceRepology, true
			}
		}
	}

	return config.InstallSpec{}, "", false
}

func (r *Resolver) fromCatalog(app config.App) (config.InstallSpec, bool) {
	if r.Catalog == nil {
		return config.InstallSpec{}, false
	}

	if e, ok := r.Catalog.Get(app.ID); ok {
		if spec, ok := (config.App{Install: e.Install}).Spec(r.PlatformKey); ok {
			return spec, true
		}
	}

	return config.InstallSpec{}, false
}

// portable reuses another platform's spec whose manager works the same everywhere (npm, pipx, uv, cargo, go).
func portable(app config.App) (config.InstallSpec, bool) {
	for _, key := range slices.Sorted(maps.Keys(app.Install)) {
		s := app.Install[key]

		if s.Npm != "" || s.Pipx != "" || s.UvTool != "" || s.Cargo != "" || s.GoPkg != "" {
			return config.InstallSpec{Npm: s.Npm, Pipx: s.Pipx, UvTool: s.UvTool, Cargo: s.Cargo, GoPkg: s.GoPkg, Bin: s.Bin}, true
		}
	}

	return config.InstallSpec{}, false
}

// skipped: the catalog says the app does not exist on this platform, so do not guess.
func (r *Resolver) skipped(app config.App) bool {
	if r.Catalog == nil {
		return false
	}

	e, ok := r.Catalog.Get(app.ID)

	return ok && contains(e.Skip, r.PlatformKey)
}

// candidates are the package names the app is known by on other platforms, then its id.
func candidates(app config.App) []string {
	var out []string
	add := func(n string) {
		n = strings.TrimSpace(n)

		if n != "" && !contains(out, n) {
			out = append(out, n)
		}
	}

	for _, s := range app.Install {
		add(s.Apt)
		add(s.Snap)
		add(s.Brew)
		add(s.Cask)

		if s.Flatpak != "" {
			add(strings.ToLower(s.Flatpak[strings.LastIndex(s.Flatpak, ".")+1:]))
		}
	}

	add(app.ID)

	return out
}

// isGUI: snap, flatpak and cask apps are desktop apps, so a cask is the better match.
func isGUI(app config.App) bool {
	for _, s := range app.Install {
		if s.Snap != "" || s.Flatpak != "" || s.Cask != "" {
			return true
		}
	}

	return false
}

func rename(n string) (string, bool) {
	if v, ok := renames[n]; ok {
		return v, true
	}

	for k, v := range renames {
		if v == n && v != "" {
			return k, true
		}
	}

	return "", false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}

	return false
}
