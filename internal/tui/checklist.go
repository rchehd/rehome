package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
)

const statusSkipped = "skipped"

var statusLabels = map[string]string{
	install.StatusInstall:     "missing",
	install.StatusFilesOnly:   "settings",
	install.StatusInstalled:   "installed",
	install.StatusUnavailable: "n/a",
	install.StatusDisabled:    "disabled",
	statusSkipped:             "skipped",
}

type tab struct {
	name  string
	match func(*install.Plan, install.Step) bool
}

var tabs = []tab{
	{"all", func(*install.Plan, install.Step) bool { return true }},
	{"missing here", func(_ *install.Plan, s install.Step) bool { return s.Status != install.StatusInstalled }},
	{"can install", func(_ *install.Plan, s install.Step) bool {
		return s.Status == install.StatusInstall || s.Status == install.StatusFilesOnly
	}},
	{"installed", func(_ *install.Plan, s install.Step) bool { return s.Status == install.StatusInstalled }},
	{"no recipe", func(_ *install.Plan, s install.Step) bool {
		return s.Code == install.ReasonNoRecipe || (s.Status == statusSkipped && strings.HasPrefix(s.Reason, "no recipe"))
	}},
	{"conflicts", func(p *install.Plan, s install.Step) bool { return len(p.Conflicts[s.App.ID]) > 1 }},
}

type item struct {
	step    install.Step
	checked bool
	// latest overrides the source node's version for this app; upgrade raises it to that version.
	latest     bool
	upgrade    bool
	noSettings bool
}

type row struct {
	group string
	item  *item
}

type checklist struct {
	plan      *install.Plan
	opts      ChecklistOptions
	tab       int
	back      bool
	switchID  string
	items     []*item
	groups    []string
	cursor    int
	filter    string
	filtering bool
	height    int
	done      bool
	cancelled bool
	recipe    string
}

// ChecklistOptions name the node the plan comes from; Back lets esc return to the node list.
// Prior restores an earlier choice and its filter tab, with the cursor on Focus.
type ChecklistOptions struct {
	Source string
	Back   bool
	Prior  *ChecklistResult
	Focus  string
}

// ChecklistResult is the user's choice: the apps to install and upgrade, an app to write a recipe
// for first, an app to take from its next node (Switch), or Back to the node list.
type ChecklistResult struct {
	Selection install.Selection
	Upgrades  []string
	AddRecipe string
	Switch    string
	Back      bool
	Tab       int
}

// Checklist lets the user pick what to install; nothing starts checked. ok is false when cancelled.
func Checklist(plan *install.Plan, opts ChecklistOptions) (ChecklistResult, bool, error) {
	m := &checklist{plan: plan, opts: opts, height: 20}
	seen := map[string]bool{}

	for _, s := range plan.Steps {
		if s.Status == install.StatusIgnored {
			continue
		}

		it := &item{step: s}

		if p := opts.Prior; p != nil {
			id := s.App.ID
			it.checked, it.latest = slices.Contains(p.Selection.IDs, id), slices.Contains(p.Selection.Latest, id)
			it.upgrade, it.noSettings = slices.Contains(p.Upgrades, id), slices.Contains(p.Selection.NoSettings, id)
		}

		m.items = append(m.items, it)

		m.addGroup(groupOf(s), seen)
	}

	for _, c := range plan.Skipped {
		s := install.Step{App: config.App{ID: c.ID}, Status: statusSkipped, Reason: c.Reason}
		m.items = append(m.items, &item{step: s})
		m.addGroup(groupOf(s), seen)
	}

	if opts.Prior != nil {
		m.tab = opts.Prior.Tab
	}

	for i, r := range m.rows() {
		if r.item != nil && r.item.step.App.ID == opts.Focus {
			m.cursor = i
		}
	}

	final, err := tea.NewProgram(m).Run()

	if err != nil {
		return ChecklistResult{}, false, err
	}

	m = final.(*checklist)

	if m.cancelled {
		return ChecklistResult{}, false, nil
	}

	if m.back {
		return ChecklistResult{Back: true}, true, nil
	}

	if m.recipe != "" {
		return ChecklistResult{AddRecipe: m.recipe}, true, nil
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

		if it.noSettings {
			sel.NoSettings = append(sel.NoSettings, it.step.App.ID)
		}
	}

	return ChecklistResult{Selection: sel, Upgrades: upgrades, Switch: m.switchID, Tab: m.tab}, true, nil
}

func (m *checklist) addGroup(g string, seen map[string]bool) {
	if !seen[g] {
		seen[g] = true
		m.groups = append(m.groups, g)
	}
}

func (m *checklist) Init() tea.Cmd { return nil }

func (m *checklist) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = max(5, msg.Height-9)
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
	case "ctrl+c", "q":
		m.cancelled = true

		return tea.Quit
	case "esc":
		if m.filter != "" {
			m.filter, m.cursor = "", 0

			return nil
		}

		m.back, m.cancelled = m.opts.Back, !m.opts.Back

		return tea.Quit
	case "tab", "shift+tab":
		step := 1

		if key == "shift+tab" {
			step = len(tabs) - 1
		}

		m.tab, m.cursor = (m.tab+step)%len(tabs), 0
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
	case "A":
		m.toggleMissing()
	case "v":
		if it := m.current(rows); it != nil && it.step.Pin != (lock.Entry{}) {
			it.latest = !it.latest
		}
	case "n":
		if it := m.current(rows); it != nil && len(m.plan.Conflicts[it.step.App.ID]) > 1 {
			m.switchID = it.step.App.ID

			return tea.Quit
		}
	case "s":
		if it := m.current(rows); it != nil && m.hasSettings(it) {
			it.noSettings = !it.noSettings
		}
	case "u":
		if it := m.current(rows); it != nil && it.step.Upgrade != (lock.Entry{}) {
			it.upgrade = !it.upgrade
		}
	case "r":
		if it := m.current(rows); it != nil && it.step.Code == install.ReasonNoRecipe {
			m.recipe = it.step.App.ID

			return tea.Quit
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

// toggleMissing checks every visible app that is missing here, or unchecks them when all are checked.
func (m *checklist) toggleMissing() {
	var missing []*item

	for _, it := range m.items {
		if m.matches(it) && (it.step.Status == install.StatusInstall || it.step.Status == install.StatusFilesOnly) {
			missing = append(missing, it)
		}
	}

	check := false

	for _, it := range missing {
		check = check || !it.checked
	}

	for _, it := range missing {
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
	return tabs[m.tab].match(m.plan, it.step) &&
		(m.filter == "" || strings.Contains(it.step.App.ID, strings.ToLower(m.filter)))
}

// hasSettings reports whether the app brings settings files that can be left out.
func (m *checklist) hasSettings(it *item) bool {
	s := it.step

	return s.App.Files != "" && len(m.plan.Config.Files[s.App.Files]) > 0 &&
		s.Status != install.StatusFilesOnly && selectable(it)
}

// note explains the current app: why it cannot be installed, which version and whose settings it gets.
func (m *checklist) note(it *item) string {
	s := it.step

	if s.Status == install.StatusUnavailable || s.Status == statusSkipped || s.Status == install.StatusDisabled {
		return s.Reason
	}

	var parts []string

	if from := m.plan.From[s.App.ID]; from != "" {
		parts = append(parts, "from "+from)
	}

	if others := slices.DeleteFunc(slices.Clone(m.plan.Conflicts[s.App.ID]), func(n string) bool { return n == m.plan.From[s.App.ID] }); len(others) > 0 {
		parts = append(parts, "also on "+strings.Join(others, ", ")+" (n to switch)")
	}

	switch {
	case s.Pin != (lock.Entry{}) && it.latest:
		parts = append(parts, "latest instead of @"+pinText(s.Pin))
	case s.Pin != (lock.Entry{}):
		parts = append(parts, "same version @"+pinText(s.Pin)+" (v for latest)")
	case s.PinNote != "":
		parts = append(parts, "latest: "+s.PinNote)
	}

	switch {
	case s.Status == install.StatusFilesOnly:
		parts = append(parts, "settings only, from "+filepath.Base(m.plan.FilesRoot(s.App.Files)))
	case !m.hasSettings(it):
		parts = append(parts, "no settings recorded")
	case it.noSettings:
		parts = append(parts, "without settings (s to include)")
	default:
		parts = append(parts, "settings from "+filepath.Base(m.plan.FilesRoot(s.App.Files))+" (s to leave out)")
	}

	return strings.Join(parts, " · ")
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

	source := "all nodes"

	if m.opts.Source != "" {
		source = "from " + m.opts.Source
	}

	b.WriteString(titleStyle.Render(fmt.Sprintf("rehome install — %s — %d selected", source, selected)) + "\n")
	b.WriteString(m.tabBar() + "\n")

	if m.filtering || m.filter != "" {
		b.WriteString("filter: " + m.filter + "▏\n")
	}

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

	note := ""

	if it := m.current(rows); it != nil {
		note = m.note(it)
	}

	back := "q quit"

	if m.opts.Back {
		back = "esc nodes · q quit"
	}

	b.WriteString(dimStyle.Render(note) + "\n")
	b.WriteString(helpStyle.Render("↑/↓ move · space toggle · a group · A all missing · tab filter · / search · enter continue · "+back) + "\n")
	b.WriteString(helpStyle.Render("v same/latest · s settings · n other node · u upgrade · r add recipe"))
	v := tea.NewView(b.String())
	v.AltScreen = true

	return v
}

func (m *checklist) tabBar() string {
	var parts []string

	for i, t := range tabs {
		n := 0

		for _, it := range m.items {
			if t.match(m.plan, it.step) {
				n++
			}
		}

		label := fmt.Sprintf("%s (%d)", t.name, n)

		if i == m.tab {
			label = cursorStyle.Render(" " + label + " ")
		} else {
			label = dimStyle.Render(" " + label + " ")
		}

		parts = append(parts, label)
	}

	return strings.Join(parts, " ")
}

func (m *checklist) renderRow(r row) string {
	if r.item == nil {
		return groupStyle.Render("▸ " + r.group)
	}

	box := "[ ]"

	if r.item.checked {
		box = "[x]"
	}

	if !selectable(r.item) {
		box = "   "
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

	if r.item.noSettings {
		detail += " · no settings"
	}

	if len(m.plan.Conflicts[s.App.ID]) > 1 {
		detail = "⇄ " + m.plan.From[s.App.ID] + " · " + detail
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
	return !slices.Contains([]string{install.StatusUnavailable, statusSkipped}, it.step.Status)
}

func groupOf(s install.Step) string {
	if s.App.Group == "" {
		return "other"
	}

	return s.App.Group
}
