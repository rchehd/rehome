package cli

import (
	"strings"
	"testing"
)

func TestStatusRejectsIncompatibleInteractiveOutput(t *testing.T) {
	previous := flags
	t.Cleanup(func() { flags = previous })
	for _, args := range [][]string{{"--interactive", "--details"}, {"--interactive", "--details=false"}} {
		cmd := newStatusCmd()
		flags.json = len(args) == 2 && args[1] == "--details=false"
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatal(err)
		}
		if err := cmd.RunE(cmd, nil); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("incompatible output modes accepted: %v", err)
		}
	}
}
