package config

const (
	FileName       = "rehome.yml"
	CurrentVersion = 1
	AnyPlatform    = "any"
	UnknownGroup   = "unknown"
)

type Config struct {
	Version  int                  `yaml:"version"`
	Apps     []App                `yaml:"apps"`
	AptRepos map[string]AptRepo   `yaml:"apt_repos,omitempty"`
	Files    map[string][]FileMap `yaml:"files,omitempty"`
}

type App struct {
	ID    string `yaml:"id"`
	Group string `yaml:"group,omitempty"`
	// Enabled is nil when the user never set it, which means enabled.
	Enabled       *bool                  `yaml:"enabled,omitempty"`
	Install       map[string]InstallSpec `yaml:"install,omitempty"`
	Files         string                 `yaml:"files,omitempty"`
	Requires      []string               `yaml:"requires,omitempty"`
	PostInstall   []string               `yaml:"post_install,omitempty"`
	Detected      map[string]string      `yaml:"detected,omitempty"`
	MissingOnScan bool                   `yaml:"missing_on_scan,omitempty"`
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

	Repo      string `yaml:"repo,omitempty"`
	DebURL    string `yaml:"deb_url,omitempty"`
	Classic   bool   `yaml:"classic,omitempty"`
	Channel   string `yaml:"channel,omitempty"`
	Remote    string `yaml:"remote,omitempty"`
	RemoteURL string `yaml:"remote_url,omitempty"`
	// Bin is a binary name or path glob proving a script install is present.
	Bin string `yaml:"bin,omitempty"`
}

type AptRepo struct {
	Source  string `yaml:"source"`
	Keyring string `yaml:"keyring,omitempty"`
}

type FileMap struct {
	Src  string            `yaml:"src"`
	Dest map[string]string `yaml:"dest"`
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
	}
}

func (c *Config) App(id string) (*App, bool) {
	for i := range c.Apps {
		if c.Apps[i].ID == id {
			return &c.Apps[i], true
		}
	}

	return nil, false
}
