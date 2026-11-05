package discover

import "github.com/rchehd/rehome/internal/adapter"

func DesktopSessions() []Warning {
	var warnings []Warning
	for _, parent := range []string{"~/.config", "~/Library/Application Support"} {
		name := childDir(parent, "Claude")
		if name == "" {
			continue
		}
		path := parent + "/" + name + "/claude-code-sessions"
		if !isDir(path) {
			continue
		}
		warnings = append(warnings, Warning{
			ID: "claude-desktop", Path: adapter.Tilde(adapter.Home(path)),
			Reason: "Desktop session format is unsupported and excluded from automatic capture",
			Action: "--sessions transfers supported Claude Code, Codex and Cursor CLI/ACP sessions only; keep Desktop sessions on this device",
		})
	}
	return warnings
}
