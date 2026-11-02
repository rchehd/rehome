package catalog

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
)

//go:embed data/*.yaml
var data embed.FS

type Entry struct {
	ID          string                        `yaml:"id"`
	Group       string                        `yaml:"group"`
	Install     map[string]config.InstallSpec `yaml:"install"`
	Detect      Detect                        `yaml:"detect"`
	Files       map[string]FileRule           `yaml:"files"`
	Requires    []string                      `yaml:"requires"`
	PostInstall []string                      `yaml:"post_install"`
}

// Detect says how to recognise the software; any matching field is enough.
type Detect struct {
	// Packages are names under apt, snap, flatpak, npm, pipx, uv or go.
	Packages []string `yaml:"packages"`
	Bins     []string `yaml:"bins"`
	// Path is a glob (with ~) whose existence proves a script install.
	Path string `yaml:"path"`
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
		if e.Detect.Path == "" {
			continue
		}

		// Entries without install specs carry settings only (fonts).
		if _, ok := e.spec(platformKey); !ok && len(e.Install) > 0 {
			continue
		}

		if matches, _ := filepath.Glob(adapter.Home(e.Detect.Path)); len(matches) > 0 {
			found = append(found, e)
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
