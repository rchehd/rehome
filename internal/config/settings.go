package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Settings is the machine-local state in ~/.config/rehome/settings.yml.
type Settings struct {
	RepoURL   string `yaml:"repo_url,omitempty"`
	ClonePath string `yaml:"clone_path,omitempty"`
	// NodeID never changes; NodeName names this machine's folder under nodes/.
	NodeID   string `yaml:"node_id,omitempty"`
	NodeName string `yaml:"node_name,omitempty"`
	// StaleAfterDays hides nodes that have not pushed for this long from install --all; 0 means 90.
	StaleAfterDays int `yaml:"stale_after_days,omitempty"`
}

// StaleAfter is how long a node may stay silent before it counts as stale.
func (s Settings) StaleAfter() time.Duration {
	days := s.StaleAfterDays

	if days == 0 {
		days = 90
	}

	return time.Duration(days) * 24 * time.Hour
}

// EnsureNode gives this machine a node id and a name from its hostname; it reports a change.
func (s *Settings) EnsureNode() (bool, error) {
	changed := false

	if s.NodeID == "" {
		b := make([]byte, 16)

		if _, err := rand.Read(b); err != nil {
			return false, err
		}

		s.NodeID = hex.EncodeToString(b)
		changed = true
	}

	if s.NodeName == "" {
		s.NodeName = NodeName(Hostname())
		changed = true
	}

	return changed, nil
}

// NodeName turns a hostname into a folder-safe node name.
func NodeName(host string) string {
	name := strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}

		return '-'
	}, host), "-.")

	if name == "" {
		return "machine"
	}

	return name
}

func (s Settings) Linked() bool {
	return s.RepoURL != "" && s.ClonePath != ""
}

func LoadSettings() (Settings, error) {
	var s Settings
	path, err := settingsPath()

	if err != nil {
		return s, err
	}

	data, err := os.ReadFile(path)

	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}

	if err != nil {
		return s, err
	}

	return s, yaml.Unmarshal(data, &s)
}

func (s Settings) Save() error {
	path, err := settingsPath()

	if err != nil {
		return err
	}

	data, err := yaml.Marshal(s)

	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

// DefaultClonePath is where a linked config repo is cloned.
func DefaultClonePath() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "rehome", "config"), nil
	}

	home, err := os.UserHomeDir()

	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".local", "share", "rehome", "config"), nil
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()

	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "rehome", "settings.yml"), nil
}
