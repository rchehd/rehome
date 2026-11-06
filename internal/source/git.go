package source

import (
	"context"
	"fmt"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/gitsync"
	"github.com/rchehd/rehome/internal/migrate"
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

func (g *Git) Unpublished(ctx context.Context, rel string) bool {
	return gitsync.Unpublished(ctx, g.Path, rel)
}

// MigratedUpstream reports whether the remote's config already has the version current, so another
// machine migrated it first. A rebase the failed publish left half done is given up first.
func (g *Git) MigratedUpstream(ctx context.Context, current int) bool {
	g.Git.AbortRebase(ctx, g.Path)

	if err := g.Git.Fetch(ctx, g.Path); err != nil {
		return false
	}

	data, err := g.Git.Upstream(ctx, g.Path, config.FileName)

	if err != nil {
		return false
	}

	v, err := migrate.VersionOf(data)

	return err == nil && v >= current
}

// SetAside keeps the local commits on branch keep and takes the remote's config.
func (g *Git) SetAside(ctx context.Context, keep string) error {
	return g.Git.SetAside(ctx, g.Path, keep)
}

// Backup needs no copy: the migration is one commit that git revert undoes.
func (g *Git) Backup(context.Context) (string, error) {
	return "git history (git revert undoes the migration commit)", nil
}
