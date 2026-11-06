package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ReviewFile is a settings file scan would write: its app, system path, redacted keys and diff.
type ReviewFile struct {
	App     string
	Path    string
	New     bool
	Secrets []string
	Diff    string
}

type reviewTab struct {
	name  string
	match func(ReviewFile) bool
}

var reviewTabs = []reviewTab{
	{"all", func(ReviewFile) bool { return true }},
	{"new", func(f ReviewFile) bool { return f.New }},
	{"changed", func(f ReviewFile) bool { return !f.New }},
	{"secrets", func(f ReviewFile) bool { return len(f.Secrets) > 0 }},
}

type reviewRow struct {
	app  string
	file int
}

type review struct {
	files     []ReviewFile
	keep      []bool
	apps      []string
	tab       int
	cursor    int
	height    int
	filter    string
	filtering bool
	detail    []string
	offset    int
	done      bool
}

// Review lets the user leave apps or single files out of a scan; ok is false when cancelled.
func Review(files []ReviewFile) ([]bool, bool, error) {
	m := &review{files: files, keep: make([]bool, len(files)), height: 20}

	for i, f := range files {
		m.keep[i] = true

		if !slices.Contains(m.apps, f.App) {
			m.apps = append(m.apps, f.App)
		}
	}

	slices.Sort(m.apps)
	final, err := tea.NewProgram(m).Run()

	if err != nil {
		return nil, false, err
	}

	m = final.(*review)

	return m.keep, m.done, nil
}

func (m *review) Init() tea.Cmd { return nil }

func (m *review) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = max(5, msg.Height-7)
	case tea.KeyPressMsg:
		key := msg.String()

		switch {
		case key == "ctrl+c":
			return m, tea.Quit
		case m.detail != nil:
			m.updateDetail(key)
		case m.filtering:
			m.updateFilter(key)
		default:
			return m, m.updateKey(key)
		}
	}

	return m, nil
}

func (m *review) updateKey(key string) tea.Cmd {
	rows := m.rows()

	switch key {
	case "q", "esc":
		if key == "esc" && m.filter != "" {
			m.filter, m.cursor = "", 0

			return nil
		}

		return tea.Quit
	case "enter":
		m.done = true

		return tea.Quit
	case "tab", "shift+tab":
		step := 1

		if key == "shift+tab" {
			step = len(reviewTabs) - 1
		}

		m.tab, m.cursor = (m.tab+step)%len(reviewTabs), 0
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
			if r := rows[m.cursor]; r.file >= 0 {
				m.keep[r.file] = !m.keep[r.file]
			} else {
				m.toggleApp(r.app)
			}
		}
	case "a":
		if m.cursor < len(rows) {
			m.toggleApp(rows[m.cursor].app)
		}
	case "d":
		if m.cursor < len(rows) && rows[m.cursor].file >= 0 {
			m.detail, m.offset = strings.Split(strings.TrimRight(m.files[rows[m.cursor].file].Diff, "\n"), "\n"), 0
		}
	}

	return nil
}

func (m *review) updateDetail(key string) {
	maxOffset := max(0, len(m.detail)-m.height)

	switch key {
	case "esc", "q", "d":
		m.detail = nil
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

func (m *review) updateFilter(key string) {
	switch key {
	case "esc":
		m.filter, m.filtering = "", false
	case "enter":
		m.filtering = false
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	default:
		if len([]rune(key)) == 1 {
			m.filter += key
		}
	}

	m.cursor = 0
}

func (m *review) matches(f ReviewFile) bool {
	return reviewTabs[m.tab].match(f) &&
		(m.filter == "" || strings.Contains(strings.ToLower(f.Path), strings.ToLower(m.filter)))
}

// toggleApp keeps every visible file of app, or leaves them all out when all are kept.
func (m *review) toggleApp(app string) {
	var members []int

	for i, f := range m.files {
		if f.App == app && m.matches(f) {
			members = append(members, i)
		}
	}

	keep := false

	for _, i := range members {
		keep = keep || !m.keep[i]
	}

	for _, i := range members {
		m.keep[i] = keep
	}
}

func (m *review) rows() []reviewRow {
	var rows []reviewRow

	for _, app := range m.apps {
		var members []reviewRow

		for i, f := range m.files {
			if f.App == app && m.matches(f) {
				members = append(members, reviewRow{app: app, file: i})
			}
		}

		if len(members) > 0 {
			rows = append(rows, reviewRow{app: app, file: -1})
			rows = append(rows, members...)
		}
	}

	return rows
}

func (m *review) View() tea.View {
	var b strings.Builder
	kept := 0

	for _, k := range m.keep {
		if k {
			kept++
		}
	}

	b.WriteString(titleStyle.Render(fmt.Sprintf("rehome scan — %d of %d settings files will be written", kept, len(m.files))) + "\n")

	if m.detail != nil {
		end := min(len(m.detail), m.offset+m.height)
		b.WriteString(strings.Join(m.detail[m.offset:end], "\n") + "\n")
		b.WriteString(helpStyle.Render("↑/↓ scroll · esc back"))

		return altView(b.String())
	}

	b.WriteString(m.tabBar() + "\n")

	if m.filtering || m.filter != "" {
		b.WriteString("filter: " + m.filter + "▏\n")
	}

	rows := m.rows()
	start, end := visibleWindow(len(rows), m.cursor, m.height)

	for i := start; i < end; i++ {
		line := m.renderRow(rows[i])

		if i == m.cursor {
			line = cursorStyle.Render(line)
		}

		b.WriteString(line + "\n")
	}

	if len(rows) == 0 {
		b.WriteString(dimStyle.Render("  nothing here") + "\n")
	}

	if m.filtering {
		b.WriteString(helpStyle.Render("type to search · enter apply · esc clear · ctrl+c quit"))
	} else {
		b.WriteString(helpStyle.Render("↑/↓ move · space toggle · a app · d diff · tab filter · / search · enter write · q quit"))
	}

	return altView(b.String())
}

func (m *review) tabBar() string {
	var parts []string

	for i, t := range reviewTabs {
		n := 0

		for _, f := range m.files {
			if t.match(f) {
				n++
			}
		}

		label := " " + fmt.Sprintf("%s (%d)", t.name, n) + " "

		if i == m.tab {
			parts = append(parts, cursorStyle.Render(label))
		} else {
			parts = append(parts, dimStyle.Render(label))
		}
	}

	return strings.Join(parts, " ")
}

func (m *review) renderRow(r reviewRow) string {
	if r.file < 0 {
		kept, total := 0, 0

		for i, f := range m.files {
			if f.App == r.app && m.matches(f) {
				total++

				if m.keep[i] {
					kept++
				}
			}
		}

		return groupStyle.Render(fmt.Sprintf("▸ %s", r.app)) + dimStyle.Render(fmt.Sprintf("  %d/%d", kept, total))
	}

	f := m.files[r.file]
	box, state := "[ ]", "changed"

	if m.keep[r.file] {
		box = "[x]"
	}

	if f.New {
		state = "new"
	}

	detail := ""

	if len(f.Secrets) > 0 {
		detail = " · redacted: " + strings.Join(f.Secrets, ", ")
	}

	return fmt.Sprintf("  %s %-7s %s%s", box, state, f.Path, dimStyle.Render(detail))
}

func altView(s string) tea.View {
	v := tea.NewView(s)
	v.AltScreen = true

	return v
}
