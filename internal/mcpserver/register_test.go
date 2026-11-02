package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rchehd/rehome/internal/runner"
)

// fakeAgents puts claude and codex stand-ins on PATH that remember `mcp add` in a file.
func fakeAgents(t *testing.T) (string, string) {
	t.Helper()
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	added := filepath.Join(home, "added")
	script := `#!/bin/sh
case "$2" in
  add) echo "$0 $*" >> ` + added + ` ;;
  get) grep -q "$(basename $0)" ` + added + ` 2>/dev/null ;;
  list) grep -q "$(basename $0)" ` + added + ` 2>/dev/null && echo "rehome  /bin/rehome mcp" ;;
esac
`

	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	return home, added
}

func statuses(results []Result) map[string]string {
	out := map[string]string{}

	for _, r := range results {
		out[r.Client] = r.Status
	}

	return out
}

func TestRegisterAllFoundAgentsOnce(t *testing.T) {
	home, added := fakeAgents(t)
	os.MkdirAll(filepath.Join(home, ".cursor"), 0o755)
	os.WriteFile(filepath.Join(home, ".cursor/mcp.json"), []byte(`{"mcpServers": {"other": {"command": "x"}}}`), 0o644)
	command := []string{"/opt/rehome", "mcp"}

	results, err := Register(context.Background(), &runner.Runner{}, command, "")

	if err != nil {
		t.Fatal(err)
	}

	if got := statuses(results); got["claude-code"] != StatusRegistered || got["codex"] != StatusRegistered || got["cursor"] != StatusRegistered {
		t.Fatalf("first run = %v", got)
	}

	calls, _ := os.ReadFile(added)

	if !strings.Contains(string(calls), "mcp add --scope user rehome -- /opt/rehome mcp") || !strings.Contains(string(calls), "codex mcp add rehome -- /opt/rehome mcp") {
		t.Errorf("agent calls:\n%s", calls)
	}

	var doc struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}

	data, _ := os.ReadFile(filepath.Join(home, ".cursor/mcp.json"))
	json.Unmarshal(data, &doc)

	if doc.MCPServers["rehome"]["command"] != "/opt/rehome" || doc.MCPServers["other"] == nil {
		t.Errorf("cursor config = %s", data)
	}

	results, _ = Register(context.Background(), &runner.Runner{}, command, "")

	for client, status := range statuses(results) {
		if status != StatusPresent {
			t.Errorf("second run %s = %s", client, status)
		}
	}
}

func TestRegisterSkipsMissingAgentsAndRejectsUnknown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	results, err := Register(context.Background(), &runner.Runner{}, []string{"rehome", "mcp"}, "")

	if err != nil {
		t.Fatal(err)
	}

	for client, status := range statuses(results) {
		if status != StatusNotFound {
			t.Errorf("%s = %s", client, status)
		}
	}

	if _, err := Register(context.Background(), &runner.Runner{}, nil, "vim"); err == nil {
		t.Error("unknown client accepted")
	}
}

func TestCommandKeepsConfig(t *testing.T) {
	cmd, err := Command("cfg")

	if err != nil {
		t.Fatal(err)
	}

	if cmd[len(cmd)-1] != "mcp" || cmd[1] != "--config" || !filepath.IsAbs(cmd[2]) {
		t.Errorf("command = %v", cmd)
	}
}
