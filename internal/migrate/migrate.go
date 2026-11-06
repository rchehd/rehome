// Package migrate upgrades a config folder to the current format one numbered step at a time.
package migrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/node"
)

// Step moves a config folder from one version to the next; dry only lists the actions.
type Step func(root string, self *node.Node, dry bool) ([]string, error)

// steps[v] upgrades version v to v+1.
var steps = map[int]Step{1: v1to2}

// Version reads the config version in root; a missing version field means 1.
func Version(root string) (int, error) {
	data, err := os.ReadFile(config.FilePath(root))

	if err != nil {
		return 0, err
	}

	return VersionOf(data)
}

// VersionOf reads the version of a rehome.yml's contents.
func VersionOf(data []byte) (int, error) {
	var head struct {
		Version int `yaml:"version"`
	}

	if err := yaml.Unmarshal(data, &head); err != nil {
		return 0, err
	}

	if head.Version == 0 {
		return 1, nil
	}

	return head.Version, nil
}

// Run upgrades root to the current version. It works on a copy that replaces root only when
// every step succeeded, so an interrupted run leaves the old config untouched.
func Run(root string, self *node.Node, dry bool) ([]string, error) {
	v, err := Version(root)

	if err != nil || v >= config.CurrentVersion {
		return nil, err
	}

	work := root

	if !dry {
		work = root + ".migrating"

		if err := os.RemoveAll(work); err != nil {
			return nil, err
		}

		if err := copyTree(root, work); err != nil {
			return nil, err
		}

		defer os.RemoveAll(work)
	}

	var done []string

	for ; v < config.CurrentVersion; v++ {
		step, ok := steps[v]

		if !ok {
			return nil, fmt.Errorf("no migration from config version %d", v)
		}

		self.Path = node.DirOf(work, filepath.Base(self.Path))
		actions, err := step(work, self, dry)

		if err != nil {
			return nil, fmt.Errorf("migrate v%d to v%d: %w", v, v+1, err)
		}

		done = append(done, actions...)
	}

	self.Path = node.DirOf(root, filepath.Base(self.Path))

	if dry {
		return done, nil
	}

	return done, swap(root, work)
}

// swap puts work in place of root's contents; root itself stays, so a linked path still works.
func swap(root, work string) error {
	old := root + ".old"

	if err := os.RemoveAll(old); err != nil {
		return err
	}

	if err := os.Rename(root, old); err != nil {
		return err
	}

	if err := os.Rename(work, root); err != nil {
		return errors.Join(err, os.Rename(old, root))
	}

	return os.RemoveAll(old)
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
