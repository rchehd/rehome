package status

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

type Item struct {
	Name, Installed, Recorded string
	Detail                    []string
}

type Section struct {
	Key, Title, Action string
	Count              int
	Items              []Item
}

func (s *Status) Sections() []Section {
	sections := []Section{
		{Key: "missing", Title: "Missing apps", Action: "rehome install", Count: len(s.Missing)},
		{Key: "behind", Title: "Older versions", Action: "rehome install --upgrade", Count: len(s.Behind)},
		{Key: "files", Title: "Changed files", Action: "review; scan to save / sync to apply", Count: len(s.ChangedFiles)},
		{Key: "new", Title: "Local apps", Action: "rehome scan", Count: len(s.New)},
		{Key: "versions", Title: "Local versions", Action: "rehome scan", Count: len(s.NotSynced)},
		{Key: "gone", Title: "Gone from this node", Action: "rehome scan", Count: len(s.Gone)},
		{Key: "attention", Title: "Needs attention", Action: "review details", Count: len(s.Attention)},
		{Key: "unavailable", Title: "Unavailable", Action: "no installer for this OS", Count: len(s.Unsupported)},
	}
	for _, id := range s.Gone {
		sections[5].Items = append(sections[5].Items, Item{Name: id, Installed: "not found", Recorded: versionText(s.Versions[id].Recorded, "recorded"), Detail: []string{"This node still records it; rehome scan drops it, rehome install " + id + " brings it back."}})
	}
	for _, id := range s.Missing {
		v := s.Versions[id]
		recorded := versionText(v.Recorded, "latest")
		if v.Pinned != "" {
			recorded = v.Pinned + " (pin)"
		}
		sections[0].Items = append(sections[0].Items, Item{Name: id, Installed: "missing", Recorded: recorded, Detail: []string{s.MissingReasons[id]}})
	}
	for _, text := range s.Behind {
		id, rest, _ := strings.Cut(text, " ")
		v := s.Versions[id]
		installed, recorded, _ := strings.Cut(rest, " → ")
		installed = versionText(v.Installed, installed)
		recorded = versionText(v.Recorded, recorded)
		if v.Pinned != "" {
			recorded = v.Pinned + " (pin)"
		}
		sections[1].Items = append(sections[1].Items, Item{Name: id, Installed: installed, Recorded: recorded, Detail: []string{"Upgrade to the configured version with rehome install --upgrade."}})
	}
	for _, id := range s.New {
		sections[3].Items = append(sections[3].Items, Item{Name: id, Installed: versionText(s.Versions[id].Installed, "detected"), Recorded: "not recorded"})
	}
	for _, id := range s.NotSynced {
		v := s.Versions[id]
		detail := []string{"rehome scan records this machine's version in its node; review, then rehome push for a linked repo."}
		if v.Installed.Manager != v.Recorded.Manager && v.Recorded.Manager != "" {
			detail = append(detail, "Install method changed: "+v.Recorded.Manager+" → "+v.Installed.Manager)
		}
		if v.Pinned != "" {
			detail = append(detail, "Explicit version pin: "+v.Pinned)
		}
		sections[4].Items = append(sections[4].Items, Item{Name: id, Installed: versionText(v.Installed, "unknown"), Recorded: versionText(v.Recorded, "not recorded"), Detail: detail})
	}
	groups := map[string][]string{}
	for _, path := range s.ChangedFiles {
		groups[fileGroup(path)] = append(groups[fileGroup(path)], path)
	}
	for _, group := range slices.Sorted(maps.Keys(groups)) {
		paths := slices.Clone(groups[group])
		slices.Sort(paths)
		sections[2].Items = append(sections[2].Items, Item{Name: group, Installed: fmt.Sprintf("%d files", len(paths)), Detail: paths})
	}
	for _, warning := range s.Attention {
		name := warning.ID
		if name == "" {
			name = warning.Path
		}
		sections[6].Items = append(sections[6].Items, Item{Name: name, Detail: []string{warning.Path, warning.Reason, warning.Action}})
	}
	for _, id := range s.Unsupported {
		sections[7].Items = append(sections[7].Items, Item{Name: id, Installed: "unavailable"})
	}
	return sections
}

func fileGroup(path string) string {
	if strings.HasPrefix(path, "session missing: ") {
		return "sessions to restore (--sessions)"
	}
	if parts := strings.SplitN(path, "/", 3); len(parts) >= 2 && parts[0] == "files" {
		return parts[1]
	}
	return "other files"
}
