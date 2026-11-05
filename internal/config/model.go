package config

import (
	"os"
	"slices"
	"sync"
)

const (
	FileName       = "rehome.yml"
	CurrentVersion = 2
	AnyPlatform    = "any"
	UnknownGroup   = "unknown"
)

type Config struct {
	Version  int                `yaml:"version"`
	Apps     []App              `yaml:"apps"`
	AptRepos map[string]AptRepo `yaml:"apt_repos,omitempty"`
	// AptKeys are legacy keys for /etc/apt/trusted.gpg.d, used by repos without signed-by.
	AptKeys []string `yaml:"apt_keys,omitempty"`
	// Files is never stored in rehome.yml: it is filled from the nodes chosen for each group.
	Files map[string][]FileMap `yaml:"files,omitempty"`
	// Ignore lists app ids a given node (by name) never installs.
	Ignore map[string][]string `yaml:"ignore,omitempty"`
	// Capture holds settings rules per app id and file key (linux, macos) beyond the catalog.
	Capture map[string]map[string]FileRule `yaml:"capture,omitempty"`
}

type App struct {
	ID    string `yaml:"id"`
	Group string `yaml:"group,omitempty"`
	// Enabled is nil when the user never set it, which means enabled.
	Enabled     *bool                  `yaml:"enabled,omitempty"`
	Install     map[string]InstallSpec `yaml:"install,omitempty"`
	Files       string                 `yaml:"files,omitempty"`
	Requires    []string               `yaml:"requires,omitempty"`
	PostInstall []string               `yaml:"post_install,omitempty"`
	Detected    map[string]string      `yaml:"detected,omitempty"`
	// Skip lists platform keys (debian, macos, …) where the app is not wanted.
	Skip []string `yaml:"skip,omitempty"`
	// Version pins the app permanently, whatever the install mode.
	Version       string `yaml:"version,omitempty"`
	MissingOnScan bool   `yaml:"missing_on_scan,omitempty"`
}

// InstallSpec names exactly one manager field; the rest are modifiers of it.
type InstallSpec struct {
	Apt     string `yaml:"apt,omitempty"`
	Snap    string `yaml:"snap,omitempty"`
	Flatpak string `yaml:"flatpak,omitempty"`
	Npm     string `yaml:"npm,omitempty"`
	Pipx    string `yaml:"pipx,omitempty"`
	UvTool  string `yaml:"uv_tool,omitempty"`
	Cargo   string `yaml:"cargo,omitempty"`
	GoPkg   string `yaml:"go,omitempty"`
	Script  string `yaml:"script,omitempty"`
	Brew    string `yaml:"brew,omitempty"`
	Cask    string `yaml:"cask,omitempty"`
	Mas     string `yaml:"mas,omitempty"`

	Repo string `yaml:"repo,omitempty"`
	// Tap is a third-party Homebrew tap (user/repo) the formula or cask comes from.
	Tap       string `yaml:"tap,omitempty"`
	DebURL    string `yaml:"deb_url,omitempty"`
	Classic   bool   `yaml:"classic,omitempty"`
	Channel   string `yaml:"channel,omitempty"`
	Remote    string `yaml:"remote,omitempty"`
	RemoteURL string `yaml:"remote_url,omitempty"`
	// Bin is a binary name or path glob proving a script install is present.
	Bin string `yaml:"bin,omitempty"`
}

type AptRepo struct {
	Source   string   `yaml:"source"`
	Keyring  string   `yaml:"keyring,omitempty"`
	Keyrings []string `yaml:"keyrings,omitempty"`
	// Release is the codename the source was captured on; only it is swapped for the target's.
	Release string `yaml:"release,omitempty"`
}

func (r AptRepo) AllKeyrings() []string {
	if r.Keyring == "" {
		return r.Keyrings
	}

	return append([]string{r.Keyring}, r.Keyrings...)
}

type FileMap struct {
	Session   string            `yaml:"session,omitempty"`
	Src       string            `yaml:"src,omitempty"`
	Dest      map[string]string `yaml:"dest,omitempty"`
	Bitwarden string            `yaml:"bitwarden,omitempty"`
	Dconf     bool              `yaml:"dconf,omitempty"`
	// Mode keeps private permissions (e.g. "0600"), which git does not carry.
	Mode string `yaml:"mode,omitempty"`
}

func (a App) IsEnabled() bool {
	return a.Enabled == nil || *a.Enabled
}

// Spec returns the install spec for the platform key, falling back to "any".
func (a App) Spec(platformKey string) (InstallSpec, bool) {
	if s, ok := a.Install[platformKey]; ok {
		return s, true
	}

	s, ok := a.Install[AnyPlatform]

	return s, ok
}

// Manager returns the adapter name and package of the spec, or empty strings.
func (s InstallSpec) Manager() (name, pkg string) {
	for _, m := range s.managers() {
		if m.pkg != "" {
			return m.name, m.pkg
		}
	}

	return "", ""
}

type managerField struct{ name, pkg string }

func (s InstallSpec) managers() []managerField {
	return []managerField{
		{"apt", s.Apt},
		{"snap", s.Snap},
		{"flatpak", s.Flatpak},
		{"npm", s.Npm},
		{"pipx", s.Pipx},
		{"uvtool", s.UvTool},
		{"cargo", s.Cargo},
		{"go", s.GoPkg},
		{"script", s.Script},
		{"brew", s.Brew},
		{"cask", s.Cask},
		{"mas", s.Mas},
	}
}

// Ignored reports whether the app is skipped on this platform or ignored on this node.
func (c *Config) Ignored(a App, platformKey, node string) bool {
	return slices.Contains(a.Skip, platformKey) || slices.Contains(c.Ignore[node], a.ID)
}

func Hostname() string {
	h, _ := os.Hostname()

	return h
}

// ThisNode is this machine's node name, the key of its `ignore` list.
var ThisNode = sync.OnceValue(func() string {
	if s, err := LoadSettings(); err == nil && s.NodeName != "" {
		return s.NodeName
	}

	return NodeName(Hostname())
})

func (c *Config) App(id string) (*App, bool) {
	for i := range c.Apps {
		if c.Apps[i].ID == id {
			return &c.Apps[i], true
		}
	}

	return nil, false
}
