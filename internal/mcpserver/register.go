package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/runner"
)

const (
	serverName = "rehome"

	StatusRegistered = "registered"
	StatusWould      = "would register"
	StatusPresent    = "already registered"
	StatusNotFound   = "not installed"
	StatusFailed     = "failed"
)

type Result struct {
	Client string `json:"client"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// client knows how to find one agent and add rehome to it.
type client struct {
	name     string
	found    func() bool
	present  func(ctx context.Context, r *runner.Runner) bool
	register func(ctx context.Context, r *runner.Runner, command []string) error
}

var clients = []client{
	{
		name:  "claude-code",
		found: func() bool { return adapter.HasCmd("claude") },
		present: func(ctx context.Context, r *runner.Runner) bool {
			_, err := r.Output(ctx, runner.Cmd{Name: "claude", Args: []string{"mcp", "get", serverName}})

			return err == nil
		},
		register: func(ctx context.Context, r *runner.Runner, command []string) error {
			args := append([]string{"mcp", "add", "--scope", "user", serverName, "--"}, command...)
			_, err := r.Run(ctx, runner.Cmd{Name: "claude", Args: args})

			return err
		},
	},
	{
		name:  "codex",
		found: func() bool { return adapter.HasCmd("codex") },
		present: func(ctx context.Context, r *runner.Runner) bool {
			out, err := r.Output(ctx, runner.Cmd{Name: "codex", Args: []string{"mcp", "list"}})

			return err == nil && hasServerLine(out)
		},
		register: func(ctx context.Context, r *runner.Runner, command []string) error {
			args := append([]string{"mcp", "add", serverName, "--"}, command...)
			_, err := r.Run(ctx, runner.Cmd{Name: "codex", Args: args})

			return err
		},
	},
	{
		name:  "cursor",
		found: func() bool { return adapter.HasCmd("cursor") || isDir("~/.cursor") },
		present: func(context.Context, *runner.Runner) bool {
			servers, _ := cursorServers()
			_, ok := servers[serverName]

			return ok
		},
		register: func(_ context.Context, r *runner.Runner, command []string) error {
			if r.DryRun {
				return nil
			}

			return addCursorServer(command)
		},
	},
}

// Register adds rehome to every agent found here (or only the named one); it never duplicates.
func Register(ctx context.Context, r *runner.Runner, command []string, only string) ([]Result, error) {
	var results []Result

	for _, c := range clients {
		if only != "" && c.name != only {
			continue
		}

		res := Result{Client: c.name}

		switch {
		case !c.found():
			res.Status = StatusNotFound
		case c.present(ctx, r):
			res.Status = StatusPresent
		default:
			res.Status = StatusRegistered

			if r.DryRun {
				res.Status = StatusWould
			}

			if err := c.register(ctx, r, command); err != nil {
				res.Status, res.Detail = StatusFailed, err.Error()
			}
		}

		results = append(results, res)
	}

	if only != "" && len(results) == 0 {
		return nil, errors.New("unknown client " + only + " (claude-code, codex, cursor)")
	}

	return results, nil
}

// Command is how agents should start this rehome binary, with --config kept if given.
func Command(configFlag string) ([]string, error) {
	exe, err := os.Executable()

	if err != nil {
		return nil, err
	}

	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	command := []string{exe}

	if configFlag != "" {
		if abs, err := filepath.Abs(configFlag); err == nil && !strings.Contains(configFlag, "://") {
			configFlag = abs
		}

		command = append(command, "--config", configFlag)
	}

	return append(command, "mcp"), nil
}

func hasServerLine(out string) bool {
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) > 0 && strings.TrimSuffix(f[0], ":") == serverName {
			return true
		}
	}

	return false
}

func cursorPath() string {
	return adapter.Home("~/.cursor/mcp.json")
}

func cursorServers() (map[string]any, map[string]any) {
	doc := map[string]any{}
	data, err := os.ReadFile(cursorPath())

	if err == nil {
		_ = json.Unmarshal(data, &doc)
	}

	servers, _ := doc["mcpServers"].(map[string]any)

	if servers == nil {
		servers = map[string]any{}
	}

	return servers, doc
}

// addCursorServer adds rehome to ~/.cursor/mcp.json, keeping every other server.
func addCursorServer(command []string) error {
	servers, doc := cursorServers()
	servers[serverName] = map[string]any{"command": command[0], "args": command[1:]}
	doc["mcpServers"] = servers
	data, err := json.MarshalIndent(doc, "", "  ")

	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(cursorPath()), 0o755); err != nil {
		return err
	}

	return os.WriteFile(cursorPath(), append(data, '\n'), 0o600)
}

func isDir(path string) bool {
	info, err := os.Stat(adapter.Home(path))

	return err == nil && info.IsDir()
}
