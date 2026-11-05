package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/selfupdate"
)

func newUpdateCmd() *cobra.Command {
	return updateCommand(selfupdate.Run)
}

func updateCommand(run func(context.Context, string, selfupdate.Options) (selfupdate.Result, error)) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use: "update", Short: "Update rehome itself to the latest stable release", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !flags.json {
				cmd.PrintErrln("Checking for a rehome update…")
			}
			result, err := run(cmd.Context(), Version, selfupdate.Options{CheckOnly: check, DryRun: flags.dryRun})
			if err != nil {
				return err
			}
			message := fmt.Sprintf("No newer stable release (installed %s, latest %s).", result.Current, result.Latest)
			if result.Updated {
				message = fmt.Sprintf("Updated rehome %s → %s\nBinary: %s", result.Current, result.Latest, result.Path)
			} else if result.Available {
				message = fmt.Sprintf("Update available: rehome %s → %s\nRun rehome update to install it.", result.Current, result.Latest)
			}
			return report(cmd, result, message)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Check for a newer stable release without installing it")
	return cmd
}
