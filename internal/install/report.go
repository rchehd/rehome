package install

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/ui"
)

// Print writes the human-readable end-of-run report.
func (r *Report) Print(w io.Writer) {
	out := ui.New(w)

	out.Println()
	out.Println(ui.Bold(fmt.Sprintf("installed: %d, skipped: %d, failed: %d", len(r.Done), len(r.Skipped), len(r.Failed))))

	var items []ui.Item

	for _, o := range r.Failed {
		item := ui.Item{Mark: ui.Fail, Text: o.ID + ": " + o.Reason}

		if o.StderrTail != "" {
			item.Sub = strings.Split(o.StderrTail, "\n")
		}

		items = append(items, item)
	}

	for _, o := range r.Skipped {
		if o.Reason != "" {
			items = append(items, ui.Item{Mark: ui.Drop, Text: o.ID + ": " + o.Reason})
		}
	}

	for _, o := range r.Upgraded {
		items = append(items, ui.Item{Mark: ui.Update, Text: o.ID + ": " + o.Reason})
	}

	out.List(items...)

	if len(r.Upgradable) > 0 {
		out.Section("Older than on the other nodes", "rehome install --upgrade")

		items = nil

		for _, o := range r.Upgradable {
			items = append(items, ui.Item{Mark: ui.Plain, Text: o.ID + ": " + o.Reason})
		}

		out.List(items...)
	}

	items = nil

	for _, n := range r.VersionNotes {
		items = append(items, ui.Item{Mark: ui.Plain, Text: "version: " + n})
	}

	out.List(items...)

	if len(r.KeptLocal) > 0 {
		out.Section("Kept", "settings kept as they are (differ from the config; use --overwrite-files to replace)")

		items = nil

		for _, k := range r.KeptLocal {
			items = append(items, ui.Item{Mark: ui.Plain, Text: k.Dest})
		}

		out.List(items...)
	}

	if len(r.ManualSecrets) > 0 {
		out.Section("Secrets", "fill in secrets by hand")

		items = nil

		for _, s := range r.ManualSecrets {
			items = append(items, ui.Item{Mark: ui.Plain, Text: s})
		}

		out.List(items...)
	}

	r.printSources(out)

	items = nil

	for _, n := range r.Notes {
		items = append(items, ui.Item{Mark: ui.Plain, Text: "note: " + n})
	}

	out.List(items...)
}

// printSources lists the installed apps by the node they came from, then what was left out and why.
func (r *Report) printSources(out *ui.Out) {
	byNode := map[string][]string{}

	for id, from := range r.TakenFrom {
		byNode[from] = append(byNode[from], id)
	}

	if len(byNode) > 0 {
		var rows [][]string

		for _, n := range slices.Sorted(maps.Keys(byNode)) {
			slices.Sort(byNode[n])
			rows = append(rows, []string{n, strings.Join(byNode[n], ", ")})
		}

		out.Section("Taken from", "")
		out.Table(nil, rows)
	}

	if len(r.LeftOut) > 0 {
		var items []ui.Item

		for _, c := range r.LeftOut {
			on := ""

			if c.Node != "" {
				on = " (on " + c.Node + ")"
			}

			items = append(items, ui.Item{Mark: ui.Drop, Text: c.ID + ": " + c.Reason + on})
		}

		out.Section("Left out", "")
		out.List(items...)
	}
}
