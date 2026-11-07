package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/secrets"
	"github.com/rchehd/rehome/internal/source"
	"github.com/rchehd/rehome/internal/ui"
)

var errNotLinked = errors.New("no linked config repo: run rehome link <git-url>")

func newPushCmd() *cobra.Command {
	var message string

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Publish this machine's changes to the linked config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := linkedSettings()

			if err != nil {
				return err
			}

			return publishChecked(cmd, s, message)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message")

	return cmd
}

func newPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Fetch the other machines' changes from the linked config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := linkedSettings()

			if err != nil {
				return err
			}

			return source.Open(s, flags.dryRun, cmd.ErrOrStderr()).Pull(cmd.Context())
		},
	}
}

// publishChecked refuses to publish secret-looking values, then publishes; an empty message gets a default.
func publishChecked(cmd *cobra.Command, s config.Settings, message string) error {
	findings, err := secrets.ScanDir(s.ClonePath)

	if err != nil {
		return err
	}

	if len(findings) > 0 {
		for _, f := range findings {
			cmd.PrintErrf("  %s line %d (%s)\n", f.Path, f.Line, f.Key)
		}

		return fmt.Errorf("refusing to push: %d secret-looking values in the config repo; replace them with %s", len(findings), secrets.Placeholder)
	}

	if message == "" {
		message = fmt.Sprintf("rehome: update from %s at %s", s.NodeName, time.Now().Format(time.DateTime))
	}

	return publish(cmd, s, message)
}

// refreshSeen is how often a push with nothing else to publish still records that this machine is alive.
const refreshSeen = 7 * 24 * time.Hour

// publish marks this machine's node as seen now and sends the config to its source; with nothing new it sends nothing.
func publish(cmd *cobra.Command, s config.Settings, message string) error {
	src := source.Open(s, flags.dryRun, cmd.ErrOrStderr())

	if !src.Unpublished(cmd.Context(), ".") && app.NodeSeenWithin(s.ClonePath, s.NodeID, refreshSeen) {
		ui.New(cmd.OutOrStdout()).Done("nothing to publish")

		return nil
	}

	if !flags.dryRun {
		if err := app.TouchNode(s.ClonePath, s.NodeID); err != nil {
			return err
		}
	}

	return src.Publish(cmd.Context(), message)
}

func linkedSettings() (config.Settings, error) {
	s, err := config.LoadSettings()

	if err != nil {
		return s, err
	}

	if !s.Linked() {
		return s, errNotLinked
	}

	return s, nil
}
