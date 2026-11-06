package app

import (
	"context"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/node"
	"github.com/rchehd/rehome/internal/scan"
	"github.com/rchehd/rehome/internal/status"
)

func Status(ctx context.Context, o Options) (*status.Status, error) {
	w, err := LoadWorkspace(o)

	if err != nil {
		return nil, err
	}

	cfg, _ := w.WithFiles(w.OwnFiles)
	existing, err := config.Marshal(cfg)

	if err != nil {
		return nil, err
	}

	cat, err := catalog.Load()

	if err != nil {
		return nil, err
	}

	options := scan.Options{HarnessData: o.HarnessData, Sessions: o.Sessions, SessionPaths: o.SessionPaths, FilesRoot: w.Self.Path}

	st, err := status.Compute(ctx, o.Runner, cfg, existing, w.Root, w.Platform, cat, node.Versions(w.Live()), w.Self.Lock(), options)

	if err != nil {
		return nil, err
	}

	st.Node = status.NodeState{Name: w.Self.Name, Unscanned: w.Self.LastSeen.IsZero() || st.ScanChanges() > 0}

	return st, nil
}
