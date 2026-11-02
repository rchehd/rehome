package mas

import "testing"

func TestParseList(t *testing.T) {
	got := parseList("497799835  Xcode  (15.0)\n1333542190  1Password 7 - Password Manager  (7.9.11)\n")

	if got["497799835"] != "xcode" || got["1333542190"] != "1password-7---password-manager" {
		t.Errorf("parsed = %v", got)
	}
}
