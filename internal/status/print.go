package status

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/rchehd/rehome/internal/gitsync"
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

func (s *Status) printHeader(w io.Writer) {
	fmt.Fprintln(w, "rehome status")
	fmt.Fprintf(w, "  Config source  %s\n", s.SourceLabel())
	fmt.Fprintf(w, "  This node      %s\n", s.NodeLabel())
	state := "changes to review"
	if len(s.Missing)+len(s.New)+len(s.ChangedFiles)+len(s.Behind)+len(s.NotSynced)+len(s.Gone) == 0 {
		state = "in sync with the local config"
	}
	fmt.Fprintf(w, "  System         %s\n\n", state)
}

func (s *Status) Print(w io.Writer) {
	s.printHeader(w)
	sections := s.Sections()
	table := tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
	fmt.Fprintln(table, "AREA\tCOUNT\tNEXT STEP")
	for _, section := range sections {
		action := section.Action
		if section.Count == 0 {
			action = "—"
		}
		count := fmt.Sprint(section.Count)
		if section.Key == "files" {
			count += " files"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\n", section.Title, count, action)
	}
	table.Flush()

	count := len(s.Behind) + len(s.NotSynced)
	if count > 0 {
		fmt.Fprintf(w, "\nVersion differences (%d)\n", count)
		table = tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
		fmt.Fprintln(table, "APP\tON MACHINE\tIN CONFIG\tNEXT STEP")
		shown := 0
		for _, section := range sections {
			if section.Key != "behind" && section.Key != "versions" {
				continue
			}
			for _, item := range section.Items {
				if shown >= 4 {
					break
				}
				fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", item.Name, item.Installed, item.Recorded, section.Action)
				shown++
			}
		}
		table.Flush()
		if shown < count {
			fmt.Fprintf(w, "  … %d more version differences\n", count-shown)
		}
	}
	s.printActions(w)
	fmt.Fprintln(w, "\nBrowse: rehome status --interactive  ·  Full list: --details  ·  Structured data: --json")
}

func (s *Status) PrintDetails(w io.Writer) {
	s.printHeader(w)
	for _, section := range s.Sections() {
		if section.Count == 0 {
			continue
		}
		fmt.Fprintf(w, "%s (%d) — %s\n", section.Title, section.Count, section.Action)
		table := tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
		fmt.Fprintln(table, "  APP\tON MACHINE\tIN CONFIG")
		for _, item := range section.Items {
			fmt.Fprintf(table, "  %s\t%s\t%s\n", item.Name, item.Installed, item.Recorded)
			for _, detail := range item.Detail {
				if detail != "" {
					fmt.Fprintf(table, "    %s\n", detail)
				}
			}
		}
		table.Flush()
		fmt.Fprintln(w)
	}
	s.printActions(w)
}

func (s *Status) printActions(w io.Writer) {
	localChanges := len(s.New)+len(s.NotSynced)+len(s.Gone) > 0
	savedSessions := false
	for _, path := range s.ChangedFiles {
		if strings.HasPrefix(path, "session missing: ") {
			savedSessions = true
		} else {
			localChanges = true
		}
	}
	if savedSessions {
		fmt.Fprintln(w, "\nApply saved sessions here: rehome sync --sessions.")
	}
	if localChanges {
		fmt.Fprintln(w, "\nSave this machine's changes: rehome scan → review → rehome push (linked repo).")
		fmt.Fprintln(w, "Apply shared settings here: rehome sync; review file conflicts when prompted.")
	}
}
