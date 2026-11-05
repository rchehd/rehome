package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/selfupdate"
)

func newUpdateCmd() *cobra.Command {
	return updateCommand(selfupdate.Run, migrateWith)
}

// migrateWith lets the new binary bring the config to its format; it has nothing to do when current.
func migrateWith(cmd *cobra.Command, binary string) error {
	args := []string{"migrate"}
	if flags.config != "" {
		args = append(args, "--config", flags.config)
	}
	c := exec.CommandContext(cmd.Context(), binary, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := c.Run(); err != nil {
		return fmt.Errorf("updated, but the config migration failed: %w; run rehome migrate", err)
	}
	return nil
}

func updateCommand(run func(context.Context, string, selfupdate.Options) (selfupdate.Result, error), migrate func(*cobra.Command, string) error) *cobra.Command {
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
			if err := report(cmd, result, message); err != nil || !result.Updated || flags.json {
				return err
			}
			return migrate(cmd, result.Path)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Check for a newer stable release without installing it")
	return cmd
}
