package script

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewestNode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, v := range []string{"v20.20.0", "v22.9.0", "v22.10.1"} {
		dir := filepath.Join(home, ".nvm/versions/node", v, "bin")
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "node"), nil, 0o755)
	}

	if got := newestNode("~/.nvm/versions/node/v22*/bin/node"); got != "22.10.1" {
		t.Errorf("newest = %q", got)
	}

	if got := newestNode("~/.nvm/versions/node/*/bin/node"); got != "22.10.1" {
		t.Errorf("newest any = %q", got)
	}
}
