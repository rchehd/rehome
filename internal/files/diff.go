package files

import (
	"fmt"
	"strings"
)

const maxDiffLines = 2000

// Unified renders a line diff of old → new; new files show as all additions.
func Unified(old, new []byte, name string) string {
	a := splitLines(old)
	b := splitLines(new)

	if len(a)+len(b) > maxDiffLines {
		return fmt.Sprintf("--- %s\n+++ %s\n(%d → %d lines, too large to show)\n", name, name, len(a), len(b))
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s (current)\n+++ %s (new)\n", name, name)

	for _, op := range lcsDiff(a, b) {
		out.WriteString(op)
		out.WriteByte('\n')
	}

	return out.String()
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}

	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// lcsDiff emits only changed lines and one line of context around them.
func lcsDiff(a, b []string) []string {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)

	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}

	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	type line struct {
		op   byte
		text string
	}

	var all []line
	i, j := 0, 0

	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			all = append(all, line{' ', a[i]})
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			all = append(all, line{'+', b[j]})
			j++
		default:
			all = append(all, line{'-', a[i]})
			i++
		}
	}

	var out []string

	for k, l := range all {
		near := l.op != ' ' ||
			(k > 0 && all[k-1].op != ' ') ||
			(k+1 < len(all) && all[k+1].op != ' ')

		if near {
			out = append(out, string(l.op)+l.text)
		}
	}

	return out
}
