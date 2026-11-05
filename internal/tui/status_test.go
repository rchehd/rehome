package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rchehd/rehome/internal/status"
)

func TestStatusBrowserPagingSearchAndCategories(t *testing.T) {
	s := &status.Status{New: []string{"local-tool"}, ChangedFiles: []string{"files/zsh/.zshrc"}}
	for i := range 120 {
		s.Missing = append(s.Missing, fmt.Sprintf("tool-%03d", i))
	}
	m := newStatusBrowser(s)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.cursor != 119 || !strings.Contains(m.View().Content, "tool-119") {
		t.Fatal("last item is unreachable")
	}
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "tool-119" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.items()) != 1 || m.cursor != 0 {
		t.Fatalf("filter did not reset cursor: %+v", m)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.details || !strings.Contains(m.View().Content, "tool-119") {
		t.Fatal("selected details are unavailable")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.category != 1 || m.filter != "" || m.cursor != 0 || m.details {
		t.Fatal("switching categories left stale selection")
	}
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range ".zshrc" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.items()) != 1 || m.items()[0].Name != "zsh" {
		t.Fatal("file path search did not find its app")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("quit key did not exit")
	}
}

func TestStatusBrowserNoMatchesUnicodeAndResize(t *testing.T) {
	m := newStatusBrowser(&status.Status{Missing: []string{"Жук"}})
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.Update(tea.KeyPressMsg{Code: 'Ж', Text: "Ж"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.filter != "" || !utf8.ValidString(m.filter) {
		t.Fatal("backspace corrupted Unicode search")
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.cursor != 0 || m.details || !strings.Contains(m.View().Content, "No matching items") {
		t.Fatal("empty search results are not handled")
	}
	for _, size := range [][2]int{{80, 24}, {35, 12}, {12, 8}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
		if !view.AltScreen || strings.Count(view.Content, "\n")+1 > size[1] {
			t.Fatalf("view exceeds terminal height: %dx%d", size[0], size[1])
		}
		for _, line := range strings.Split(view.Content, "\n") {
			if lipgloss.Width(line) > size[0] {
				t.Fatalf("view exceeds terminal width: %q", line)
			}
		}
	}
}

func TestStatusBrowserFileDetailsScrollAndWrap(t *testing.T) {
	s := &status.Status{}
	for i := range 200 {
		s.ChangedFiles = append(s.ChangedFiles, fmt.Sprintf("files/editor/some/long/configuration/path/%03d.json", i))
	}
	m := newStatusBrowser(s)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	view := m.View()
	if !m.details || m.offset == 0 || !strings.Contains(view.Content, "199.json") {
		t.Fatal("last changed file is unreachable")
	}
	for _, line := range strings.Split(view.Content, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("detail path was not wrapped: %q", line)
		}
	}
	clean := newStatusBrowser(&status.Status{})
	if !strings.Contains(clean.View().Content, "In sync") {
		t.Fatal("clean state is missing")
	}
}
