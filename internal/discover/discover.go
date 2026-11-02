package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
)

const maxAppBytes = 10 << 20

// Warning is something scan could not take by itself, with what the user should do.
type Warning struct {
	ID, Path, Reason, Action string
}

// Excludes keeps caches, logs, state, databases and VCS data out of auto rules.
var Excludes = []string{
	"**/cache/**", "**/Cache/**", "**/caches/**", "**/CachedData/**", "**/Code Cache/**", "**/GPUCache/**",
	"**/DawnCache/**", "**/ShaderCache/**", "**/logs/**", "**/log/**", "**/Crash Reports/**", "**/crashes/**",
	"**/IndexedDB/**", "**/Local Storage/**", "**/Session Storage/**", "**/Service Worker/**", "**/blob_storage/**",
	"**/sessions/**", "**/history/**", "**/state/**", "**/tmp/**", "**/node_modules/**", "**/.git/**",
	"**/*.log", "**/*.db", "**/*.db-*", "**/*.sqlite", "**/*.sqlite-*", "**/*.lock", "**/*.pid", "**/*.sock",
	"**/Cookies*", "**/*history*",
	// Package trees and indexes, not settings.
	"**/vendor/**", "**/packages/**", "**/index/**", "**/*.cache", "**/storage/**",
	// Electron runtime state.
	"**/Partitions/**", "**/WebStorage/**", "**/Shared Dictionary/**", "**/databases/**", "**/Dictionaries/**",
	"**/Network/**", "**/Local State", "**/Network Persistent State", "**/TransportSecurity", "**/Trust Tokens*",
	"**/*.ldb", "**/Singleton*", "**/DIPS*", "**/Preferences~",
}

// Dot dirs that hold data, toolchains or credentials, never settings worth copying wholesale.
var noisyDotDirs = map[string]bool{
	"cache": true, "local": true, "npm": true, "cargo": true, "rustup": true, "nvm": true, "mozilla": true,
	"docker": true, "ssh": true, "gnupg": true, "pki": true, "java": true, "gradle": true, "m2": true,
	"vscode-server": true, "var": true, "config": true, "steam": true, "wine": true, "android": true,
}

var configExts = []string{"toml", "yaml", "yml", "json", "conf", "ini"}

// ForApp looks for an app's settings in the usual places under names, skipping paths other
// rules already claim. It returns an auto rule, a warning when something exists but cannot be
// taken safely, or neither.
func ForApp(id string, names, claimed []string) (*config.FileRule, *Warning) {
	var include []string

	for _, n := range names {
		for _, c := range candidates(n) {
			if !Covered(adapter.Home("~/"+files.StaticPrefix(c)), claimed) {
				include = append(include, c)
			}
		}
	}

	include = unique(include)

	if len(include) == 0 {
		return nil, nil
	}

	rule := config.FileRule{Base: "~", Include: include, Exclude: Excludes, Origin: config.OriginAuto, TextOnly: true}
	size, count := measure(rule)

	switch {
	case count == 0:
		return nil, nil
	case size > maxAppBytes:
		return nil, &Warning{
			ID:     id,
			Path:   "~/" + strings.TrimSuffix(include[0], "/**"),
			Reason: fmt.Sprintf("%d MB of files, too large to take whole", size>>20),
			Action: "add a capture rule for " + id + " with just its settings files",
		}
	}

	return &rule, nil
}

// candidates are home-relative include patterns for one name that exist on disk.
func candidates(name string) []string {
	var out []string
	add := func(rel, pattern string) {
		if _, err := os.Stat(adapter.Home("~/" + rel)); err == nil {
			out = append(out, pattern)
		}
	}

	// ~/.config names are often capitalised (GitKraken, Termius); match them case-insensitively.
	if dir := configDir(name); dir != "" {
		out = append(out, ".config/"+dir+"/**")
	}

	for _, ext := range configExts {
		add(".config/"+name+"."+ext, ".config/"+name+"."+ext)
	}

	if !noisyDotDirs[name] && isDir("~/."+name) {
		add("."+name, "."+name+"/**")
	}

	if !isDir("~/." + name) {
		add("."+name, "."+name)
	}

	add("."+name+"rc", "."+name+"rc")

	// Sandboxed apps keep settings in their own trees.
	if strings.Contains(name, ".") && isDir("~/.var/app/"+name+"/config") {
		out = append(out, ".var/app/"+name+"/config/**")
	}

	if isDir("~/snap/" + name + "/current/.config") {
		out = append(out, "snap/"+name+"/current/.config/**")
	}

	return out
}

// HasSettings reports whether dir holds files an auto rule would take (not only caches).
func HasSettings(dir string) bool {
	_, count := measure(config.FileRule{Base: dir, Include: []string{"**"}, Exclude: Excludes})

	return count > 0
}

// measure sums the text files a rule would take (the same selection Capture uses).
func measure(rule config.FileRule) (int64, int) {
	base := adapter.Home(rule.Base)
	rels, err := files.Select(base, rule)

	if err != nil {
		return 0, 0
	}

	var size int64
	count := 0

	for _, rel := range rels {
		info, err := os.Stat(filepath.Join(base, rel))

		if err != nil || info.Size() > 1<<20 {
			continue
		}

		size += info.Size()
		count++
	}

	return size, count
}

// Covered reports whether path is inside, or contains, one of the claimed paths.
func Covered(path string, claimed []string) bool {
	for _, c := range claimed {
		if c == path || strings.HasPrefix(c, path+"/") || strings.HasPrefix(path, c+"/") {
			return true
		}
	}

	return false
}

// Claimed turns rules into the absolute paths they take; a rule rooted at ~ claims only its
// include prefixes, never the whole home directory.
func Claimed(rules []config.FileRule) []string {
	home := adapter.Home("~")
	var out []string

	for _, r := range rules {
		for _, inc := range r.Include {
			if p := filepath.Join(adapter.Home(r.Base), files.StaticPrefix(inc)); p != home {
				out = append(out, p)
			}
		}
	}

	return out
}

func configDir(name string) string {
	entries, err := os.ReadDir(adapter.Home("~/.config"))

	if err != nil {
		return ""
	}

	for _, e := range entries {
		if e.IsDir() && strings.EqualFold(e.Name(), name) {
			return e.Name()
		}
	}

	return ""
}

func isDir(path string) bool {
	info, err := os.Stat(adapter.Home(path))

	return err == nil && info.IsDir()
}

func unique(list []string) []string {
	seen := map[string]bool{}
	var out []string

	for _, v := range list {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}

	return out
}
