package source

import (
	"context"
	"fmt"

	"github.com/rchehd/rehome/internal/gitsync"
)

// Git is a clone of the config repo; every node writes only its own folder, so rebases merge cleanly.
type Git struct {
	Git  gitsync.Git
	Path string
}

func (g *Git) Dir() string {
	return g.Path
}

func (g *Git) Pull(ctx context.Context) error {
	return g.Git.Pull(ctx, g.Path)
}

// Publish commits, rebases onto the remote and pushes; offline, the commit waits for the next push.
func (g *Git) Publish(ctx context.Context, message string) error {
	if err := g.Git.Commit(ctx, g.Path, message); err != nil {
		return err
	}

	if err := g.Git.Pull(ctx, g.Path); err != nil {
		return fmt.Errorf("committed locally; pushing needs the remote first: %w", err)
	}

	return g.Git.PushOnly(ctx, g.Path)
}

func (g *Git) Status(ctx context.Context) Status {
	return gitsync.RemoteStatus(ctx, g.Path)
}

// Backup needs no copy: the migration is one commit that git revert undoes.
func (g *Git) Backup(context.Context) (string, error) {
	return "git history (git revert undoes the migration commit)", nil
}
