package app

import (
	"github.com/rchehd/rehome/internal/lan"
	"github.com/rchehd/rehome/internal/node"
)

// SaveNode writes this machine's node; with LAN in use it counts a new revision and signs the folder so peers take it.
func SaveNode(n *node.Node) error {
	dir, err := lan.Dir()

	if err != nil || !lan.HasIdentity(dir) {
		return n.Save()
	}

	id, err := lan.LoadIdentity(dir)

	if err != nil {
		return err
	}

	n.Key = lan.EncodeKey(id.Public())
	n.Revision++

	if err := n.Save(); err != nil {
		return err
	}

	return lan.SignFolder(n.Path, n, id)
}
