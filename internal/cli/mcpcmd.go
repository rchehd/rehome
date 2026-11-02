package cli

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/mcpserver"
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
		},
	}
	cmd.Flags().StringVar(&only, "client", "", "only this agent: claude-code, codex or cursor")

	return cmd
}
