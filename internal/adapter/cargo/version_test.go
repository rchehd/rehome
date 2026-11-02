package cargo

import "testing"

func TestCrateVersion(t *testing.T) {
	out := "ripgrep v14.1.0:\n    rg\nbat v0.24.0:\n    bat\n"

	if got := crateVersion(out, "bat"); got != "0.24.0" {
		t.Errorf("version = %q", got)
	}

	if got := crateVersion(out, "rg"); got != "" {
		t.Errorf("binary matched as crate: %q", got)
	}
}
