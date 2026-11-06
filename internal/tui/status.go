package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rchehd/rehome/internal/status"
)

type statusBrowser struct {
	report                   *status.Status
	sections                 []status.Section
	category, cursor, offset int
	width, height            int
	filter                   string
	filtering, details       bool
}

func Status(ctx context.Context, report *status.Status, input io.Reader, output io.Writer) error {
	_, err := tea.NewProgram(newStatusBrowser(report), tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output)).Run()
	return err
}

func newStatusBrowser(report *status.Status) *statusBrowser {
	m := &statusBrowser{report: report, width: 88, height: 24}
	for _, section := range report.Sections() {
		if section.Count > 0 {
			m.sections = append(m.sections, section)
		}
	}
	if len(m.sections) == 0 {
		m.sections = []status.Section{{Title: "Local system", Action: "No local differences to apply", Items: []status.Item{{Name: "In sync", Detail: []string{"The system matches the local config."}}}}}
	}
	return m
}

func (m *statusBrowser) Init() tea.Cmd { return nil }

func (m *statusBrowser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.offset = min(m.offset, max(0, len(m.detailLines())-m.pageSize()))
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.filtering {
			switch key {
			case "enter":
				m.filtering = false
			case "esc":
				m.filter, m.filtering = "", false
			case "backspace":
				runes := []rune(m.filter)
				if len(runes) > 0 {
					m.filter = string(runes[:len(runes)-1])
				}
			default:
				m.filter += msg.Text
			}
			m.cursor = 0
			return m, nil
		}
		if key == "q" {
			return m, tea.Quit
		}
		if m.details {
			maximum := max(0, len(m.detailLines())-m.pageSize())
			switch key {
			case "esc", "enter":
				m.details, m.offset = false, 0
			case "up", "k":
				m.offset = max(0, m.offset-1)
			case "down", "j":
				m.offset = min(maximum, m.offset+1)
			case "pgup":
				m.offset = max(0, m.offset-m.pageSize())
			case "pgdown", "space":
				m.offset = min(maximum, m.offset+m.pageSize())
			case "home":
				m.offset = 0
			case "end":
				m.offset = maximum
			}
			return m, nil
		}
		switch key {
		case "esc":
			if m.filter == "" {
				return m, tea.Quit
			}
			m.filter, m.cursor = "", 0
		case "tab", "]", "shift+tab", "[":
			delta := 1
			if key == "shift+tab" || key == "[" {
				delta = -1
			}
			m.category = (m.category + delta + len(m.sections)) % len(m.sections)
			m.cursor, m.filter = 0, ""
		case "/":
			m.filtering = true
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(max(0, len(m.items())-1), m.cursor+1)
		case "pgup":
			m.cursor = max(0, m.cursor-m.pageSize())
		case "pgdown":
			m.cursor = min(max(0, len(m.items())-1), m.cursor+m.pageSize())
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = max(0, len(m.items())-1)
		case "enter":
			if len(m.items()) > 0 {
				m.details, m.offset = true, 0
			}
		}
	}
	return m, nil
}

func (m *statusBrowser) items() []status.Item {
	var items []status.Item
	for _, item := range m.sections[m.category].Items {
		text := item.Name + " " + item.Installed + " " + item.Recorded + " " + strings.Join(item.Detail, " ")
		if strings.Contains(strings.ToLower(text), strings.ToLower(m.filter)) {
			items = append(items, item)
		}
	}
	return items
}

func (m *statusBrowser) pageSize() int { return max(1, m.height-9) }

func (m *statusBrowser) detailLines() []string {
	items := m.items()
	if len(items) == 0 {
		return nil
	}
	item := items[min(m.cursor, len(items)-1)]
	lines := []string{item.Name, ""}
	if item.Installed != "" {
		lines = append(lines, "On machine: "+item.Installed)
	}
	if item.Recorded != "" {
		lines = append(lines, "In config: "+item.Recorded)
	}
	lines = append(lines, m.sections[m.category].Action, "")
	lines = append(lines, item.Detail...)
	wrapped := ansi.Hardwrap(strings.Join(lines, "\n"), m.width, false)
	return strings.Split(wrapped, "\n")
}

func (m *statusBrowser) View() tea.View {
	section := m.sections[m.category]
	lines := []string{
		titleStyle.Render(statusClip("rehome status", m.width)),
		dimStyle.Render(statusClip("Config source: "+m.report.SourceLabel(), m.width)),
		dimStyle.Render(statusClip("This node: "+m.report.NodeLabel(), m.width)),
		"",
		groupStyle.Render(statusClip(fmt.Sprintf("%s (%d) · category %d/%d", section.Title, section.Count, m.category+1, len(m.sections)), m.width)),
	}
	search := "Search: / to filter this category"
	if m.filter != "" || m.filtering {
		search = "Search: " + m.filter
		if m.filtering {
			search += "▏"
		}
	}
	lines = append(lines, statusClip(search, m.width))
	body := []string{}
	footer := ""
	help := "tab/shift+tab category · ↑/↓ move · / search · enter details · q quit"
	if m.filtering {
		help = "type to search · enter apply · esc clear · ctrl+c quit"
	}
	if m.details {
		lines = append(lines, dimStyle.Render("Details"))
		all := m.detailLines()
		start := min(m.offset, max(0, len(all)-m.pageSize()))
		body = all[start:min(len(all), start+m.pageSize())]
		footer = fmt.Sprintf("Lines %d–%d of %d", min(start+1, len(all)), start+len(body), len(all))
		help = "↑/↓ scroll · pgup/pgdown page · esc back · q quit"
	} else {
		lines = append(lines, dimStyle.Render(m.tableRow(status.Item{Name: "APP", Installed: "ON MACHINE", Recorded: "IN CONFIG"})))
		items := m.items()
		start, end := visibleWindow(len(items), m.cursor, m.pageSize())
		for i := start; i < end; i++ {
			line := m.tableRow(items[i])
			if i == m.cursor {
				line = cursorStyle.Render(line)
			}
			body = append(body, line)
		}
		if len(items) == 0 {
			body = append(body, dimStyle.Render("No matching items"))
		}
		footer = fmt.Sprintf("Rows %d–%d of %d · %s", min(start+1, len(items)), end, len(items), section.Action)
	}
	for _, line := range body {
		lines = append(lines, statusClip(line, m.width))
	}
	for i := len(body); i < m.pageSize(); i++ {
		lines = append(lines, "")
	}
	lines = append(lines, dimStyle.Render(statusClip(footer, m.width)), helpStyle.Render(statusClip(help, m.width)))
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	return view
}

func (m *statusBrowser) tableRow(item status.Item) string {
	if m.width < 52 {
		return statusClip(strings.TrimSpace(item.Name+"  "+item.Installed+"  "+item.Recorded), m.width)
	}
	versionWidth := min(24, (m.width-8)/3)
	nameWidth := m.width - 2*versionWidth - 4
	return statusCell(item.Name, nameWidth) + "  " + statusCell(item.Installed, versionWidth) + "  " + statusCell(item.Recorded, versionWidth)
}

func statusCell(text string, width int) string {
	text = statusClip(text, width)
	return text + strings.Repeat(" ", max(0, width-lipgloss.Width(text)))
}

func statusClip(text string, width int) string {
	text = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(text)
	return ansi.Truncate(text, width, "…")
}
