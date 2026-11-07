package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/rchehd/rehome/internal/ui"
)

var (
	titleStyle  = ui.TitleStyle
	groupStyle  = ui.GroupStyle
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	dimStyle    = ui.MutedStyle
	helpStyle   = ui.HelpStyle

	statusStyles = map[string]lipgloss.Style{
		"missing":   lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		"settings":  lipgloss.NewStyle().Foreground(lipgloss.Color("14")),
		"installed": lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		"n/a":       lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		"disabled":  lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		"skipped":   lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
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
