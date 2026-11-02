package lock

import (
	"os/exec"
	"testing"
)

func TestGenericCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.10.0", "1.9.9", 1},
		{"1.2", "1.2.0", -1},
		{"v22.12.0", "22.12.0", 0},
		{"2.0.0-rc1", "2.0.0-rc2", -1},
		{"1.0.0", "1.0.0", 0},
		{"10", "9", 1},
	}

	for _, c := range cases {
		if got := Compare("npm", c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestAptCompareUsesDpkg(t *testing.T) {
	if _, err := exec.LookPath("dpkg"); err != nil {
		t.Skip("dpkg not available")
	}

	cases := []struct {
		a, b string
		want int
	}{
		{"1:1.0", "2.0", 1},
		{"1.0~rc1", "1.0", -1},
		{"3.3.0-4build1", "3.3.0-4", 1},
	}

	for _, c := range cases {
		if got := Compare("apt", c.a, c.b); got != c.want {
			t.Errorf("Compare(apt, %q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
