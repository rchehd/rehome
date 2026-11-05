package source

import (
	"context"
	"os"

	"github.com/rchehd/rehome/internal/gitsync"
)

// Folder is used in place: writes land in it directly, so there is nothing to pull or publish.
type Folder struct {
	Path string
}

func (f *Folder) Dir() string {
	return f.Path
}

func (f *Folder) Pull(context.Context) error {
	return nil
}

func (f *Folder) Publish(context.Context, string) error {
	return nil
}

func (f *Folder) Status(context.Context) Status {
	return gitsync.SourceLocal
}

func (f *Folder) Backup(context.Context) (string, error) {
	dir := backupDir(f.Path)

	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}

	return dir, copyTree(f.Path, dir)
}
