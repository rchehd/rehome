package status

import (
	"fmt"
	"io"
	"strings"

	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/ui"
)

func (s *Status) SourceLabel() string {
	switch s.Source {
	case gitsync.SourceCurrent:
		return "up to date"
	case gitsync.SourceAhead:
		return "local commits to publish (rehome push)"
	case gitsync.SourceDirty:
		return "local config edits to publish (rehome push)"
	case gitsync.SourceChanged:
		return "updates available (rehome sync)"
	case gitsync.SourceLocal:
		return "folder, written in place"
	case gitsync.SourceUnknown:
		return "could not check the config source"
	default:
		return "local comparison"
	}
}

// NodeLabel says whether this machine's node matches the system and the shared config.
func (s *Status) NodeLabel() string {
	var parts []string
	switch {
	case s.Node.Unscanned && s.ScanChanges() == 0:
		parts = append(parts, "never scanned (rehome scan)")
	case s.Node.Unscanned:
		parts = append(parts, fmt.Sprintf("not scanned: %d differences from the system (rehome scan)", s.ScanChanges()))
	}
	if s.Node.Unpublished {
		parts = append(parts, "unpublished node changes (rehome push)")
	}
	if len(parts) == 0 {
		parts = append(parts, "up to date")
	}
	return s.Node.Name + ": " + strings.Join(parts, " · ")
}

func (s *Status) printHeader(out *ui.Out) {
	state := "changes to review"

	if len(s.Missing)+len(s.New)+len(s.ChangedFiles)+len(s.Behind)+len(s.NotSynced)+len(s.Gone) == 0 {
		state = "in sync with the local config"
	}

	out.Header("rehome status")
	out.Table(nil, [][]string{
		{"Config source", s.SourceLabel()},
		{"This node", s.NodeLabel()},
		{"System", state},
	})
	out.Println()
}

func (s *Status) Print(w io.Writer) {
	out := ui.New(w)
	s.printHeader(out)

	sections := s.Sections()
	var rows [][]string

	for _, section := range sections {
		action := section.Action

		if section.Count == 0 {
			action = "—"
		}

		count := fmt.Sprint(section.Count)

		if section.Key == "files" {
			count += " files"
		}

		rows = append(rows, []string{section.Title, count, action})
	}

	out.Table([]string{"AREA", "COUNT", "NEXT STEP"}, rows)

	count := len(s.Behind) + len(s.NotSynced)

	if count > 0 {
		out.Section(fmt.Sprintf("Version differences (%d)", count), "")

		rows = nil

		for _, section := range sections {
			if section.Key != "behind" && section.Key != "versions" {
				continue
			}

			for _, item := range section.Items {
				if len(rows) >= 4 {
					break
				}

				rows = append(rows, []string{item.Name, item.Installed, item.Recorded, section.Action})
			}
		}

		out.Table([]string{"APP", "ON MACHINE", "IN CONFIG", "NEXT STEP"}, rows)

		if len(rows) < count {
			out.List(ui.Item{Mark: ui.Plain, Text: fmt.Sprintf("… %d more version differences", count-len(rows))})
		}
	}

	s.printActions(out)
	out.Println()
	out.Println(ui.Muted("Browse: rehome status --interactive  ·  Full list: --details  ·  Structured data: --json"))
}

func (s *Status) PrintDetails(w io.Writer) {
	out := ui.New(w)
	s.printHeader(out)

	for _, section := range s.Sections() {
		if section.Count == 0 {
			continue
		}

		var rows [][]string

		for _, item := range section.Items {
			rows = append(rows, []string{item.Name, item.Installed, item.Recorded})

			for _, detail := range item.Detail {
				if detail != "" {
					rows = append(rows, []string{ui.Muted("  " + detail), "", ""})
				}
			}
		}

		out.Section(fmt.Sprintf("%s (%d)", section.Title, section.Count), section.Action)
		out.Table([]string{"APP", "ON MACHINE", "IN CONFIG"}, rows)
	}

	s.printActions(out)
}

func (s *Status) printActions(out *ui.Out) {
	localChanges := len(s.New)+len(s.NotSynced)+len(s.Gone) > 0
	savedSessions := false

	for _, path := range s.ChangedFiles {
		if strings.HasPrefix(path, "session missing: ") {
			savedSessions = true
		} else {
			localChanges = true
		}
	}

	var items []ui.Item

	if savedSessions {
		items = append(items, ui.Item{Mark: ui.Plain, Text: "Apply saved sessions here: rehome sync --sessions."})
	}

	if localChanges {
		items = append(items,
			ui.Item{Mark: ui.Plain, Text: "Save this machine's changes: rehome scan → review → rehome push (linked repo)."},
			ui.Item{Mark: ui.Plain, Text: "Apply shared settings here: rehome sync; review file conflicts when prompted."})
	}

	if len(items) > 0 {
		out.Section("Next steps", "")
		out.List(items...)
	}
}
