package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/source"
	"github.com/rchehd/rehome/internal/ui"
)

func newLinkCmd() *cobra.Command {
	var code string

	cmd := &cobra.Command{
		Use:   "link <git-url|folder|archive.tar.gz> | link lan [host:port]",
		Short: "Link the place that holds your shared config: a private git repo, a folder or an archive, or pair over the LAN",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "lan" {
				addr := ""

				if len(args) == 2 {
					addr = args[1]
				}

				return linkLAN(cmd, addr, code)
			}

			if len(args) != 1 {
				return fmt.Errorf("link takes one source")
			}

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

			ui.New(cmd.OutOrStdout()).Done(fmt.Sprintf("linked %s (%s) → %s", ref, kind, path))

			return nil
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "with link lan: the pairing code rehome serve --pair shows")

	return cmd
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
