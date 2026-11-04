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

func applySecret(root string, m config.FileMap, opts ApplyOptions, res *ApplyResult) error {
	if opts.DryRun || opts.ResolveSecret == nil {
		res.Status = StatusSecretSkip
		res.Problem = "use --bitwarden with an unlocked vault to restore this setting"

		if opts.DryRun {
			res.Problem = "Bitwarden lookup skipped in dry run"
		}

		return nil
	}

	sshKey := secrets.IsBitwardenSSHKey(m.Bitwarden)
	var template []byte

	if sshKey {
		if m.Src != "" || !config.ValidSSHKeyDest(res.Dest) {
			return errors.New("Bitwarden SSH keys need a destination directly under ~/.ssh and no src template")
		}
	} else {
		var err error
		template, err = readSecretTemplate(root, m.Src)

		if err != nil {
			return err
		}
	}

	dest := adapter.Home(res.Dest)
	repo, err := resolveParent(root)

	if err != nil {
		return err
	}

	parent, err := resolveParent(filepath.Dir(dest))

	if err != nil {
		return err
	}

	if sshKey {
		if err := checkSSHDir(parent); err != nil {
			return err
		}
	}

	dest = filepath.Join(parent, filepath.Base(dest))
	rel, err := filepath.Rel(repo, dest)

	if err != nil || filepath.IsLocal(rel) {
		return errors.New("Bitwarden destination must be outside the config folder")
	}

	info, err := os.Lstat(dest)
	exists := err == nil

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if exists && !info.Mode().IsRegular() {
		return errors.New("Bitwarden destination must be a regular file")
	}

	value, err := opts.ResolveSecret(m.Bitwarden)

	if err != nil {
		return err
	}

	rendered := []byte(value)

	if !sshKey {
		rendered = bytes.Replace(template, []byte(secrets.Placeholder), rendered, 1)
	}

	if !exists {
		res.Status = StatusCopied

		return writeSecret(dest, rendered, false)
	}

	local, err := os.ReadFile(dest)

	if err != nil {
		return err
	}

	if bytes.Equal(local, rendered) && info.Mode().Perm() == 0o600 {
		res.Status = StatusIdentical

		return nil
	}

	if !opts.Overwrite {
		res.Status = StatusKeptLocal

		return nil
	}

	if err := writeSecret(dest+BackupSuffix, local, false); errors.Is(err, os.ErrExist) {
		backup, err := os.CreateTemp(parent, filepath.Base(dest)+BackupSuffix+".*")

		if err != nil {
			return err
		}

		if err := writeSecretTemp(backup, local); err != nil {
			os.Remove(backup.Name())

			return err
		}
	} else if err != nil {
		return err
	}

	res.Status = StatusOverwritten

	return writeSecret(dest, rendered, true)
}

func readSecretTemplate(root, src string) ([]byte, error) {
	dir, err := os.OpenRoot(root)

	if err != nil {
		return nil, err
	}

	defer dir.Close()

	template, err := dir.ReadFile(src)

	if err != nil {
		return nil, err
	}

	if secrets.IsBinary(template) || bytes.Count(template, []byte(secrets.Placeholder)) != 1 {
		return nil, errors.New("Bitwarden template must be text with exactly one <rehome:redacted> placeholder")
	}

	return template, nil
}

func checkSSHDir(parent string) error {
	home, err := os.UserHomeDir()

	if err != nil {
		return err
	}

	home, err = resolveParent(home)

	if err != nil {
		return err
	}

	if parent != filepath.Join(home, ".ssh") {
		return errors.New("Bitwarden SSH keys require a real ~/.ssh directory")
	}

	info, err := os.Lstat(parent)

	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return errors.New("~/.ssh must be a directory without group or other write permissions")
	}

	return nil
}

func resolveParent(path string) (string, error) {
	path, err := filepath.Abs(path)

	if err != nil {
		return "", err
	}

	real, err := filepath.EvalSymlinks(path)

	if !errors.Is(err, os.ErrNotExist) {
		return real, err
	}

	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("could not resolve Bitwarden destination directory")
	}

	parent, err := resolveParent(filepath.Dir(path))

	return filepath.Join(parent, filepath.Base(path)), err
}

func writeSecret(dest string, data []byte, replace bool) error {
	dir := filepath.Dir(dest)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	temp, err := os.CreateTemp(dir, ".rehome-secret-*")

	if err != nil {
		return err
	}

	defer os.Remove(temp.Name())

	if err := writeSecretTemp(temp, data); err != nil {
		return err
	}

	if replace {
		return os.Rename(temp.Name(), dest)
	}

	return os.Link(temp.Name(), dest)
}

func writeSecretTemp(file *os.File, data []byte) error {
	_, err := file.Write(data)

	return errors.Join(err, file.Close())
}
