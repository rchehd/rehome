package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
)

const (
	MCPFile     = "files/claude-code/mcp.json"
	PluginsFile = "files/claude-code/plugins.json"
)

type Plugins struct {
	Marketplaces []Marketplace `json:"marketplaces"`
	// Installed holds "plugin@marketplace" ids.
	Installed []string `json:"installed"`
}

type Marketplace struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// CaptureClaude extracts user MCP servers from ~/.claude.json (never the file itself,
// it holds tokens) and the plugin list, as changes to write under files/.
func CaptureClaude(root string) ([]files.Change, error) {
	var changes []files.Change
	mcp, err := mcpServers()

	if err != nil {
		return nil, err
	}

	if len(mcp) > 0 {
		c, err := jsonChange(root, MCPFile, mcp, true)

		if err != nil {
			return nil, err
		}

		changes = append(changes, c...)
	}

	plugins, err := pluginList()

	if err != nil {
		return nil, err
	}

	if len(plugins.Installed)+len(plugins.Marketplaces) > 0 {
		c, err := jsonChange(root, PluginsFile, plugins, false)

		if err != nil {
			return nil, err
		}

		changes = append(changes, c...)
	}

	return changes, nil
}

// RestoreClaude re-adds marketplaces, plugins and MCP servers through the claude CLI,
// skipping what is already there. notes lists MCP servers that need secrets filled in.
func RestoreClaude(ctx context.Context, r *runner.Runner, root string) (notes []string, errs []error) {
	if !adapter.HasCmd("claude") && !adapter.BinExists("claude") {
		return nil, []error{errors.New("claude-code: claude is not installed, skipping plugins and MCP")}
	}

	claude := "claude"

	if !adapter.HasCmd("claude") {
		claude = adapter.Home("~/.local/bin/claude")
	}

	var plugins Plugins

	if ok, err := readJSON(filepath.Join(root, PluginsFile), &plugins); err != nil {
		errs = append(errs, err)
	} else if ok {
		errs = append(errs, restorePlugins(ctx, r, claude, plugins)...)
	}

	var mcp map[string]json.RawMessage

	if ok, err := readJSON(filepath.Join(root, MCPFile), &mcp); err != nil {
		errs = append(errs, err)
	} else if ok {
		n, e := restoreMCP(ctx, r, claude, mcp)
		notes, errs = append(notes, n...), append(errs, e...)
	}

	return notes, errs
}

func restorePlugins(ctx context.Context, r *runner.Runner, claude string, p Plugins) []error {
	var errs []error
	have, _ := r.Output(ctx, runner.Cmd{Name: claude, Args: []string{"plugin", "marketplace", "list"}})

	for _, m := range p.Marketplaces {
		if strings.Contains(have, m.Name) {
			continue
		}

		if _, err := r.Run(ctx, runner.Cmd{Name: claude, Args: []string{"plugin", "marketplace", "add", m.Source}}); err != nil {
			errs = append(errs, fmt.Errorf("marketplace %s: %w", m.Name, err))
		}
	}

	installed, _ := r.Output(ctx, runner.Cmd{Name: claude, Args: []string{"plugin", "list"}})

	for _, id := range p.Installed {
		name, _, _ := strings.Cut(id, "@")

		if strings.Contains(installed, id) || strings.Contains(installed, name+" ") {
			continue
		}

		if _, err := r.Run(ctx, runner.Cmd{Name: claude, Args: []string{"plugin", "install", id}}); err != nil {
			errs = append(errs, fmt.Errorf("plugin %s: %w", id, err))
		}
	}

	return errs
}

func restoreMCP(ctx context.Context, r *runner.Runner, claude string, mcp map[string]json.RawMessage) ([]string, []error) {
	var notes []string
	var errs []error
	have, _ := r.Output(ctx, runner.Cmd{Name: claude, Args: []string{"mcp", "list"}})

	for _, name := range sortedKeys(mcp) {
		cfg := mcp[name]

		if bytes.Contains(cfg, []byte(secrets.Placeholder)) {
			notes = append(notes, fmt.Sprintf("MCP server %s: fill in %s (claude mcp get %s)", name, secrets.Placeholder, name))
		}

		if strings.Contains(have, name+":") {
			continue
		}

		if _, err := r.Run(ctx, runner.Cmd{Name: claude, Args: []string{"mcp", "add-json", "--scope", "user", name, string(local(cfg))}}); err != nil {
			errs = append(errs, fmt.Errorf("mcp %s: %w", name, err))
		}
	}

	return notes, errs
}

func mcpServers() (map[string]json.RawMessage, error) {
	var state struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}

	if _, err := readJSON(adapter.Home("~/.claude.json"), &state); err != nil {
		return nil, err
	}

	return state.MCPServers, nil
}

func pluginList() (Plugins, error) {
	var p Plugins
	var installed struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}

	if _, err := readJSON(adapter.Home("~/.claude/plugins/installed_plugins.json"), &installed); err != nil {
		return p, err
	}

	var markets map[string]struct {
		Source struct {
			Source string `json:"source"`
			Repo   string `json:"repo"`
			URL    string `json:"url"`
		} `json:"source"`
	}

	if _, err := readJSON(adapter.Home("~/.claude/plugins/known_marketplaces.json"), &markets); err != nil {
		return p, err
	}

	for _, name := range sortedKeys(markets) {
		src := markets[name].Source

		switch {
		case src.Repo != "":
			p.Marketplaces = append(p.Marketplaces, Marketplace{Name: name, Source: src.Repo})
		case src.URL != "":
			p.Marketplaces = append(p.Marketplaces, Marketplace{Name: name, Source: src.URL})
		}
	}

	p.Installed = sortedKeys(installed.Plugins)

	return p, nil
}

func jsonChange(root, dst string, v any, redact bool) ([]files.Change, error) {
	data, err := json.MarshalIndent(v, "", "  ")

	if err != nil {
		return nil, err
	}

	data = append(data, '\n')
	data = portable(data)
	var findings []secrets.Finding

	if redact {
		data, findings = secrets.Redact(dst, data)
	}

	old, _ := os.ReadFile(filepath.Join(root, dst))

	if bytes.Equal(old, data) {
		return nil, nil
	}

	return []files.Change{{Src: "~/.claude.json", Dst: dst, Old: old, New: data, Mode: 0o600, Findings: findings}}, nil
}

// portable turns "/home/me/..." strings into "~/..."; local reverses it for this machine.
func portable(data []byte) []byte {
	home, err := os.UserHomeDir()

	if err != nil {
		return data
	}

	return bytes.ReplaceAll(data, []byte(`"`+home+`/`), []byte(`"~/`))
}

func local(data []byte) []byte {
	home, err := os.UserHomeDir()

	if err != nil {
		return data
	}

	return bytes.ReplaceAll(data, []byte(`"~/`), []byte(`"`+home+`/`))
}

// readJSON reports false without error when the file does not exist.
func readJSON(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)

	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, json.Unmarshal(data, v)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))

	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}
