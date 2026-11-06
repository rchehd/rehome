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
	"time"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
)

const claudeTimeout = 3 * time.Minute

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
func RestoreClaude(ctx context.Context, r *runner.Runner, root string, step func(string)) (notes []string, errs []error) {
	if !adapter.HasCmd("claude") && !adapter.BinExists("claude") {
		return nil, []error{errors.New("claude-code: claude is not installed, skipping plugins and MCP")}
	}

	if step == nil {
		step = func(string) {}
	}

	claude := "claude"

	if !adapter.HasCmd("claude") {
		claude = adapter.Home("~/.local/bin/claude")
	}

	var plugins Plugins

	if ok, err := readJSON(filepath.Join(root, PluginsFile), &plugins); err != nil {
		errs = append(errs, err)
	} else if ok {
		errs = append(errs, restorePlugins(ctx, r, claude, plugins, step)...)
	}

	var mcp map[string]json.RawMessage

	if ok, err := readJSON(filepath.Join(root, MCPFile), &mcp); err != nil {
		errs = append(errs, err)
	} else if ok {
		n, e := restoreMCP(ctx, r, claude, mcp, step)
		notes, errs = append(notes, n...), append(errs, e...)
	}

	return notes, errs
}

// run executes c under claudeTimeout; read-only commands also run in dry-run mode.
func run(ctx context.Context, r *runner.Runner, c runner.Cmd, read bool) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, claudeTimeout)
	defer cancel()

	var out string
	var err error

	if read {
		out, err = r.Output(cctx, c)
	} else {
		var res runner.Result
		res, err = r.Run(cctx, c)
		out = res.Stdout
	}

	if err != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return out, context.DeadlineExceeded
	}

	return out, err
}

func failure(kind, id string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s %s: timed out after %s", kind, id, claudeTimeout)
	}

	return fmt.Errorf("%s %s: %w", kind, id, err)
}

func restorePlugins(ctx context.Context, r *runner.Runner, claude string, p Plugins, step func(string)) []error {
	var errs []error
	have, _ := run(ctx, r, runner.Cmd{Name: claude, Args: []string{"plugin", "marketplace", "list"}}, true)

	for _, m := range p.Marketplaces {
		if listed(have)[m.Name] {
			continue
		}

		step("claude-code marketplace " + m.Name)

		if _, err := run(ctx, r, runner.Cmd{Name: claude, Args: []string{"plugin", "marketplace", "add", m.Source}}, false); err != nil {
			errs = append(errs, failure("marketplace", m.Name, err))
		}
	}

	installed, _ := run(ctx, r, runner.Cmd{Name: claude, Args: []string{"plugin", "list"}}, true)

	for _, id := range p.Installed {
		if listed(installed)[id] {
			continue
		}

		step("claude-code plugin " + id)

		if _, err := run(ctx, r, runner.Cmd{Name: claude, Args: []string{"plugin", "install", id}}, false); err != nil {
			errs = append(errs, failure("plugin", id, err))
		}
	}

	return errs
}

func restoreMCP(ctx context.Context, r *runner.Runner, claude string, mcp map[string]json.RawMessage, step func(string)) ([]string, []error) {
	var notes []string
	var errs []error
	have, _ := run(ctx, r, runner.Cmd{Name: claude, Args: []string{"mcp", "list"}}, true)

	for _, name := range sortedKeys(mcp) {
		cfg := mcp[name]

		if bytes.Contains(cfg, []byte(secrets.Placeholder)) {
			notes = append(notes, fmt.Sprintf("MCP server %s: fill in %s (claude mcp get %s)", name, secrets.Placeholder, name))
		}

		if mcpNames(have)[name] {
			continue
		}

		step("claude-code MCP " + name)

		if _, err := run(ctx, r, runner.Cmd{Name: claude, Args: []string{"mcp", "add-json", "--scope", "user", name, string(local(cfg))}}, false); err != nil {
			errs = append(errs, failure("mcp", name, err))
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

// listed reads the "❯ name" entries of `claude plugin list` and `claude plugin marketplace list`.
func listed(out string) map[string]bool {
	names := map[string]bool{}

	for _, l := range strings.Split(out, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(l), "❯ "); ok {
			names[strings.TrimSpace(name)] = true
		}
	}

	return names
}

// mcpNames reads the "name: command - status" lines of `claude mcp list`.
func mcpNames(out string) map[string]bool {
	names := map[string]bool{}

	for _, l := range strings.Split(out, "\n") {
		if name, _, ok := strings.Cut(l, ": "); ok && !strings.Contains(name, " ") {
			names[name] = true
		}
	}

	return names
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
