package snap

import "testing"

func TestRevision(t *testing.T) {
	out := "Name     Version  Rev   Tracking       Publisher  Notes\nfirefox  158.0.1  9113  latest/stable  mozilla**  -\n"

	if got := revision(out, "firefox"); got != "9113" {
		t.Errorf("revision = %q", got)
	}

	if got := revision(out, "vlc"); got != "" {
		t.Errorf("missing snap revision = %q", got)
	}
}
