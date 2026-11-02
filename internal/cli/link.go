package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/gitsync"
)

func newLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link <git-url>",
		Short: "Link a private git repo that holds your config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			path, err := config.DefaultClonePath()

			if err != nil {
				return err
			}

			git := gitsync.Git{DryRun: flags.dryRun}

			if _, err := os.Stat(path); err == nil {
				remote, err := git.RemoteURL(cmd.Context(), path)

				if err != nil || remote != url {
					return fmt.Errorf("%s already holds another repo (%s); move it away first", path, remote)
				}
			} else if err := git.Clone(cmd.Context(), url, path); err != nil {
				return err
			}

			if flags.dryRun {
				return nil
			}

			if err := (config.Settings{RepoURL: url, ClonePath: path}).Save(); err != nil {
				return err
			}

			cmd.Printf("linked %s → %s\n", url, path)

			return nil
		},
	}
}
