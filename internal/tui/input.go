package tui

import (
	tea "charm.land/bubbletea/v2"
)

type input struct {
	title, help, value string
	done               bool
}

// Input asks for one line of text; ok is false when the user cancels.
func Input(title, help string) (string, bool, error) {
	final, err := tea.NewProgram(&input{title: title, help: help}).Run()

	if err != nil {
		return "", false, err
	}

	m := final.(*input)

	return m.value, m.done && m.value != "", nil
}

func (m *input) Init() tea.Cmd { return nil }

func (m *input) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)

	if !ok {
		return m, nil
	}

	switch key.String() {
	case "ctrl+c", "esc":
		return m, tea.Quit
	case "enter":
		m.done = true

		return m, tea.Quit
	case "backspace":
		if m.value != "" {
			r := []rune(m.value)
			m.value = string(r[:len(r)-1])
		}
	default:
		if key.Text != "" {
			m.value += key.Text
		}
	}

	return m, nil
}

func (m *input) View() tea.View {
	return tea.NewView(titleStyle.Render(m.title) + "\n" + dimStyle.Render(m.help) + "\n\n› " + m.value + "▏\n\n" + helpStyle.Render("enter save · esc cancel"))
}
