package tui

import "charm.land/lipgloss/v2"

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	groupStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)

	statusStyles = map[string]lipgloss.Style{
		"missing":   lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		"settings":  lipgloss.NewStyle().Foreground(lipgloss.Color("14")),
		"installed": lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		"n/a":       lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		"disabled":  lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
)

// visibleWindow returns the [start, end) slice of n rows that keeps cursor on screen.
func visibleWindow(n, cursor, height int) (int, int) {
	if height <= 0 || n <= height {
		return 0, n
	}

	start := max(0, min(cursor-height/2, n-height))

	return start, start + height
}
