package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"charm.land/fang/v2"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/runner"
)

// Version is set at build time via ldflags.
var Version = "dev"

type globalFlags struct {
	config       string
	json         bool
	dryRun       bool
	verbose      bool
	harnessData  bool
	sessions     bool
	sessionPaths map[string]string
}

var flags globalFlags

// ExitError ends the process with Code without printing an error message.
type ExitError struct {
	Code int
}

func (e ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "rehome",
		Short:         "Move your workstation to a new machine",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !interactive() {
				return cmd.Help()
			}

			return runMenu(cmd)
		},
	}

	root.SetOut(os.Stdout)
	root.SetVersionTemplate("rehome {{.Version}}\n")
	pf := root.PersistentFlags()
	pf.StringVar(&flags.config, "config", "", "config file path or URL")
	pf.BoolVar(&flags.json, "json", false, "print machine-readable JSON (for scripts and agents)")
	pf.BoolVar(&flags.dryRun, "dry-run", false, "print what would be done without changing anything")
	pf.BoolVarP(&flags.verbose, "verbose", "v", false, "print every command")
	pf.BoolVar(&flags.harnessData, "harness-data", false, "include user skills and subagents in scan, status, install and sync")
	pf.BoolVar(&flags.sessions, "sessions", false, "include local Claude Code, Codex and Cursor CLI sessions")
	pf.StringToStringVar(&flags.sessionPaths, "session-path-map", nil, "map original project paths to new absolute paths (old=new)")

	root.AddCommand(newVersionCmd(), newUpdateCmd(), newScanCmd(), newInstallCmd(), newSyncCmd(), newStatusCmd(), newConfigCmd(), newMCPCmd(), newLinkCmd(), newMigrateCmd(), newNodesCmd(), newRemoveCmd(), newPushCmd(), newPullCmd(), newDebugCmd(), newServeCmd())

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println("rehome " + Version)
		},
	}
}

func opts(cmd *cobra.Command) app.Options {
	return app.Options{Config: flags.config, Version: Version, Runner: newRunner(), Log: cmd.ErrOrStderr(), HarnessData: flags.harnessData, Sessions: flags.sessions, SessionPaths: flags.sessionPaths}
}

// report prints v as JSON under --json, otherwise the text.
func report(cmd *cobra.Command, v any, text string) error {
	if !flags.json {
		cmd.Println(text)

		return nil
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")

	return enc.Encode(v)
}

func newRunner() *runner.Runner {
	return &runner.Runner{DryRun: flags.dryRun, Verbose: flags.verbose}
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := newRoot()
	err := fang.Execute(context.Background(), root, fang.WithVersion(Version), fang.WithoutManpage(), fang.WithErrorHandler(printError))

	if offerMigration(err) {
		fmt.Fprintln(os.Stderr, "This config uses the previous format.")

		if confirm(root, "Migrate it now?", true) {
			if err = runMigrate(root); err == nil {
				fmt.Fprintln(os.Stderr, "Done; run the command again.")

				return 0
			}
		}
	}

	if err == nil {
		return 0
	}

	var exit ExitError

	if errors.As(err, &exit) {
		return exit.Code
	}

	return 1
}

func offerMigration(err error) bool {
	return errors.Is(err, config.ErrNeedsMigration) && interactive() && !flags.json
}

// printError styles errors in a terminal and keeps them on one plain line in logs.
func printError(w io.Writer, styles fang.Styles, err error) {
	var exit ExitError

	if errors.As(err, &exit) || offerMigration(err) {
		return
	}

	if !isatty.IsTerminal(os.Stderr.Fd()) {
		fmt.Fprintln(os.Stderr, "error:", err)

		return
	}

	fang.DefaultErrorHandler(w, styles, err)
}
