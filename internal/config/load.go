package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const fetchTimeout = 30 * time.Second

// Load reads a config from a file, a directory holding rehome.yml, or an http(s) URL.
// It also returns the config root that relative files/ paths resolve against.
func Load(source string) (*Config, string, error) {
	data, root, err := read(source)

	if err != nil {
		return nil, "", err
	}

	cfg, err := Parse(data)

	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", source, err)
	}

	return cfg, root, nil
}

func Parse(data []byte) (*Config, error) {
	var cfg Config

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Version == 0 {
		cfg.Version = CurrentVersion
	}

	if cfg.Version != CurrentVersion {
		return nil, fmt.Errorf("unsupported config version %d", cfg.Version)
	}

	return &cfg, nil
}

func IsURL(source string) bool {
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

func read(source string) ([]byte, string, error) {
	// Over plain http anyone on the network could swap the install scripts.
	if strings.HasPrefix(source, "http://") {
		return nil, "", fmt.Errorf("refusing insecure config URL %s: use https", source)
	}

	if IsURL(source) {
		return fetch(source)
	}

	path := source
	info, err := os.Stat(path)

	if err != nil {
		return nil, "", err
	}

	if info.IsDir() {
		path = filepath.Join(path, FileName)
	}

	data, err := os.ReadFile(path)

	if err != nil {
		return nil, "", err
	}

	root, err := filepath.Abs(filepath.Dir(path))

	return data, root, err
}

func fetch(url string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)

	if err != nil {
		return nil, "", err
	}

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return nil, "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch %s: %s", url, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, "", err
	}

	// A remote config carries no files/; the root only anchors relative paths.
	root, err := os.MkdirTemp("", "rehome-config-")

	return data, root, err
}
