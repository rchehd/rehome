package config

import "slices"

var autoExcludes = []string{
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
	"**/claude-code-sessions/**", "**/Crashpad/**", "**/VideoDecodeStats/**", "**/VideoEncodeStats/**",
	"**/SharedStorage/**", "**/SharedStorage*", "**/claude-code/*/*/.payload", "**/claude-code/*/*/.verified",
	"**/LOCK", "**/LOG", "**/LOG.*", "**/*.LOG", "**/*.LOCK",
	"**/buddy-tokens.json", "**/ant-device-registry.json", "**/ant-did", "**/ccd-ids.json",
	"**/credentials/**", "**/*_key.key", "**/*-key.pem", "**/cakey.pem", "**/*.key",
	"**/.cache/**", "**/*-cache/**", "**/*_cache/**", "**/*Cache/**", "**/session-state/**", "**/*.jsonl",
	"**/git-shadow/**",
	// Agent sessions, telemetry and app state rather than settings.
	"**/*-sessions/**", "**/sentry/**", "**/shared_proto_db/**", "**/spaces-present/**", "**/*-state.json",
	"**/.state.yaml", "**/project_list.yaml", "**/ca-bundle.pem", "**/.runner_system_id",
	"**/*.trace", "**/*_trace.txt", "**/nohup.out", "**/blob-store/**", "**/.apm/**", "**/pkg/**",
	"**/terminus-dependencies-*/**", "**/.terminus/plugins-*/**", "**/File System/**", "**/telemetry/**",
	"**/intelephense/workspace/**", "**/*.bak", "**/*.bak-*",
	// Desktop plumbing snap and flatpak copy into each sandbox; pulse/cookie is an auth key.
	"**/ibus/bus/**", "**/pulse/cookie", "**/user-dirs.*", "**/*.md5sum", "**/*.uuid",
	"snap/*/current/.config/gtk-3.0/**", "snap/*/current/.config/fontconfig/**",
}

func AutoExcludes() []string {
	return slices.Clone(autoExcludes)
}

// WithAutoDefaults also protects auto rules saved by earlier releases.
func (r FileRule) WithAutoDefaults() FileRule {
	if r.Origin != OriginAuto {
		return r
	}

	r.Exclude = slices.Clone(r.Exclude)
	for _, pattern := range autoExcludes {
		if !slices.Contains(r.Exclude, pattern) {
			r.Exclude = append(r.Exclude, pattern)
		}
	}
	r.TextOnly = true
	return r
}
