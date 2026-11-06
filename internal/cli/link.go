package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/source"
)

func newLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link <git-url|folder|archive.tar.gz>",
		Short: "Link the place that holds your shared config: a private git repo, a folder or an archive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			kind := source.Kind(ref)
			path, err := config.DefaultClonePath()

			if err != nil {
				return err
			}

			switch kind {
			case source.KindGit:
				err = linkGit(cmd, ref, path)
			case source.KindFolder:
				if ref, err = filepath.Abs(ref); err == nil {
					path = ref
					err = os.MkdirAll(path, 0o755)
				}
			case source.KindArchive:
				if !config.IsURL(ref) {
					ref, err = filepath.Abs(ref)
				}

				if err == nil {
					err = linkArchive(path)
				}
			}

			if err != nil || flags.dryRun {
				return err
			}

			s, err := config.LoadSettings()

			if err != nil {
				return err
			}

			s.RepoURL, s.ClonePath = ref, path

			if _, err := s.EnsureNode(); err != nil {
				return err
			}

			if err := s.Save(); err != nil {
				return err
			}

			if err := source.Open(s, false, cmd.ErrOrStderr()).Pull(cmd.Context()); err != nil {
				return err
			}

			cmd.Printf("linked %s (%s) → %s\n", ref, kind, path)

			return nil
		},
	}
}

func linkGit(cmd *cobra.Command, url, path string) error {
	git := gitsync.Git{DryRun: flags.dryRun}

	if _, err := os.Stat(path); err != nil {
		return git.Clone(cmd.Context(), url, path)
	}

	remote, err := git.RemoteURL(cmd.Context(), path)

	if err != nil || remote != url {
		return fmt.Errorf("%s already holds another config (%s); move it away first", path, remote)
	}

	return nil
}

// linkArchive needs an empty working copy, or one an archive filled before.
func linkArchive(path string) error {
	entries, err := os.ReadDir(path)

	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0o755)
	}

	if err != nil {
		return err
	}

	if _, err := os.Stat(filepath.Join(path, ".rehome-archive")); len(entries) > 0 && err != nil {
		return fmt.Errorf("%s already holds another config; move it away first", path)
	}

	return nil
}
