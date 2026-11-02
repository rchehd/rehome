package discover

import (
	"slices"
	"testing"
)

func TestNames(t *testing.T) {
	cases := []struct {
		id, pkg string
		bins    []string
		want    []string
	}{
		{"neovim", "neovim", []string{"nvim"}, []string{"neovim", "nvim"}},
		{"telegram-desktop", "telegram-desktop", nil, []string{"telegram-desktop", "telegram"}},
		{"org.gnome.Evolution", "org.gnome.Evolution", nil, []string{"org.gnome.Evolution", "evolution"}},
		{"playwright-cli", "@playwright/cli", nil, []string{"playwright-cli", "playwright"}},
	}

	if got := Names("x", "@scope/cli", []string{"ls"}); slices.Contains(got, "cli") || slices.Contains(got, "ls") {
		t.Errorf("generic or too short names kept: %v", got)
	}

	for _, c := range cases {
		got := Names(c.id, c.pkg, c.bins)

		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("Names(%s) = %v, missing %s", c.id, got, w)
			}
		}
	}
}
