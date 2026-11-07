// Package node holds one machine's snapshot in the shared config: nodes/<name>/node.yml and its files/.
package node

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
)

const (
	Dir      = "nodes"
	FileName = "node.yml"
)

type Node struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	// System is a readable description such as "Ubuntu 24.04 (WSL)".
	System   string    `yaml:"system"`
	Platform string    `yaml:"platform"`
	FileKey  string    `yaml:"file_key"`
	Arch     string    `yaml:"arch"`
	LastSeen time.Time `yaml:"last_seen"`
	Rehome   string    `yaml:"rehome,omitempty"`
	// Apps are the installed apps with their versions, by app id.
	Apps  map[string]lock.Entry       `yaml:"apps,omitempty"`
	Files map[string][]config.FileMap `yaml:"files,omitempty"`
	// Unknown are binaries found here that no recipe knows, with where they were found.
	Unknown map[string]map[string]string `yaml:"unknown,omitempty"`
	// Key is the owner's LAN public key, named pubkey so the secret scan leaves it alone.
	// Revision counts its scans, so peers keep the newest copy.
	Key      string `yaml:"pubkey,omitempty"`
	Revision int    `yaml:"revision,omitempty"`
	// Path is the node's directory, the root its files/ paths resolve against.
	Path string `yaml:"-"`
}

// New describes this machine as a node with no apps yet.
func New(id, name string, p platform.Platform) *Node {
	return &Node{ID: id, Name: name, System: p.String(), Platform: p.Key(), FileKey: p.FileKey(), Arch: p.Arch}
}

// DirOf is the directory of the node called name under the config root.
func DirOf(root, name string) string {
	return filepath.Join(root, Dir, name)
}

func Load(dir string) (*Node, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))

	if err != nil {
		return nil, err
	}

	n := &Node{}

	if err := yaml.Unmarshal(data, n); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, FileName), err)
	}

	n.Path = dir

	return n, nil
}

// Save writes node.yml into n.Path; yaml.v3 sorts map keys, so diffs stay stable.
func (n *Node) Save() error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(n); err != nil {
		return err
	}

	if err := os.MkdirAll(n.Path, 0o755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(n.Path, FileName), buf.Bytes(), 0o644)
}

// List loads every node under root, sorted by name; a root without nodes/ has none.
func List(root string) ([]*Node, error) {
	entries, err := os.ReadDir(filepath.Join(root, Dir))

	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var nodes []*Node

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		n, err := Load(filepath.Join(root, Dir, e.Name()))

		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil {
			return nil, err
		}

		nodes = append(nodes, n)
	}

	slices.SortFunc(nodes, func(a, b *Node) int { return strings.Compare(a.Name, b.Name) })

	return nodes, nil
}

// Find returns the node with this id or name.
func Find(nodes []*Node, ref string) *Node {
	for _, n := range nodes {
		if n.ID == ref || n.Name == ref {
			return n
		}
	}

	return nil
}

// Stale reports whether the node has not pushed for longer than after; zero never expires.
func (n *Node) Stale(now time.Time, after time.Duration) bool {
	return after > 0 && now.Sub(n.LastSeen) > after
}

// Freshest is the most recently seen node that ok accepts, or nil.
func Freshest(nodes []*Node, ok func(*Node) bool) *Node {
	var best *Node

	for _, n := range nodes {
		if ok(n) && (best == nil || n.LastSeen.After(best.LastSeen)) {
			best = n
		}
	}

	return best
}

// FreeName is name, or name-2, name-3, … when another node already uses it.
func FreeName(nodes []*Node, name, id string) string {
	taken := func(candidate string) bool {
		n := Find(nodes, candidate)

		return n != nil && n.ID != id
	}
	candidate := name

	for i := 2; taken(candidate); i++ {
		candidate = fmt.Sprintf("%s-%d", name, i)
	}

	return candidate
}

// Lock is this node's versions keyed by its platform, the shape install and status compare against.
func (n *Node) Lock() *lock.Lock {
	return Versions([]*Node{n})
}

// Versions merges the nodes' versions per platform: a newer version wins, and an entry that
// cannot be ordered (a commit, another manager) comes from the most recently seen node.
func Versions(nodes []*Node) *lock.Lock {
	byAge := slices.Clone(nodes)
	slices.SortStableFunc(byAge, func(a, b *Node) int { return a.LastSeen.Compare(b.LastSeen) })
	l := lock.New()

	for _, n := range byAge {
		for id, e := range n.Apps {
			if old, ok := l.Get(id, n.Platform); ok && orderable(old, e) && lock.Compare(e.Manager, e.Version, old.Version) < 0 {
				continue
			}

			l.Set(id, n.Platform, e)
		}
	}

	return l
}

func orderable(a, b lock.Entry) bool {
	return a.Manager == b.Manager && a.Version != "" && b.Version != "" &&
		a.Commit == "" && b.Commit == "" && !a.Unsupported && !b.Unsupported
}
