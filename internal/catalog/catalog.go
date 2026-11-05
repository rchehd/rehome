package catalog

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
)

//go:embed data/*.yaml
var data embed.FS

type Entry struct {
	SessionFormat string                        `yaml:"session_format"`
	HarnessOnly   bool                          `yaml:"harness_only"`
	ID            string                        `yaml:"id"`
	Group         string                        `yaml:"group"`
	Desktop       bool                          `yaml:"desktop"`
	Install       map[string]config.InstallSpec `yaml:"install"`
	Detect        Detect                        `yaml:"detect"`
	Files         map[string]FileRule           `yaml:"files"`
	Requires      []string                      `yaml:"requires"`
	// Skip lists platforms the app does not exist on (no installer to map to).
	Skip        []string `yaml:"skip"`
	PostInstall []string `yaml:"post_install"`
}

// Detect says how to recognise the software; any matching field is enough.
type Detect struct {
	// Packages are names under apt, snap, flatpak, npm, pipx, uv or go.
	Packages []string `yaml:"packages"`
	Bins     []string `yaml:"bins"`
	// Path is a glob (with ~) whose existence proves a script install.
	Path string `yaml:"path"`
	// Paths are more such globs, e.g. the same thing's location on another OS.
	Paths []string `yaml:"paths"`
}

// FileRule is the shared settings-file rule (see config.FileRule).
type FileRule = config.FileRule

type Catalog struct {
	Entries []Entry
}

func Load() (*Catalog, error) {
	files, err := fs.Glob(data, "data/*.yaml")

	if err != nil {
		return nil, err
	}

	c := &Catalog{}
	ids := map[string]bool{}

	for _, f := range files {
		raw, err := data.ReadFile(f)

		if err != nil {
			return nil, err
		}

		var entries []Entry

		if err := yaml.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}

		for _, e := range entries {
			if ids[e.ID] {
				return nil, fmt.Errorf("%s: duplicate catalog id %q", f, e.ID)
			}

			ids[e.ID] = true
		}

		c.Entries = append(c.Entries, entries...)
	}

	return c, nil
}

func (c *Catalog) Get(id string) (*Entry, bool) {
	for i := range c.Entries {
		if c.Entries[i].ID == id {
			return &c.Entries[i], true
		}
	}

	return nil, false
}

func (c *Catalog) WSLReason(p platform.Platform, app config.App, spec config.InstallSpec) string {
	if !p.WSL {
		return ""
	}

	if app.ID == "gnome-settings" {
		return "GNOME desktop settings are unavailable in WSL"
	}

	desktop := desktopGroup(app.Group)

	if c != nil {
		entry, found := c.Get(app.ID)

		if !found {
			_, pkg := spec.Manager()
			entry, found = c.Match(adapter.Detected{Name: pkg})
		}

		if found {
			desktop = desktop || entry.Desktop || desktopGroup(entry.Group)
		}
	}

	apt, _, _ := strings.Cut(spec.Apt, ":")

	if desktop || strings.HasPrefix(apt, "fonts-") || strings.HasPrefix(apt, "ttf-") {
		return "install this on Windows"
	}

	if !p.Systemd && (spec.Snap != "" || spec.Flatpak != "" || apt == "snapd" || apt == "flatpak") {
		return "Snap and Flatpak are unavailable in WSL without systemd"
	}

	return ""
}

func desktopGroup(group string) bool {
	return group == "desktop" || group == "browsers" || group == "messengers"
}

// Match finds the entry describing detected software.
func (c *Catalog) Match(d adapter.Detected) (*Entry, bool) {
	if e, ok := c.Get(d.Name); ok {
		return e, true
	}

	for i := range c.Entries {
		e := &c.Entries[i]

		for _, p := range e.Detect.Packages {
			if p == d.Name {
				return e, true
			}
		}

		if bin := d.Meta["bin"]; bin != "" {
			for _, b := range e.Detect.Bins {
				if b == bin {
					return e, true
				}
			}
		}
	}

	return nil, false
}

// Present returns script-installed entries whose detect path exists on this machine.
func (c *Catalog) Present(platformKey string) []Entry {
	var found []Entry

	for _, e := range c.Entries {
		if e.Detect.Path == "" && len(e.Detect.Paths) == 0 {
			continue
		}

		// Entries without install specs carry settings only (fonts).
		if _, ok := e.spec(platformKey); !ok && len(e.Install) > 0 {
			continue
		}

		for _, path := range append([]string{e.Detect.Path}, e.Detect.Paths...) {
			if matches, _ := filepath.Glob(adapter.Home(path)); path != "" && len(matches) > 0 {
				found = append(found, e)

				break
			}
		}
	}

	return found
}

// Rule returns the settings rule for a file key ("linux" or "macos").
func (e *Entry) Rule(fileKey string) (FileRule, bool) {
	r, ok := e.Files[fileKey]

	return r, ok && r.Base != ""
}

func (e *Entry) spec(platformKey string) (config.InstallSpec, bool) {
	return config.App{Install: e.Install}.Spec(platformKey)
}

func globRegexp(pattern string) *regexp.Regexp {
	return config.GlobRegexp(pattern)
}
