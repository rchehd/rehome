package install

import (
	"fmt"
	"io"
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

	for _, n := range r.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
}
