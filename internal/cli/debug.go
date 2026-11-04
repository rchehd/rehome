package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/rchehd/rehome/internal/adapter/all"
	"github.com/rchehd/rehome/internal/adapter/apt"
	"github.com/rchehd/rehome/internal/ai"
	"github.com/rchehd/rehome/internal/binaries"
	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
)

func newDebugCmd() *cobra.Command {
	debug := &cobra.Command{
		Use:    "debug",
		Short:  "Developer diagnostics",
		Hidden: true,
	}

	var sudo bool

	run := &cobra.Command{
		Use:   "run -- <cmd> [args...]",
		Short: "Run a command through the runner",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := newRunner().Run(cmd.Context(), runner.Cmd{Name: args[0], Args: args[1:], Sudo: sudo})
			cmd.Print(res.Stdout)

			return err
		},
	}
	run.Flags().BoolVar(&sudo, "sudo", false, "run through sudo")

	var overwrite bool

	applyFile := &cobra.Command{
		Use:   "apply-file <root> <src> <dest>",
		Short: "Apply one settings file from <root>/<src> to <dest>",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			maps := []config.FileMap{{Src: args[1], Dest: map[string]string{"linux": args[2]}}}
			res, err := files.Apply(cmd.Context(), args[0], maps, files.ApplyOptions{FileKey: "linux", Overwrite: overwrite, DryRun: flags.dryRun})

			for _, r := range res {
				cmd.Printf("%s → %s: %s redacted=%v\n", r.Src, r.Dest, r.Status, r.Redacted)
			}

			return err
		},
	}
	applyFile.Flags().BoolVar(&overwrite, "overwrite", false, "replace differing local files (with backup)")

	debug.AddCommand(
		run,
		applyFile,
		&cobra.Command{
			Use:   "claude <root>",
			Short: "Capture Claude MCP servers and plugins into <root>",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				changes, err := ai.CaptureClaude(args[0])

				if err != nil {
					return err
				}

				for _, c := range changes {
					cmd.Printf("%s (%d findings)\n", c.Dst, len(c.Findings))
				}

				return files.WriteChanges(args[0], changes)
			},
		},
		&cobra.Command{
			Use:   "capture <catalog-id> <root>",
			Short: "Capture an app's settings into <root>/files and list them",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				cat, err := catalog.Load()

				if err != nil {
					return err
				}

				e, ok := cat.Get(args[0])

				if !ok {
					return fmt.Errorf("no catalog entry %q", args[0])
				}

				changes, maps, err := files.Capture(e, "linux", args[1])

				if err != nil {
					return err
				}

				for _, c := range changes {
					cmd.Printf("%s ← %s (%d findings)\n", c.Dst, c.Src, len(c.Findings))
				}

				cmd.Printf("%d files mapped, %d changed\n", len(maps), len(changes))

				return files.WriteChanges(args[1], changes)
			},
		},
		&cobra.Command{
			Use:   "redact <file>",
			Short: "Print a file with secrets redacted",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				data, err := os.ReadFile(args[0])

				if err != nil {
					return err
				}

				out, findings := secrets.Redact(args[0], data)
				cmd.Print(string(out))

				for _, f := range findings {
					cmd.PrintErrf("finding: line %d key %s\n", f.Line, f.Key)
				}

				return nil
			},
		},
		&cobra.Command{
			Use:   "catalog",
			Short: "Load the embedded catalog and list entries present on this machine",
			RunE: func(cmd *cobra.Command, _ []string) error {
				cat, err := catalog.Load()

				if err != nil {
					return err
				}

				p, err := platform.Detect()

				if err != nil {
					return err
				}

				cmd.Printf("%d entries\n", len(cat.Entries))
				known := map[string]bool{}

				for _, e := range cat.Present(p.Key()) {
					cmd.Println("present: " + e.ID)

					for _, b := range e.Detect.Bins {
						known[b] = true
					}
				}

				found, err := binaries.Resolve(cmd.Context(), newRunner(), cat, known)

				for _, d := range append(found, binaries.GitClones(cmd.Context(), newRunner())...) {
					cmd.Printf("%-8s %-24s %v %s\n", d.Manager, d.Name, d.Meta, d.Spec.Script)
				}

				return err
			},
		},
		&cobra.Command{
			Use:   "retarget <source-file> <from-codename> <to-codename>",
			Short: "Print an apt source retargeted to another release",
			Args:  cobra.ExactArgs(3),
			RunE: func(cmd *cobra.Command, args []string) error {
				data, err := os.ReadFile(args[0])

				if err != nil {
					return err
				}

				cmd.Print(apt.Retarget(string(data), args[1], args[2]))

				return nil
			},
		},
		&cobra.Command{
			Use:   "scan <manager>",
			Short: "Run one adapter's scan",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				p, err := platform.Detect()

				if err != nil {
					return err
				}

				for _, a := range all.For(p) {
					if a.Name() != args[0] {
						continue
					}

					found, err := a.Scan(cmd.Context(), newRunner())

					for _, d := range found {
						spec, _ := yaml.Marshal(d.Spec)
						cmd.Printf("%-36s %s %v %v\n", d.Name, strings.ReplaceAll(strings.TrimSpace(string(spec)), "\n", ", "), d.Requires, d.Meta)
					}

					cmd.Printf("%d found\n", len(found))

					return err
				}

				return fmt.Errorf("adapter %q not available", args[0])
			},
		},
		&cobra.Command{
			Use:   "merge <existing.yml> <scanned.yml>",
			Short: "Merge a scanned config into an existing one and print the result",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				existing, err := os.ReadFile(args[0])

				if err != nil {
					return err
				}

				scanned, _, err := config.Load(args[1])

				if err != nil {
					return err
				}

				out, rep, err := config.Merge(existing, scanned, "debian")

				if err != nil {
					return err
				}

				cmd.Print(string(out))
				cmd.PrintErrf("added=%v missing=%v returned=%v identical=%v\n", rep.Added, rep.MissingOnScan, rep.Returned, string(out) == string(existing))

				return nil
			},
		},
		&cobra.Command{
			Use:   "config",
			Short: "Load and validate the config given by --config",
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, root, err := config.Load(flags.config)

				if err != nil {
					return err
				}

				errs := cfg.Validate()
				cmd.Printf("%s: %d apps, %d repos, %d file groups, %d errors\n", root, len(cfg.Apps), len(cfg.AptRepos), len(cfg.Files), len(errs))

				for _, e := range errs {
					cmd.Println("  " + e.Error())
				}

				return nil
			},
		},
		&cobra.Command{
			Use:   "platform",
			Short: "Print the detected platform",
			RunE: func(cmd *cobra.Command, _ []string) error {
				p, err := platform.Detect()

				if err != nil {
					return err
				}

				cmd.Println(p.String())

				return nil
			},
		},
	)

	return debug
}
