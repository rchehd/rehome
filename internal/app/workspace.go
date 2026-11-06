package app

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/node"
	"github.com/rchehd/rehome/internal/platform"
)

// Workspace is the shared config with its nodes, seen from this machine.
type Workspace struct {
	Root     string
	Config   *config.Config
	Nodes    []*node.Node
	Self     *node.Node
	Platform platform.Platform
	Settings config.Settings
}

// LoadWorkspace loads the config and its nodes; this machine's node is new until its first scan.
func LoadWorkspace(o Options) (*Workspace, error) {
	cfg, root, err := LoadConfig(o)

	if err != nil {
		return nil, err
	}

	return openWorkspace(cfg, root)
}

func openWorkspace(cfg *config.Config, root string) (*Workspace, error) {
	p, err := platform.Detect()

	if err != nil {
		return nil, err
	}

	w := &Workspace{Root: root, Config: cfg, Platform: p}

	if root != "" {
		if w.Nodes, err = node.List(root); err != nil {
			return nil, err
		}
	}

	for _, n := range w.Nodes {
		if errs := (&config.Config{Files: n.Files}).Validate(); len(errs) > 0 {
			return nil, fmt.Errorf("node %s: %w", n.Name, errors.Join(errs...))
		}
	}

	if w.Settings, err = config.LoadSettings(); err != nil {
		return nil, err
	}

	changed, err := w.Settings.EnsureNode()

	if err != nil {
		return nil, err
	}

	w.Self = node.Find(w.Nodes, w.Settings.NodeID)

	if w.Self == nil {
		name := node.FreeName(w.Nodes, w.Settings.NodeName, w.Settings.NodeID)
		w.Self = node.New(w.Settings.NodeID, name, p)
		w.Self.Path = node.DirOf(root, name)
	}

	if w.Self.Name != w.Settings.NodeName {
		w.Settings.NodeName = w.Self.Name
		changed = true
	}

	if changed {
		if err := w.Settings.Save(); err != nil {
			return nil, err
		}
	}

	return w, nil
}

// Record stores an installed app's version in this machine's node, without other changes.
func (w *Workspace) Record(id string, e lock.Entry) {
	if w.Self.Apps == nil {
		w.Self.Apps = map[string]lock.Entry{}
	}

	w.Self.Apps[id] = e

	if w.Root != "" {
		if err := w.Self.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: node %s: %v\n", w.Self.Name, err)
		}
	}
}

// TouchNode records that this machine's node was seen now, if it has been scanned here.
func TouchNode(root, id string) error {
	nodes, err := node.List(root)

	if err != nil {
		return err
	}

	n := node.Find(nodes, id)

	if n == nil {
		return nil
	}

	n.LastSeen = time.Now().UTC().Truncate(time.Second)

	return n.Save()
}

// All is every node, this machine's included even before its first scan.
func (w *Workspace) All() []*node.Node {
	if slices.Contains(w.Nodes, w.Self) {
		return w.Nodes
	}

	return append(slices.Clone(w.Nodes), w.Self)
}

// Live is the nodes that pushed recently enough to count, this machine's included.
func (w *Workspace) Live() []*node.Node {
	now := time.Now()

	return slices.DeleteFunc(slices.Clone(w.All()), func(n *node.Node) bool {
		return n != w.Self && n.Stale(now, w.Settings.StaleAfter())
	})
}

// WithFiles copies the config with each settings group taken from the node pick returns,
// and the folder each group's files/ paths resolve against.
func (w *Workspace) WithFiles(pick func(group string) *node.Node) (*config.Config, map[string]string) {
	cfg := *w.Config
	cfg.Files = map[string][]config.FileMap{}
	roots := map[string]string{}
	groups := map[string]bool{}

	for _, n := range append(slices.Clone(w.Nodes), w.Self) {
		for g := range n.Files {
			groups[g] = true
		}
	}

	for _, g := range slices.Sorted(maps.Keys(groups)) {
		if n := pick(g); n != nil {
			cfg.Files[g] = n.Files[g]
			roots[g] = n.Path
		}
	}

	return &cfg, roots
}

// OwnFiles picks this machine's node for every group: what the last scan here recorded.
func (w *Workspace) OwnFiles(string) *node.Node {
	return w.Self
}

// SelfNode is this machine's node under root, new if it has none, without reading the config.
func SelfNode(root string) (*node.Node, error) {
	w, err := openWorkspace(&config.Config{}, root)

	if err != nil {
		return nil, err
	}

	return w.Self, nil
}
