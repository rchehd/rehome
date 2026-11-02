package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Settings is the machine-local state in ~/.config/rehome/settings.yml.
type Settings struct {
	RepoURL   string `yaml:"repo_url,omitempty"`
	ClonePath string `yaml:"clone_path,omitempty"`
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
