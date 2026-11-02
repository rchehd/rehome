package brew

import "testing"

func TestParseVersion(t *testing.T) {
	out := "htop 3.3.0\nnode@20 20.18.1 20.17.0\n"

	if got := parseVersion(out, "node@20"); got != "20.18.1" {
		t.Errorf("node@20 = %q", got)
	}

	if got := parseVersion(out, "jq"); got != "" {
		t.Errorf("missing = %q", got)
	}
}

func TestSplitTap(t *testing.T) {
	cases := map[string][2]string{
		"htop":                          {"", "htop"},
		"homebrew/cask-fonts/font-hack": {"homebrew/cask-fonts", "font-hack"},
		"oven-sh/bun/bun":               {"oven-sh/bun", "bun"},
	}

	for in, want := range cases {
		if tap, name := splitTap(in); tap != want[0] || name != want[1] {
			t.Errorf("splitTap(%q) = %q, %q", in, tap, name)
		}
	}

	if got := qualified("oven-sh/bun", "bun"); got != "oven-sh/bun/bun" {
		t.Errorf("qualified = %q", got)
	}
}
