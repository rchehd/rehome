package lock

import (
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

// Compare returns -1, 0 or 1. apt versions go through dpkg's own rules (epochs, ~);
// everything else compares numeric runs numerically and the rest as text.
func Compare(manager, a, b string) int {
	if a == b {
		return 0
	}

	if manager == "apt" {
		if c, ok := dpkgCompare(a, b); ok {
			return c
		}
	}

	return genericCompare(a, b)
}

func dpkgCompare(a, b string) (int, bool) {
	if _, err := exec.LookPath("dpkg"); err != nil {
		return 0, false
	}

	if exec.Command("dpkg", "--compare-versions", a, "lt", b).Run() == nil {
		return -1, true
	}

	if exec.Command("dpkg", "--compare-versions", a, "gt", b).Run() == nil {
		return 1, true
	}

	return 0, true
}

func genericCompare(a, b string) int {
	ta, tb := tokens(strings.TrimPrefix(a, "v")), tokens(strings.TrimPrefix(b, "v"))

	for i := 0; i < max(len(ta), len(tb)); i++ {
		if i >= len(ta) {
			return -1
		}

		if i >= len(tb) {
			return 1
		}

		if c := compareToken(ta[i], tb[i]); c != 0 {
			return c
		}
	}

	return 0
}

// tokens splits "1.10.0-rc1" into numeric and textual runs: 1 10 0 rc 1.
func tokens(s string) []string {
	var out []string
	var cur strings.Builder
	digits := false

	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}

	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			if !digits {
				flush()
			}

			digits = true
			cur.WriteRune(r)
		case unicode.IsLetter(r):
			if digits {
				flush()
			}

			digits = false
			cur.WriteRune(r)
		default:
			flush()
			digits = false
		}
	}

	flush()

	return out
}

func compareToken(a, b string) int {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)

	switch {
	case errA == nil && errB == nil:
		return cmpInt(na, nb)
	case errA == nil:
		return 1
	case errB == nil:
		return -1
	}

	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}

	return 0
}
