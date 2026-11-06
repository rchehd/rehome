package install

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// Print writes the human-readable end-of-run report.
func (r *Report) Print(w io.Writer) {
	fmt.Fprintf(w, "\ninstalled: %d, skipped: %d, failed: %d\n", len(r.Done), len(r.Skipped), len(r.Failed))

	for _, o := range r.Failed {
		fmt.Fprintf(w, "  ✗ %s: %s\n", o.ID, o.Reason)

		if o.StderrTail != "" {
			fmt.Fprintf(w, "      %s\n", strings.ReplaceAll(o.StderrTail, "\n", "\n      "))
		}
	}

	for _, o := range r.Skipped {
		if o.Reason != "" {
			fmt.Fprintf(w, "  - %s: %s\n", o.ID, o.Reason)
		}
	}

	for _, o := range r.Upgraded {
		fmt.Fprintf(w, "  ↑ %s: %s\n", o.ID, o.Reason)
	}

	if len(r.Upgradable) > 0 {
		fmt.Fprintln(w, "older than on the other nodes (rehome install --upgrade):")

		for _, o := range r.Upgradable {
			fmt.Fprintf(w, "  %s: %s\n", o.ID, o.Reason)
		}
	}

	for _, n := range r.VersionNotes {
		fmt.Fprintf(w, "version: %s\n", n)
	}

	if len(r.KeptLocal) > 0 {
		fmt.Fprintln(w, "settings kept as they are (differ from the config; use --overwrite-files to replace):")

		for _, k := range r.KeptLocal {
			fmt.Fprintf(w, "  %s\n", k.Dest)
		}
	}

	if len(r.ManualSecrets) > 0 {
		fmt.Fprintln(w, "fill in secrets by hand:")

		for _, s := range r.ManualSecrets {
			fmt.Fprintf(w, "  %s\n", s)
		}
	}

	r.printSources(w)

	for _, n := range r.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
}

// printSources lists the installed apps by the node they came from, then what was left out and why.
func (r *Report) printSources(w io.Writer) {
	byNode := map[string][]string{}

	for id, from := range r.TakenFrom {
		byNode[from] = append(byNode[from], id)
	}

	if len(byNode) > 0 {
		fmt.Fprintln(w, "taken from:")

		for _, n := range slices.Sorted(maps.Keys(byNode)) {
			slices.Sort(byNode[n])
			fmt.Fprintf(w, "  %s: %s\n", n, strings.Join(byNode[n], ", "))
		}
	}

	if len(r.LeftOut) > 0 {
		fmt.Fprintln(w, "left out:")

		for _, c := range r.LeftOut {
			on := ""

			if c.Node != "" {
				on = " (on " + c.Node + ")"
			}

			fmt.Fprintf(w, "  %s: %s%s\n", c.ID, c.Reason, on)
		}
	}
}
