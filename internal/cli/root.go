package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/runner"
)

// Version is set at build time via ldflags.
var Version = "dev"

type globalFlags struct {
	config  string
	dryRun  bool
	verbose bool
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
	pf.BoolVar(&flags.dryRun, "dry-run", false, "print what would be done without changing anything")
	pf.BoolVarP(&flags.verbose, "verbose", "v", false, "print every command")

	root.AddCommand(newVersionCmd(), newScanCmd(), newInstallCmd(), newStatusCmd(), newLinkCmd(), newPushCmd(), newPullCmd(), newDebugCmd())

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
