package lock

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	FileName       = "rehome.lock"
	CurrentVersion = 1
)

type Entry struct {
	Manager     string `yaml:"manager,omitempty"`
	Version     string `yaml:"version,omitempty"`
	Commit      string `yaml:"commit,omitempty"`
	Unsupported bool   `yaml:"unsupported,omitempty"`
}

type Lock struct {
	Version int `yaml:"version"`
	// Apps maps an app id to its entry per platform key (debian, macos, …).
	Apps map[string]map[string]Entry `yaml:"apps"`
}

func New() *Lock {
	return &Lock{Version: CurrentVersion, Apps: map[string]map[string]Entry{}}
}

// Load reads a lock file; a missing file is an empty lock, not an error.
func Load(path string) (*Lock, error) {
	data, err := os.ReadFile(path)

	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}

	if err != nil {
		return nil, err
	}

	l := New()

	if err := yaml.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if l.Apps == nil {
		l.Apps = map[string]map[string]Entry{}
	}

	if l.Version != CurrentVersion {
		return nil, fmt.Errorf("%s: unsupported lock version %d", path, l.Version)
	}

	return l, nil
}

// Save writes the lock; yaml.v3 sorts map keys, so diffs stay stable.
func (l *Lock) Save(path string) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(l); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func (l *Lock) Get(id, platform string) (Entry, bool) {
	e, ok := l.Apps[id][platform]

	return e, ok
}

func (l *Lock) Set(id, platform string, e Entry) {
	if l.Apps[id] == nil {
		l.Apps[id] = map[string]Entry{}
	}

	l.Apps[id][platform] = e
}

func (l *Lock) Clone() *Lock {
	c := New()

	for id, byPlatform := range l.Apps {
		for p, e := range byPlatform {
			c.Set(id, p, e)
		}
	}

	return c
}

// Path picks the lock file: the --lock flag, else rehome.lock next to the config.
func Path(root, flag string) string {
	if flag != "" {
		return flag
	}

	return filepath.Join(root, FileName)
}

// LocalPath is this machine's record of the versions at the last sync.
func LocalPath() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "rehome", "lock.yml"), nil
	}

	home, err := os.UserHomeDir()

	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".local", "state", "rehome", "lock.yml"), nil
}
