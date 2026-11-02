package discover

import (
	"slices"
	"strings"
)

// Generic words that would match unrelated directories.
var stopNames = []string{"cli", "app", "bin", "server", "client", "common", "data", "core", "tools", "utils", "lib", "desktop", "config", "local", "share"}

var suffixes = []string{"-desktop", "-cli", "-app", "-bin", "-stable", "-launcher", "-ce", "-community", "-editor"}

// Names builds the lookup names for an app from its id, package and binaries. The id and
// package may be two letters (mc, uv, go); derived names and binaries need three.
func Names(id, pkg string, bins []string) []string {
	var out []string
	add := func(n string, minLen int) {
		if len(n) >= minLen && !slices.Contains(stopNames, n) && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}

	for i, raw := range append([]string{strings.TrimPrefix(id, "unknown-"), pkg}, bins...) {
		minLen := 3

		if i < 2 {
			minLen = 2
		}

		// Flatpak ids keep their full form for ~/.var/app and add their last segment.
		if strings.Count(raw, ".") >= 2 && !strings.Contains(raw, "/") {
			add(raw, minLen)
			raw = raw[strings.LastIndex(raw, ".")+1:]
		}

		n := strings.ToLower(raw)

		if j := strings.LastIndex(n, "/"); j >= 0 {
			n = n[j+1:]
		}

		add(n, minLen)

		for _, s := range suffixes {
			if base, ok := strings.CutSuffix(n, s); ok {
				add(base, 3)
			}
		}
	}

	return out
}
