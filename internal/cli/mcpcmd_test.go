package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestOfferMCPInstall(t *testing.T) {
	for _, answer := range []string{"y\n", "n\n", "\n", ""} {
		t.Run(strings.TrimSpace(answer)+"answer", func(t *testing.T) {
			cmd, output, home := mcpPromptFixture(t, answer)
			flags.config = "example.yml"
			path := filepath.Join(home, ".cursor", "mcp.json")
			original := []byte(`{"mcpServers":{"other":{"command":"other-server"}}}`)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}

			if err := offerMCPInstall(cmd); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "Connect rehome via MCP to cursor?") {
				t.Fatalf("missing confirmation: %s", output)
			}
			if answer != "y\n" {
				if !bytes.Equal(data, original) {
					t.Fatalf("declined registration changed settings: %s", data)
				}
				return
			}

			var doc struct {
				Servers map[string]struct {
					Command string   `json:"command"`
					Args    []string `json:"args"`
				} `json:"mcpServers"`
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			configPath, _ := filepath.Abs(flags.config)
			args := doc.Servers["rehome"].Args
			if doc.Servers["other"].Command != "other-server" || len(args) != 3 || args[0] != "--config" || args[1] != configPath || args[2] != "mcp" {
				t.Fatalf("unexpected registration: %s", data)
			}
			output.Reset()
			if err := offerMCPInstall(cmd); err != nil || output.Len() != 0 {
				t.Fatalf("repeated registration prompted again: %s, %v", output, err)
			}
		})
	}
}

func TestOfferMCPInstallSkipsDryRunAndMissingAgents(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		t.Run(map[bool]string{true: "dry-run", false: "no-agents"}[dryRun], func(t *testing.T) {
			cmd, output, home := mcpPromptFixture(t, "y\n")
			flags.dryRun = dryRun
			if !dryRun {
				if err := os.Remove(filepath.Join(home, ".cursor")); err != nil {
					t.Fatal(err)
				}
			}
			if err := offerMCPInstall(cmd); err != nil || output.Len() != 0 {
				t.Fatalf("unexpected prompt: %s, %v", output, err)
			}
			if _, err := os.Stat(filepath.Join(home, ".cursor", "mcp.json")); !os.IsNotExist(err) {
				t.Fatalf("settings were written: %v", err)
			}
		})
	}
}

func TestOfferMCPInstallReportsRegistrationFailure(t *testing.T) {
	cmd, output, home := mcpPromptFixture(t, "y\n")
	if err := os.Mkdir(filepath.Join(home, ".cursor", "mcp.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := offerMCPInstall(cmd)
	var exit ExitError
	if !errors.As(err, &exit) || exit.Code != 1 || !strings.Contains(output.String(), "failed") {
		t.Fatalf("registration failure was lost: %s, %v", output, err)
	}
}

func mcpPromptFixture(t *testing.T, answer string) (*cobra.Command, *bytes.Buffer, string) {
	t.Helper()
	savedFlags, savedStdin := flags, stdin
	t.Cleanup(func() { flags, stdin = savedFlags, savedStdin })
	flags = globalFlags{}
	stdin = bufio.NewReader(strings.NewReader(answer))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	if err := os.Mkdir(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	return cmd, &output, home
}
