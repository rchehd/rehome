package ui

import "charm.land/lipgloss/v2"

// Mark prefixes a line with what happens to the thing it names.
type Mark string

const (
	Add       Mark = "+"
	Update    Mark = "~"
	Drop      Mark = "-"
	Unknown   Mark = "?"
	Attention Mark = "!"
	Done      Mark = "✓"
	Fail      Mark = "✗"
	Step      Mark = "»"
	Plain     Mark = " "
)

var (
	green  = lipgloss.Color("10")
	blue   = lipgloss.Color("12")
	red    = lipgloss.Color("9")
	yellow = lipgloss.Color("11")
	purple = lipgloss.Color("13")
	gray   = lipgloss.Color("8")

	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(blue)
	GroupStyle = lipgloss.NewStyle().Bold(true).Foreground(purple)
	BoldStyle  = lipgloss.NewStyle().Bold(true)
	MutedStyle = lipgloss.NewStyle().Foreground(gray)
	HelpStyle  = MutedStyle.Italic(true)

	markStyles = map[Mark]lipgloss.Style{
		Add:       lipgloss.NewStyle().Foreground(green),
		Update:    lipgloss.NewStyle().Foreground(blue),
		Drop:      lipgloss.NewStyle().Foreground(red),
		Unknown:   lipgloss.NewStyle().Foreground(yellow),
		Attention: lipgloss.NewStyle().Foreground(yellow).Bold(true),
		Done:      lipgloss.NewStyle().Foreground(green),
		Fail:      lipgloss.NewStyle().Foreground(red).Bold(true),
		Step:      MutedStyle,
	}
)

// Paint colors s the way lines with mark m are colored.
func Paint(m Mark, s string) string {
	return markStyles[m].Render(s)
}

func Muted(s string) string {
	return MutedStyle.Render(s)
}

func Bold(s string) string {
	return BoldStyle.Render(s)
}
