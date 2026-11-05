package files

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
)

const (
	StatusCopied       = "copied"
	StatusIdentical    = "identical"
	StatusKeptLocal    = "kept-local"
	StatusOverwritten  = "overwritten"
	StatusNoSource     = "missing-source"
	StatusNoDest       = "no-destination"
	StatusSecretSkip   = "secret-skipped"
	StatusSecretError  = "secret-error"
	StatusDconfSkip    = "dconf-skipped"
	StatusDconfError   = "dconf-error"
	StatusSessionError = "session-error"
	BackupSuffix       = ".rehome.bak"
)

type ApplyResult struct {
	Src    string `json:"src"`
	Dest   string `json:"dest"`
	Status string `json:"status"`
	// Redacted means the written file still holds placeholders to fill in by hand.
	Redacted bool   `json:"redacted,omitempty"`
	Problem  string `json:"problem,omitempty"`
	Backup   string `json:"backup,omitempty"`
	// Local and Repo are set for kept-local files so a caller can show the diff.
	Local []byte `json:"-"`
	Repo  []byte `json:"-"`
}

type ApplyOptions struct {
	FileKey       string
	WSL           bool
	HarnessData   bool
	Sessions      bool
	SessionPaths  map[string]string
	Overwrite     bool
	DryRun        bool
	ResolveSecret func(string) (string, error)
	Runner        *runner.Runner
}

// Apply copies settings from files/ to the system without silently replacing local edits.
func Apply(ctx context.Context, root string, maps []config.FileMap, opts ApplyOptions) ([]ApplyResult, error) {
	var results []ApplyResult

	for _, m := range maps {
		if m.Session != "" {
			if opts.Sessions {
				results = append(results, applySession(ctx, root, m, opts))
			}
			continue
		}
		if !opts.HarnessData && isHarnessData(m.Dest[opts.FileKey]) {
			continue
		}

		if m.Dconf {
			res := applyDconf(ctx, root, m.Src, opts)
			results = append(results, res)

			continue
		}

		res, err := applyOne(root, m, opts)

		if err != nil {
			if m.Bitwarden == "" {
				return results, err
			}

			res.Status, res.Problem = StatusSecretError, err.Error()
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

	if m.Bitwarden != "" {
		err := applySecret(root, m, opts, &res)

		return res, err
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

		return res, write(dest, repo, fileMode(m.Mode), opts.DryRun)
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

	if err := backup(dest, local); err != nil {
		return res, err
	}

	return res, write(dest, repo, fileMode(m.Mode), false)
}

// Overwrite replaces a kept-local file with the config's version, keeping a backup.
func Overwrite(res ApplyResult) error {
	dest := adapter.Home(res.Dest)

	if err := backup(dest, res.Local); err != nil {
		return err
	}

	return write(dest, res.Repo, 0, false)
}

// backup keeps the previous content; an older backup is never overwritten.
func backup(dest string, data []byte) error {
	path := dest + BackupSuffix

	if _, err := os.Stat(path); err == nil {
		path += "." + time.Now().Format("20060102-150405")
	}

	return os.WriteFile(path, data, 0o600)
}

// fileMode parses a FileMap mode; 0 means "keep the file's or the default mode".
func fileMode(s string) os.FileMode {
	m, err := strconv.ParseUint(s, 8, 32)

	if err != nil {
		return 0
	}

	return os.FileMode(m)
}

// isRedactedCopy is true when the local file only differs by the secrets the repo hides.
func isRedactedCopy(path string, local, repo []byte) bool {
	redacted, findings := secrets.Redact(path, local)

	return len(findings) > 0 && bytes.Equal(redacted, repo)
}

// write keeps an existing file's mode; a new file gets want (private modes from the config)
// or 0644.
func write(dest string, data []byte, want os.FileMode, dryRun bool) error {
	if dryRun {
		return nil
	}

	dir := filepath.Dir(dest)
	dirMode := os.FileMode(0o755)

	if dir == adapter.Home("~/.ssh") {
		dirMode = 0o700
	}

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}

	mode := os.FileMode(0o644)

	if want != 0 {
		mode = want
	}

	if info, err := os.Stat(dest); err == nil {
		mode = info.Mode().Perm()
	}

	if err := os.WriteFile(dest, data, mode); err != nil {
		return err
	}

	// WriteFile applies the mode only when creating; an existing file keeps its own.
	return os.Chmod(dest, mode)
}
