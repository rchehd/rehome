package uvtool

import (
	"testing"

	"github.com/rchehd/rehome/internal/config"
)

func TestToolVersion(t *testing.T) {
	out := "graphifyy v0.9.65\n- graphify\nserena-agent v1.5.3\n- serena\n"

	if got := toolVersion(out, "serena-agent"); got != "1.5.3" {
		t.Errorf("version = %q", got)
	}

	if got := toolVersion(out, "graphify"); got != "" {
		t.Errorf("entrypoint matched as tool: %q", got)
	}
}

func TestToolName(t *testing.T) {
	if got := toolName(config.InstallSpec{UvTool: "graphifyy[mcp] --python 3.13"}); got != "graphifyy" {
		t.Errorf("name = %q", got)
	}
}
