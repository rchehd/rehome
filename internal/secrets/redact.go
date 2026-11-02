package secrets

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
)

const Placeholder = "<rehome:redacted>"

const minSecretLen = 6

type Finding struct {
	Path string
	Key  string
	Line int
}

var (
	// key = value, "key": "value", export KEY=value — only where a key can start (line start,
	// after { or , or npmrc's /:), so prose like "If all PASS: ..." is left alone.
	assignment = regexp.MustCompile(`(^\s*(?:export\s+|set\s+|-\s+)?|[{,]\s*|/:)(["']?)([A-Za-z_][\w.-]*)(["']?)(\s*[:=]\s*)(?:"((?:[^"\\]|\\.)*)"|'([^']*)'|([^\s"',#}\]]+))`)
	// --api-key VALUE, --token=VALUE, "--api-key", "VALUE".
	flagValue  = regexp.MustCompile(`(?i)(--?[a-z][\w-]*(?:key|token|secret|password)[\w-]*)("?\s*,\s*"|=|\s+)([^\s"',\]]{6,})`)
	tokenShape = regexp.MustCompile(`\b(sk-ant-[\w-]{20,}|sk-[A-Za-z0-9_-]{16,}|ghp_[A-Za-z0-9]{30,}|gho_[A-Za-z0-9]{30,}|ghu_[A-Za-z0-9]{30,}|ghs_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[\w-]{20,}|xox[abposr]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35})\b`)
	authScheme = regexp.MustCompile(`(?i)\b(bearer|basic|token)(\s+)([A-Za-z0-9._~+/=-]{12,})`)
	userinfo   = regexp.MustCompile(`([a-z][a-z0-9+.-]*://)([^/\s:@"']+)(?::([^/\s@"']+))?@`)
	privateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	camelSplit = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	varRef     = regexp.MustCompile(`^\$[A-Za-z_{(]`)
)

// In code `token = tokens[i]` is logic and in prose "key: ..." is text; only token shapes,
// URL credentials and private keys are redacted there.
var codeExt = map[string]bool{
	".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true, ".jsx": true, ".py": true,
	".go": true, ".rb": true, ".php": true, ".java": true, ".rs": true, ".lua": true, ".kt": true,
	".md": true, ".markdown": true, ".txt": true, ".rst": true,
}

// Redact replaces secret-looking values with Placeholder, keeping the file's formatting.
// Already redacted values are left alone, so redacting twice changes nothing.
func Redact(path string, data []byte) ([]byte, []Finding) {
	if IsBinary(data) {
		return data, nil
	}

	var findings []Finding
	text := privateKey.ReplaceAllStringFunc(string(data), func(string) string {
		findings = append(findings, Finding{Path: path, Key: "private key"})

		return Placeholder
	})

	lines := strings.Split(text, "\n")
	code := codeExt[strings.ToLower(filepath.Ext(path))]

	for i, line := range lines {
		redacted, keys := redactLine(line, code)

		for _, k := range keys {
			findings = append(findings, Finding{Path: path, Key: k, Line: i + 1})
		}

		lines[i] = redacted
	}

	return []byte(strings.Join(lines, "\n")), findings
}

// StripUserinfo removes credentials embedded in URLs: https://user:pass@host → https://host.
func StripUserinfo(s string) string {
	return userinfo.ReplaceAllString(s, "$1")
}

func redactLine(line string, code bool) (string, []string) {
	var keys []string
	hide := func(key string) string {
		keys = append(keys, key)

		return Placeholder
	}

	if !code {
		line = assignment.ReplaceAllStringFunc(line, func(m string) string {
			g := assignment.FindStringSubmatch(m)
			idx := assignment.FindStringSubmatchIndex(m)
			key := g[3]
			value, quote := g[8], ""

			switch {
			case idx[12] >= 0:
				value, quote = g[6], `"`
			case idx[14] >= 0:
				value, quote = g[7], "'"
			}

			if !isSecretKey(key) || !looksSecret(value) {
				return m
			}

			return g[1] + g[2] + key + g[4] + g[5] + quote + hide(key) + quote
		})

		line = flagValue.ReplaceAllStringFunc(line, func(m string) string {
			g := flagValue.FindStringSubmatch(m)

			if g[3] == Placeholder || varRef.MatchString(g[3]) {
				return m
			}

			return g[1] + g[2] + hide(g[1])
		})
	}

	line = tokenShape.ReplaceAllStringFunc(line, func(m string) string {
		return hide("token:" + m[:min(4, len(m))])
	})

	line = authScheme.ReplaceAllStringFunc(line, func(m string) string {
		g := authScheme.FindStringSubmatch(m)

		if g[3] == Placeholder || !hasDigitOrSymbol(g[3]) {
			return m
		}

		return g[1] + g[2] + hide(strings.ToLower(g[1]))
	})

	line = userinfo.ReplaceAllStringFunc(line, func(m string) string {
		g := userinfo.FindStringSubmatch(m)

		if strings.Contains(m, Placeholder) || (g[3] == "" && len(g[2]) < 16) {
			return m
		}

		return g[1] + hide("url credentials") + "@"
	})

	return line, keys
}

// IsBinary uses git's heuristic: a NUL byte in the first 8000 bytes.
func IsBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0
}

func isSecretKey(key string) bool {
	k := strings.ToLower(camelSplit.ReplaceAllString(key, "${1}_${2}"))
	k = strings.NewReplacer("-", "_", ".", "_").Replace(k)

	for _, part := range []string{"token", "secret", "password", "passwd", "passphrase", "credential", "bearer", "authorization"} {
		if strings.Contains(k, part) {
			return true
		}
	}

	segments := strings.Split(strings.Trim(k, "_"), "_")
	last := segments[len(segments)-1]

	return last == "key" || last == "pass" || last == "auth" || last == "pwd" || k == "_auth"
}

func looksSecret(v string) bool {
	if len(v) < minSecretLen || v == Placeholder || varRef.MatchString(v) || strings.HasPrefix(v, "<") {
		return false
	}

	switch strings.ToLower(v) {
	case "true", "false", "null", "none", "undefined":
		return false
	}

	return strings.Trim(v, "0123456789.") != ""
}

func hasDigitOrSymbol(s string) bool {
	return strings.ContainsAny(s, "0123456789+/=_-.")
}
