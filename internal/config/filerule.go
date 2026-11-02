package config

import (
	"regexp"
	"strings"
)

const OriginAuto = "auto"

// FileRule selects settings files under Base; globs are relative and support **.
type FileRule struct {
	Base    string   `yaml:"base"`
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
	// MaxSizeMB overrides the default per-file size limit.
	MaxSizeMB int `yaml:"max_size_mb,omitempty"`
	// Origin is "auto" for rules scan found by itself; empty for the user's own.
	Origin string `yaml:"origin,omitempty"`
	// TextOnly skips binary files (auto rules never carry binaries).
	TextOnly bool `yaml:"text_only,omitempty"`
}

// Matches reports whether a relative path is selected by the rule.
func (r FileRule) Matches(rel string) bool {
	return anyGlob(r.Include, rel) && !anyGlob(r.Exclude, rel)
}

func (r FileRule) Excludes(rel string) bool {
	return anyGlob(r.Exclude, rel)
}

func anyGlob(patterns []string, rel string) bool {
	for _, p := range patterns {
		if GlobRegexp(p).MatchString(rel) {
			return true
		}
	}

	return false
}

// GlobRegexp compiles a path glob: ** spans directories, * and ? stay within one.
func GlobRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")

	for i := 0; i < len(pattern); i++ {
		switch ch := pattern[i]; {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(.*/)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case ch == '*':
			b.WriteString("[^/]*")
		case ch == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}

	b.WriteString("$")

	return regexp.MustCompile(b.String())
}
