package ai

import "testing"

func TestListedIsExact(t *testing.T) {
	out := "Installed plugins:\n\n  ❯ foo-bar@market\n    Version: 1\n  ❯ acc@awesome-claude-code\n"
	got := listed(out)

	if got["foo@market"] || !got["foo-bar@market"] || !got["acc@awesome-claude-code"] {
		t.Errorf("listed = %v", got)
	}
}

func TestMCPNames(t *testing.T) {
	out := "Checking MCP server health…\n\ncontext7: https://mcp.context7.com/mcp (HTTP) - ✔ Connected\nplaywright-headed: npx @playwright/mcp - ✔ Connected\nclaude.ai Claude Docs: https://x - ✔ Connected\n"
	got := mcpNames(out)

	if !got["context7"] || !got["playwright-headed"] || got["playwright"] || got["claude.ai Claude Docs"] {
		t.Errorf("names = %v", got)
	}
}
