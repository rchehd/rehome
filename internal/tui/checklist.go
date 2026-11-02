package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
)

var statusLabels = map[string]string{
	install.StatusInstall:     "missing",
	install.StatusFilesOnly:   "settings",
	install.StatusInstalled:   "installed",
	install.StatusUnavailable: "n/a",
	install.StatusDisabled:    "disabled",
}

type item struct {
	step    install.Step
	checked bool
	// latest overrides a lock pin for this app; upgrade raises it to the lock version.
	latest  bool
	upgrade bool
}

type row struct {
	group string
	item  *item
}

type checklist struct {
	items     []*item
	groups    []string
	cursor    int
	filter    string
	filtering bool
	height    int
	done      bool
	cancelled bool
}

// Checklist lets the user pick what to install; missing apps start checked. It returns the
// selection and the ids to upgrade to their lock version.
func Checklist(plan *install.Plan) (install.Selection, []string, bool, error) {
	m := &checklist{height: 20}
	seen := map[string]bool{}

	for _, s := range plan.Steps {
		if s.Status == install.StatusIgnored {
			continue
		}

		// Installed apps with settings start checked so new files from the config still arrive.
		checked := s.Status == install.StatusInstall || s.Status == install.StatusFilesOnly ||
			(s.Status == install.StatusInstalled && s.App.Files != "")
		it := &item{step: s, checked: checked}
		m.items = append(m.items, it)

		if g := groupOf(s); !seen[g] {
			seen[g] = true
			m.groups = append(m.groups, g)
		}
	}

	final, err := tea.NewProgram(m).Run()

	if err != nil {
		return install.Selection{}, nil, false, err
	}

	m = final.(*checklist)

	if m.cancelled {
		return install.Selection{}, nil, false, nil
	}

	sel := install.Selection{IDs: []string{}, Mode: plan.Mode}
	var upgrades []string

	for _, it := range m.items {
		if it.checked {
			sel.IDs = append(sel.IDs, it.step.App.ID)
		}

		if it.latest {
			sel.Latest = append(sel.Latest, it.step.App.ID)
		}

		if it.upgrade {
			upgrades = append(upgrades, it.step.App.ID)
		}
	}

	return sel, upgrades, true, nil
}

func (m *checklist) Init() tea.Cmd { return nil }

func (m *checklist) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = max(5, msg.Height-6)
	case tea.KeyPressMsg:
		if m.filtering {
			m.updateFilter(msg.String())

			return m, nil
		}

		return m, m.updateKey(msg.String())
	}

	return m, nil
}

func (m *checklist) updateKey(key string) tea.Cmd {
	rows := m.rows()

	switch key {
	case "ctrl+c", "q", "esc":
		m.cancelled = true

		return tea.Quit
	case "enter":
		m.done = true

		return tea.Quit
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(rows)-1, m.cursor+1)
	case "pgup":
		m.cursor = max(0, m.cursor-m.height)
	case "pgdown":
		m.cursor = min(len(rows)-1, m.cursor+m.height)
	case "/":
		m.filtering = true
	case "space", " ":
		if m.cursor < len(rows) {
			if it := rows[m.cursor].item; it != nil {
				toggle(it)
			} else {
				m.toggleGroup(rows[m.cursor].group)
			}
		}
	case "a":
		if m.cursor < len(rows) {
			m.toggleGroup(rows[m.cursor].group)
		}
	case "v":
		if it := m.current(rows); it != nil && it.step.Pin != (lock.Entry{}) {
			it.latest = !it.latest
		}
	case "u":
		if it := m.current(rows); it != nil && it.step.Upgrade != (lock.Entry{}) {
			it.upgrade = !it.upgrade
		}
	}

	return nil
}

func (m *checklist) updateFilter(key string) {
	switch key {
	case "esc":
		m.filter, m.filtering = "", false
	case "enter":
		m.filtering = false
	case "backspace":
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
		}
	default:
		if len(key) == 1 {
			m.filter += key
		}
	}

	m.cursor = 0
}

func (m *checklist) current(rows []row) *item {
	if m.cursor < len(rows) {
		return rows[m.cursor].item
	}

	return nil
}

func (m *checklist) toggleGroup(group string) {
	var members []*item

	for _, it := range m.items {
		if groupOf(it.step) == group && m.matches(it) && selectable(it) {
			members = append(members, it)
		}
	}

	check := false

	for _, it := range members {
		check = check || !it.checked
	}

	for _, it := range members {
		it.checked = check
	}
}

func (m *checklist) rows() []row {
	var rows []row

	for _, g := range m.groups {
		var members []row

		for _, it := range m.items {
			if groupOf(it.step) == g && m.matches(it) {
				members = append(members, row{group: g, item: it})
			}
		}

		if len(members) > 0 {
			rows = append(rows, row{group: g})
			rows = append(rows, members...)
		}
	}

	return rows
}

func (m *checklist) matches(it *item) bool {
	return m.filter == "" || strings.Contains(it.step.App.ID, strings.ToLower(m.filter))
}

func (m *checklist) View() tea.View {
	var b strings.Builder
	rows := m.rows()
	selected := 0

	for _, it := range m.items {
		if it.checked {
			selected++
		}
	}

	b.WriteString(titleStyle.Render(fmt.Sprintf("rehome install — %d selected", selected)) + "\n")

	if m.filtering || m.filter != "" {
		b.WriteString("filter: " + m.filter + "▏\n")
	}

	start, end := visibleWindow(len(rows), m.cursor, m.height)

	for i := start; i < end; i++ {
		line := renderRow(rows[i])

		if i == m.cursor {
			line = cursorStyle.Render(line)
		}

		b.WriteString(line + "\n")
	}

	b.WriteString(helpStyle.Render("↑/↓ move · space toggle · a group · v lock/latest · u upgrade · / filter · enter continue · q quit"))
	v := tea.NewView(b.String())
	v.AltScreen = true

	return v
}

func renderRow(r row) string {
	if r.item == nil {
		return groupStyle.Render("▸ " + r.group)
	}

	box := "[ ]"

	if r.item.checked {
		box = "[x]"
	}

	s := r.item.step
	label := statusLabels[s.Status]
	detail := s.Reason

	if name, pkg := s.Spec.Manager(); name != "" {
		detail = name + " " + pkg
	}

	if s.Mapped != "" {
		detail += " (auto)"
	}

	if len(detail) > 60 {
		detail = detail[:57] + "…"
	}

	return fmt.Sprintf("  %s %-32s %s %-14s %s", box, s.App.ID, statusStyles[label].Render(fmt.Sprintf("%-9s", label)), versionLabel(r.item), dimStyle.Render(detail))
}

// versionLabel shows what will be installed: @pin, latest, or ↑ an available upgrade.
func versionLabel(it *item) string {
	s := it.step

	switch {
	case s.Upgrade != (lock.Entry{}) && it.upgrade:
		return "↑" + s.Upgrade.Version
	case s.Upgrade != (lock.Entry{}):
		return s.Installed + " (↑)"
	case s.Pin != (lock.Entry{}) && !it.latest:
		return "@" + pinText(s.Pin)
	case s.Status == install.StatusInstall:
		return "latest"
	}

	return s.Installed
}

func pinText(e lock.Entry) string {
	if e.Commit != "" {
		return e.Commit[:min(8, len(e.Commit))]
	}

	return e.Version
}

func toggle(it *item) {
	if selectable(it) {
		it.checked = !it.checked
	}
}

func selectable(it *item) bool {
	return it.step.Status != install.StatusUnavailable
}

func groupOf(s install.Step) string {
	if s.App.Group == "" {
		return "other"
	}

	return s.App.Group
}
