package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/secrets"
)

var errNotLinked = errors.New("no linked config repo: run rehome link <git-url>")

func newPushCmd() *cobra.Command {
	var message string

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Commit and push the linked config repo",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := linkedSettings()

			if err != nil {
				return err
			}

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
				host, _ := os.Hostname()
				message = fmt.Sprintf("rehome: update from %s at %s", host, time.Now().Format(time.DateTime))
			}

			return gitsync.Git{DryRun: flags.dryRun}.Push(cmd.Context(), s.ClonePath, message)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message")

	return cmd
}

func newPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Pull the linked config repo",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := linkedSettings()

			if err != nil {
				return err
			}

			return gitsync.Git{DryRun: flags.dryRun}.Pull(cmd.Context(), s.ClonePath)
		},
	}
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
