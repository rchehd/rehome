package lan

import "strings"

// Clean makes a peer-supplied string safe to print: control characters, ESC included, become "?".
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f {
			return '?'
		}

		return r
	}, s)
}
