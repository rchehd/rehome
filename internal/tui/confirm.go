package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type confirm struct {
	title    string
	lines    []string
	offset   int
	height   int
	accepted bool
}

// Confirm shows a scrollable text and asks yes or no.
func Confirm(title, body string) (bool, error) {
	m := &confirm{title: title, lines: strings.Split(strings.TrimRight(body, "\n"), "\n"), height: 20}
	final, err := tea.NewProgram(m).Run()

	if err != nil {
		return false, err
	}

	return final.(*confirm).accepted, nil
}

func (m *confirm) Init() tea.Cmd { return nil }

func (m *confirm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	maxOffset := max(0, len(m.lines)-m.height)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = max(3, msg.Height-4)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y", "enter":
			m.accepted = true

			return m, tea.Quit
		case "n", "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.offset = max(0, m.offset-1)
		case "down", "j":
			m.offset = min(maxOffset, m.offset+1)
		case "pgup":
			m.offset = max(0, m.offset-m.height)
		case "pgdown", "space":
			m.offset = min(maxOffset, m.offset+m.height)
		}
	}

	return m, nil
}

func (m *confirm) View() tea.View {
	end := min(len(m.lines), m.offset+m.height)
	body := strings.Join(m.lines[m.offset:end], "\n")
	help := helpStyle.Render("↑/↓ scroll · y/enter yes · n/q no")
	v := tea.NewView(titleStyle.Render(m.title) + "\n" + body + "\n" + help)
	v.AltScreen = true

	return v
}
