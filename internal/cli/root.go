package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/runner"
)

// Version is set at build time via ldflags.
var Version = "dev"

type globalFlags struct {
	config       string
	lock         string
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
	pf := root.PersistentFlags()
	pf.StringVar(&flags.config, "config", "", "config file path or URL")
	pf.StringVar(&flags.lock, "lock", "", "lock file path (default: rehome.lock next to the config)")
	pf.BoolVar(&flags.json, "json", false, "print machine-readable JSON (for scripts and agents)")
	pf.BoolVar(&flags.dryRun, "dry-run", false, "print what would be done without changing anything")
	pf.BoolVarP(&flags.verbose, "verbose", "v", false, "print every command")
	pf.BoolVar(&flags.harnessData, "harness-data", false, "include user skills and subagents in scan, status, install and sync")
	pf.BoolVar(&flags.sessions, "sessions", false, "include local Claude Code, Codex and Cursor CLI sessions")
	pf.StringToStringVar(&flags.sessionPaths, "session-path-map", nil, "map original project paths to new absolute paths (old=new)")

	root.AddCommand(newVersionCmd(), newScanCmd(), newInstallCmd(), newSyncCmd(), newStatusCmd(), newConfigCmd(), newMCPCmd(), newLinkCmd(), newPushCmd(), newPullCmd(), newDebugCmd())

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
	return app.Options{Config: flags.config, Lock: flags.lock, Runner: newRunner(), Log: cmd.ErrOrStderr(), HarnessData: flags.harnessData, Sessions: flags.sessions, SessionPaths: flags.sessionPaths}
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
	err := newRoot().Execute()

	if err == nil {
		return 0
	}

	var exit ExitError

	if errors.As(err, &exit) {
		return exit.Code
	}

	fmt.Fprintln(os.Stderr, "error:", err)

	return 1
}
