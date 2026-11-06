package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// NodeRow is one node on the main screen; Missing counts its apps this machine does not have.
type NodeRow struct {
	Name, System, Seen string
	Apps, Missing      int
	This, Stale        bool
}

type nodesPicker struct {
	rows   []NodeRow
	cursor int
	height int
	chosen string
	ok     bool
}

// Nodes lets the user pick the node to install from; "" means every live node. ok is false when the user quits.
func Nodes(rows []NodeRow) (string, bool, error) {
	all := NodeRow{System: "every live node; versions and settings from the freshest"}
	m := &nodesPicker{rows: append([]NodeRow{all}, rows...), height: 20}
	final, err := tea.NewProgram(m).Run()

	if err != nil {
		return "", false, err
	}

	m = final.(*nodesPicker)

	return m.chosen, m.ok, nil
}

func (m *nodesPicker) Init() tea.Cmd { return nil }

func (m *nodesPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = max(3, msg.Height-5)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(len(m.rows)-1, m.cursor+1)
		case "enter":
			m.chosen, m.ok = m.rows[m.cursor].Name, true

			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *nodesPicker) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render("rehome install") + "  " + dimStyle.Render("pick a node to install its apps here") + "\n\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("  %-20s %-30s %5s %15s  %s", "NODE", "SYSTEM", "APPS", "NOT IN MY NODE", "LAST SEEN")) + "\n")
	start, end := visibleWindow(len(m.rows), m.cursor, m.height)

	for i := start; i < end; i++ {
		line := nodeLine(m.rows[i])

		if i == m.cursor {
			line = cursorStyle.Render(line)
		}

		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + helpStyle.Render("↑/↓ move · enter open · q quit"))
	v := tea.NewView(b.String())
	v.AltScreen = true

	return v
}

func nodeLine(r NodeRow) string {
	if r.Name == "" {
		return fmt.Sprintf("  %-20s %s", "all nodes", r.System)
	}

	note := r.Seen

	switch {
	case r.This:
		note += " · this machine"
	case r.Stale:
		note += " · stale"
	}

	return fmt.Sprintf("  %-20s %-30s %5d %15d  %s", clip(r.Name, 20), clip(r.System, 30), r.Apps, r.Missing, note)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}

	return s
}
