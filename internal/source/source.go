// Package source moves the shared config between this machine's copy and where it is kept:
// a git repo, a folder or a .tar.gz archive.
package source

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/gitsync"
)

type Status = gitsync.SourceStatus

type Source interface {
	// Dir is this machine's working copy, the config root commands read and write.
	Dir() string
	Pull(ctx context.Context) error
	Publish(ctx context.Context, message string) error
	Status(ctx context.Context) Status
	// Unpublished reports whether rel (a path under Dir) changed since it was last published.
	Unpublished(ctx context.Context, rel string) bool
	// Backup keeps a copy of the current config before a migration and says where.
	Backup(ctx context.Context) (string, error)
}

const (
	KindGit     = "git"
	KindFolder  = "folder"
	KindArchive = "archive"
)

// Kind tells a git URL, a .tar.gz archive (path or https URL) and a folder apart.
func Kind(ref string) string {
	switch {
	case strings.HasSuffix(ref, ".tar.gz") || strings.HasSuffix(ref, ".tgz"):
		return KindArchive
	case strings.Contains(ref, "://") || strings.HasPrefix(ref, "git@") || strings.HasSuffix(ref, ".git"):
		return KindGit
	}

	return KindFolder
}

// Open returns the linked source; out receives git's own output.
func Open(s config.Settings, dryRun bool, out io.Writer) Source {
	git := gitsync.Git{DryRun: dryRun, Stdout: out}

	switch Kind(s.RepoURL) {
	case KindArchive:
		return &Archive{Ref: s.RepoURL, Path: s.ClonePath, Own: s.NodeID, DryRun: dryRun}
	case KindFolder:
		return &Folder{Path: s.ClonePath}
	}

	return &Git{Git: git, Path: s.ClonePath}
}

// backupDir is a fresh sibling of path, named after it, for a copy kept before a migration.
func backupDir(path string) string {
	return filepath.Join(filepath.Dir(path), filepath.Base(path)+".v1.bak")
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, p)

		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)

		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)

			if err != nil {
				return err
			}

			return os.Symlink(link, target)
		}

		data, err := os.ReadFile(p)

		if err != nil {
			return err
		}

		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
