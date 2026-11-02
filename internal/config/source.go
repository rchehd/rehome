package config

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrNoConfig = errors.New("no config: use --config or rehome link")

// Resolve picks the config source: --config, then the linked repo, then ./rehome.yml.
func Resolve(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}

	s, err := LoadSettings()

	if err != nil {
		return "", err
	}

	if s.Linked() {
		return s.ClonePath, nil
	}

	if _, err := os.Stat(FileName); err == nil {
		return FileName, nil
	}

	return "", ErrNoConfig
}

// ResolveRoot picks the directory a scan writes into; the config there may not exist yet.
func ResolveRoot(flag string) (string, error) {
	if IsURL(flag) {
		return "", errors.New("scan cannot write to a URL config")
	}

	if flag != "" {
		if info, err := os.Stat(flag); err == nil && info.IsDir() {
			return filepath.Abs(flag)
		}

		return filepath.Abs(filepath.Dir(flag))
	}

	s, err := LoadSettings()

	if err != nil {
		return "", err
	}

	if s.Linked() {
		return s.ClonePath, nil
	}

	return os.Getwd()
}
