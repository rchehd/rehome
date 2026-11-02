package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type MenuItem struct {
	Key, Title, Help string
}

type menu struct {
	header string
	items  []MenuItem
	cursor int
	chosen string
}

// Menu returns the chosen item's Key, or "" when the user quits.
func Menu(header string, items []MenuItem) (string, error) {
	final, err := tea.NewProgram(&menu{header: header, items: items}).Run()

	if err != nil {
		return "", err
	}

	return final.(*menu).chosen, nil
}

func (m *menu) Init() tea.Cmd { return nil }

func (m *menu) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)

	if !ok {
		return m, nil
	}

	switch key.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.cursor = (m.cursor + len(m.items) - 1) % len(m.items)
	case "down", "j":
		m.cursor = (m.cursor + 1) % len(m.items)
	case "enter":
		m.chosen = m.items[m.cursor].Key

		return m, tea.Quit
	}

	return m, nil
}

func (m *menu) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render("rehome") + "  " + dimStyle.Render(m.header) + "\n\n")

	for i, it := range m.items {
		line := fmt.Sprintf("  %-8s %s", it.Title, dimStyle.Render(it.Help))

		if i == m.cursor {
			line = cursorStyle.Render(fmt.Sprintf("› %-8s", it.Title)) + " " + dimStyle.Render(it.Help)
		}

		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + helpStyle.Render("↑/↓ move · enter select · q quit"))

	return tea.NewView(b.String())
}
