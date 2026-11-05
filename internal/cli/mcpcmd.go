package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/mcpserver"
	"github.com/rchehd/rehome/internal/runner"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve rehome to AI agents over MCP (stdio)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return mcpserver.New(opts(cmd), Version).Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
	cmd.AddCommand(newMCPInstallCmd())

	return cmd
}

func newMCPInstallCmd() *cobra.Command {
	var only string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Add rehome to the AI agents found here (Claude Code, Codex, Cursor)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return installMCP(cmd, only)
		},
	}
	cmd.Flags().StringVar(&only, "client", "", "only this agent: claude-code, codex or cursor")

	return cmd
}

func installMCP(cmd *cobra.Command, only string) error {
	command, err := mcpserver.Command(flags.config)

	if err != nil {
		return err
	}

	results, err := mcpserver.Register(cmd.Context(), newRunner(), command, only)

	if err != nil {
		return err
	}

	var text strings.Builder

	for _, r := range results {
		fmt.Fprintf(&text, "%-12s %s", r.Client, r.Status)

		if r.Detail != "" {
			fmt.Fprintf(&text, ": %s", r.Detail)
		}

		text.WriteString("\n")
	}

	text.WriteString("other MCP clients: command " + strings.Join(command, " "))

	if err := report(cmd, results, text.String()); err != nil {
		return err
	}

	for _, r := range results {
		if r.Status == mcpserver.StatusFailed {
			return ExitError{Code: 1}
		}
	}

	return nil
}

func offerMCPInstall(cmd *cobra.Command) error {
	if flags.dryRun {
		return nil
	}

	command, err := mcpserver.Command(flags.config)

	if err != nil {
		return err
	}

	results, err := mcpserver.Register(cmd.Context(), &runner.Runner{DryRun: true, Log: io.Discard}, command, "")

	if err != nil {
		return err
	}

	var clients []string

	for _, result := range results {
		if result.Status == mcpserver.StatusWould {
			clients = append(clients, result.Client)
		}
	}

	if len(clients) == 0 {
		return nil
	}

	cmd.Println("Connect rehome via MCP to " + strings.Join(clients, ", ") + "?")

	if ask(cmd, "Add rehome to these agents' user settings? [y/N]", "n") != "y" {
		return nil
	}

	return installMCP(cmd, "")
}
