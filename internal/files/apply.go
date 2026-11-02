package files

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/secrets"
)

const (
	StatusCopied      = "copied"
	StatusIdentical   = "identical"
	StatusKeptLocal   = "kept-local"
	StatusOverwritten = "overwritten"
	StatusNoSource    = "missing-source"
	StatusNoDest      = "no-destination"
	BackupSuffix      = ".rehome.bak"
)

type ApplyResult struct {
	Src    string `json:"src"`
	Dest   string `json:"dest"`
	Status string `json:"status"`
	// Redacted means the written file still holds placeholders to fill in by hand.
	Redacted bool `json:"redacted,omitempty"`
	// Local and Repo are set for kept-local files so a caller can show the diff.
	Local []byte `json:"-"`
	Repo  []byte `json:"-"`
}

type ApplyOptions struct {
	FileKey   string
	Overwrite bool
	DryRun    bool
}

// Apply copies settings from files/ to the system without silently replacing local edits.
func Apply(root string, maps []config.FileMap, opts ApplyOptions) ([]ApplyResult, error) {
	var results []ApplyResult

	for _, m := range maps {
		res, err := applyOne(root, m, opts)

		if err != nil {
			return results, err
		}

		results = append(results, res)
	}

	return results, nil
}

func applyOne(root string, m config.FileMap, opts ApplyOptions) (ApplyResult, error) {
	res := ApplyResult{Src: m.Src, Dest: m.Dest[opts.FileKey]}

	if res.Dest == "" {
		res.Status = StatusNoDest

		return res, nil
	}

	repo, err := os.ReadFile(filepath.Join(root, m.Src))

	if errors.Is(err, os.ErrNotExist) {
		res.Status = StatusNoSource

		return res, nil
	}

	if err != nil {
		return res, err
	}

	dest := adapter.Home(res.Dest)
	local, err := os.ReadFile(dest)
	res.Redacted = bytes.Contains(repo, []byte(secrets.Placeholder))

	switch {
	case errors.Is(err, os.ErrNotExist):
		res.Status = StatusCopied

		return res, write(dest, repo, opts.DryRun)
	case err != nil:
		return res, err
	case bytes.Equal(local, repo) || isRedactedCopy(dest, local, repo):
		res.Status = StatusIdentical
		res.Redacted = false

		return res, nil
	case !opts.Overwrite:
		res.Status = StatusKeptLocal
		res.Redacted = false
		res.Local, res.Repo = local, repo

		return res, nil
	}

	res.Status = StatusOverwritten

	if opts.DryRun {
		return res, nil
	}

	if err := os.WriteFile(dest+BackupSuffix, local, 0o600); err != nil {
		return res, err
	}

	return res, write(dest, repo, false)
}

// Overwrite replaces a kept-local file with the config's version, keeping a backup.
func Overwrite(res ApplyResult) error {
	dest := adapter.Home(res.Dest)

	if err := os.WriteFile(dest+BackupSuffix, res.Local, 0o600); err != nil {
		return err
	}

	return write(dest, res.Repo, false)
}

// isRedactedCopy is true when the local file only differs by the secrets the repo hides.
func isRedactedCopy(path string, local, repo []byte) bool {
	redacted, findings := secrets.Redact(path, local)

	return len(findings) > 0 && bytes.Equal(redacted, repo)
}

func write(dest string, data []byte, dryRun bool) error {
	if dryRun {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	mode := os.FileMode(0o644)

	if info, err := os.Stat(dest); err == nil {
		mode = info.Mode().Perm()
	}

	return os.WriteFile(dest, data, mode)
}
