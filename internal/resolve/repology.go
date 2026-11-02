package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	cacheTTL    = 7 * 24 * time.Hour
	minInterval = time.Second
	userAgent   = "rehome (https://github.com/rchehd/rehome)"
)

// Repology looks up a project on repology.org and returns its package names in the target
// platform's repositories. Responses are cached on disk; requests stay at most one a second.
type Repology struct {
	BaseURL  string
	CacheDir string
	Client   *http.Client

	mu   sync.Mutex
	last time.Time
}

func NewRepology() *Repology {
	dir, err := os.UserCacheDir()

	if err != nil {
		dir = os.TempDir()
	}

	return &Repology{
		BaseURL:  "https://repology.org/api/v1/project/",
		CacheDir: filepath.Join(dir, "rehome", "repology"),
		Client:   &http.Client{Timeout: 15 * time.Second},
	}
}

type pkg struct {
	Repo     string   `json:"repo"`
	SrcName  string   `json:"srcname"`
	BinName  string   `json:"binname"`
	Visible  string   `json:"visiblename"`
	BinNames []string `json:"binnames"`
}

func (r *Repology) Names(ctx context.Context, name, platformKey string) []string {
	pkgs, err := r.project(ctx, strings.ToLower(name))

	if err != nil {
		return nil
	}

	var out []string

	for _, p := range pkgs {
		if !inPlatform(p.Repo, platformKey) {
			continue
		}

		for _, n := range []string{p.BinName, p.SrcName, p.Visible} {
			if n != "" && !contains(out, n) {
				out = append(out, n)
			}
		}
	}

	return out
}

func inPlatform(repo, platformKey string) bool {
	switch platformKey {
	case "macos":
		return repo == "homebrew" || repo == "homebrew_casks"
	case "debian":
		return strings.HasPrefix(repo, "ubuntu_") || strings.HasPrefix(repo, "debian_")
	case "fedora":
		return strings.HasPrefix(repo, "fedora_")
	case "arch":
		return repo == "arch"
	}

	return false
}

func (r *Repology) project(ctx context.Context, name string) ([]pkg, error) {
	cache := filepath.Join(r.CacheDir, url.PathEscape(name)+".json")

	if info, err := os.Stat(cache); err == nil && time.Since(info.ModTime()) < cacheTTL {
		if data, err := os.ReadFile(cache); err == nil {
			return decode(data)
		}
	}

	data, err := r.fetch(ctx, name)

	if err != nil {
		return nil, err
	}

	if os.MkdirAll(r.CacheDir, 0o755) == nil {
		_ = os.WriteFile(cache, data, 0o644)
	}

	return decode(data)
}

func (r *Repology) fetch(ctx context.Context, name string) ([]byte, error) {
	r.mu.Lock()

	if wait := minInterval - time.Since(r.last); wait > 0 {
		time.Sleep(wait)
	}

	r.last = time.Now()
	r.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.BaseURL+url.PathEscape(name), nil)

	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)
	resp, err := r.Client.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("repology %s: %s", name, resp.Status)
	}

	var raw json.RawMessage

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	return raw, nil
}

func decode(data []byte) ([]pkg, error) {
	var pkgs []pkg

	return pkgs, json.Unmarshal(data, &pkgs)
}
