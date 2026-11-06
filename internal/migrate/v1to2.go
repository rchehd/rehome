package migrate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/node"
)

// sharedFiles stay at the root for every node: the apt sources and keys recipes point to.
const sharedFiles = "apt"

// v1to2 turns the single-machine layout into recipes plus this machine's node: settings files,
// their mappings and this platform's lock entries move into nodes/<self>/, ignore keys become node names.
func v1to2(root string, self *node.Node, dry bool) ([]string, error) {
	path := config.FilePath(root)
	data, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}

	var doc yaml.Node

	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("config root is not a mapping")
	}

	top := doc.Content[0]
	actions := []string{"set version: 2 in rehome.yml"}

	if files := take(top, "files"); files != nil {
		if err := files.Decode(&self.Files); err != nil {
			return nil, fmt.Errorf("files: %w", err)
		}

		actions = append(actions, fmt.Sprintf("move %d settings groups from rehome.yml into %s", len(self.Files), rel(root, self.Path, node.FileName)))
	}

	if ignore := value(top, "ignore"); ignore != nil {
		for i := 0; i+1 < len(ignore.Content); i += 2 {
			if name := config.NodeName(ignore.Content[i].Value); name != ignore.Content[i].Value {
				actions = append(actions, fmt.Sprintf("rename ignore list %s to node name %s", ignore.Content[i].Value, name))
				ignore.Content[i].Value = name
			}
		}
	}

	if moved := nodeState(top, self); moved > 0 {
		actions = append(actions, fmt.Sprintf("move %d unknown binaries from rehome.yml into %s", moved, rel(root, self.Path, node.FileName)))
	}

	if v := value(top, "version"); v != nil {
		v.Value = "2"
	} else {
		top.Content = append([]*yaml.Node{{Kind: yaml.ScalarNode, Value: "version"}, {Kind: yaml.ScalarNode, Value: "2"}}, top.Content...)
	}

	lockPath := filepath.Join(root, "rehome.lock")
	lockData, err := os.ReadFile(lockPath)

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if lockData != nil {
		var old struct {
			Apps map[string]map[string]lock.Entry `yaml:"apps"`
		}

		if err := yaml.Unmarshal(lockData, &old); err != nil {
			return nil, fmt.Errorf("rehome.lock: %w", err)
		}

		self.Apps = map[string]lock.Entry{}

		for id, byPlatform := range old.Apps {
			if e, ok := byPlatform[self.Platform]; ok && !e.Unsupported {
				self.Apps[id] = e
			}
		}

		actions = append(actions, fmt.Sprintf("move %d %s versions from rehome.lock into the node and remove rehome.lock", len(self.Apps), self.Platform))
	}

	groups, err := os.ReadDir(filepath.Join(root, "files"))

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	for _, g := range groups {
		if g.Name() != sharedFiles {
			actions = append(actions, fmt.Sprintf("move files/%s to %s", g.Name(), rel(root, self.Path, "files", g.Name())))
		}
	}

	if dry {
		return actions, nil
	}

	for _, g := range groups {
		if g.Name() == sharedFiles {
			continue
		}

		dst := filepath.Join(self.Path, "files", g.Name())

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}

		if err := os.Rename(filepath.Join(root, "files", g.Name()), dst); err != nil {
			return nil, err
		}
	}

	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return nil, err
	}

	self.LastSeen = time.Now().UTC().Truncate(time.Second)

	return actions, self.Save()
}

// nodeState strips what v1 kept per machine in the recipes: missing_on_scan flags, and unknown
// binaries (disabled apps with only detected:), which move to the node.
func nodeState(top *yaml.Node, self *node.Node) int {
	apps := value(top, "apps")

	if apps == nil {
		return 0
	}

	moved := 0
	kept := apps.Content[:0]

	for _, app := range apps.Content {
		take(app, "missing_on_scan")
		detected := take(app, "detected")
		id := value(app, "id")

		if detected != nil && id != nil && value(app, "install") == nil {
			meta := map[string]string{}

			if err := detected.Decode(&meta); err == nil {
				if self.Unknown == nil {
					self.Unknown = map[string]map[string]string{}
				}

				self.Unknown[id.Value] = meta
				moved++

				continue
			}
		}

		kept = append(kept, app)
	}

	apps.Content = kept

	return moved
}

// take removes key from the mapping and returns its value.
func take(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v := m.Content[i+1]
			m.Content = append(m.Content[:i], m.Content[i+2:]...)

			return v
		}
	}

	return nil
}

func value(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	return nil
}

func rel(root string, parts ...string) string {
	r, err := filepath.Rel(root, filepath.Join(parts...))

	if err != nil {
		return filepath.Join(parts...)
	}

	return r
}
