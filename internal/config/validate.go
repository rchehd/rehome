package config

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/secrets"
)

var platformKeys = []string{"debian", "macos", "fedora", "arch"}

// Validate returns every problem found; an empty slice means the config is usable.
func (c *Config) Validate() []error {
	var errs []error
	ids := map[string]bool{}

	for _, a := range c.Apps {
		if a.ID == "" {
			errs = append(errs, fmt.Errorf("app without id"))

			continue
		}

		if ids[a.ID] {
			errs = append(errs, fmt.Errorf("%s: duplicate id", a.ID))
		}

		ids[a.ID] = true
		errs = append(errs, validateSpecs(a)...)

		for _, key := range a.Skip {
			if !slices.Contains(platformKeys, key) {
				errs = append(errs, fmt.Errorf("%s: unknown platform %q in skip", a.ID, key))
			}
		}
	}

	for _, a := range c.Apps {
		for _, dep := range a.Requires {
			if !ids[dep] {
				errs = append(errs, fmt.Errorf("%s: requires unknown app %q", a.ID, dep))
			}
		}

		if a.Files != "" && c.Files[a.Files] == nil {
			errs = append(errs, fmt.Errorf("%s: unknown files group %q", a.ID, a.Files))
		}

		for key, s := range a.Install {
			if s.Repo != "" && c.AptRepos[s.Repo].Source == "" {
				errs = append(errs, fmt.Errorf("%s.%s: unknown apt repo %q", a.ID, key, s.Repo))
			}
		}
	}

	for id, byKey := range c.Capture {
		for key, rule := range byKey {
			if key != "linux" && key != "macos" {
				errs = append(errs, fmt.Errorf("capture %s: unknown file key %q (linux or macos)", id, key))
			}

			if rule.Base == "" || len(rule.Include) == 0 {
				errs = append(errs, fmt.Errorf("capture %s.%s: base and include are required", id, key))
			}
		}
	}

	for group, files := range c.Files {
		for _, file := range files {
			if file.Session != "" && (!slices.Contains([]string{"claude-code", "codex", "cursor"}, file.Session) || !strings.HasPrefix(path.Clean(file.Src), "files/") || len(file.Dest) > 0 || file.Dconf || file.Bitwarden != "" || file.Mode != "") {
				errs = append(errs, fmt.Errorf("%s: session requires a supported format and a snapshot under files/", group))
			}
			if file.Dconf && (!strings.HasPrefix(path.Clean(file.Src), "files/") || len(file.Dest) > 0 || file.Bitwarden != "" || file.Mode != "") {
				errs = append(errs, fmt.Errorf("%s: dconf requires a snapshot under files/ and cannot use dest, mode or bitwarden", group))
			}

			if file.Bitwarden != "" && !secrets.ValidBitwardenRef(file.Bitwarden) {
				errs = append(errs, fmt.Errorf("%s: %s needs a valid Bitwarden item UUID and selector", group, file.Src))
			}

			if secrets.IsBitwardenSSHKey(file.Bitwarden) {
				errs = append(errs, validateSSHKey(group, file)...)
			}
		}
	}

	if cycle := c.findCycle(); cycle != nil {
		errs = append(errs, fmt.Errorf("requires cycle: %s", strings.Join(cycle, " -> ")))
	}

	return errs
}

var sshKeyPath = regexp.MustCompile(`^~/\.ssh/[A-Za-z0-9][A-Za-z0-9._-]*$`)

func ValidSSHKeyDest(dest string) bool {
	if !sshKeyPath.MatchString(dest) {
		return false
	}

	name := strings.ToLower(strings.TrimPrefix(dest, "~/.ssh/"))

	return !slices.Contains([]string{"config", "known_hosts", "known_hosts.old", "authorized_keys", "authorized_keys2", "rc", "environment"}, name) &&
		!strings.HasSuffix(name, ".pub") && !strings.Contains(name, ".rehome.bak")
}

func validateSSHKey(group string, file FileMap) []error {
	var errs []error

	if file.Src != "" || len(file.Dest) == 0 {
		errs = append(errs, fmt.Errorf("%s: a Bitwarden SSH key needs destinations and has no src template", group))
	}

	for key, dest := range file.Dest {
		if (key != "linux" && key != "macos") || !ValidSSHKeyDest(dest) {
			errs = append(errs, fmt.Errorf("%s: invalid Bitwarden SSH key destination for %s, expected a key filename directly under ~/.ssh", group, key))
		}
	}

	return errs
}

func validateSpecs(a App) []error {
	var errs []error

	for key, s := range a.Install {
		n := 0

		for _, m := range s.managers() {
			if m.pkg != "" {
				n++
			}
		}

		if n != 1 {
			errs = append(errs, fmt.Errorf("%s.%s: expected exactly one manager, got %d", a.ID, key, n))
		}

		if s.DebURL != "" && s.Apt == "" {
			errs = append(errs, fmt.Errorf("%s.%s: deb_url needs the apt package name", a.ID, key))
		}

		errs = append(errs, unsafeFields(a.ID+"."+key, s)...)
	}

	return errs
}

var (
	// Package names reach shell commands; a config from a URL must not smuggle code into them.
	pkgName  = regexp.MustCompile(`^[A-Za-z0-9@][\w@.+/:=~-]*$`)
	pkgArgs  = regexp.MustCompile("^[A-Za-z0-9@.][^;&|$`<>(){}'\"\\\\\n]*$")
	masID    = regexp.MustCompile(`^[0-9]+$`)
	plainURL = regexp.MustCompile(`^https://[^\s'"\\` + "`" + `]+$`)
)

func unsafeFields(where string, s InstallSpec) []error {
	var errs []error
	check := func(field, value string, re *regexp.Regexp) {
		if value != "" && !re.MatchString(value) {
			errs = append(errs, fmt.Errorf("%s: unsafe %s %q", where, field, value))
		}
	}

	check("apt", s.Apt, pkgName)
	check("snap", s.Snap, pkgName)
	check("flatpak", s.Flatpak, pkgName)
	check("npm", s.Npm, pkgName)
	check("cargo", s.Cargo, pkgName)
	check("go", s.GoPkg, pkgName)
	check("channel", s.Channel, pkgName)
	check("remote", s.Remote, pkgName)
	check("brew", s.Brew, pkgName)
	check("cask", s.Cask, pkgName)
	check("tap", s.Tap, pkgName)
	check("mas", s.Mas, masID)
	check("pipx", s.Pipx, pkgArgs)
	check("uv_tool", s.UvTool, pkgArgs)
	check("deb_url", s.DebURL, plainURL)
	check("remote_url", s.RemoteURL, plainURL)

	return errs
}

func (c *Config) findCycle() []string {
	const (
		visiting = 1
		done     = 2
	)

	state := map[string]int{}
	var path []string
	var visit func(id string) []string

	visit = func(id string) []string {
		switch state[id] {
		case visiting:
			return append(append([]string{}, path...), id)
		case done:
			return nil
		}

		state[id] = visiting
		path = append(path, id)

		if a, ok := c.App(id); ok {
			for _, dep := range a.Requires {
				if cycle := visit(dep); cycle != nil {
					return cycle
				}
			}
		}

		path = path[:len(path)-1]
		state[id] = done

		return nil
	}

	for _, a := range c.Apps {
		if cycle := visit(a.ID); cycle != nil {
			return cycle
		}
	}

	return nil
}
