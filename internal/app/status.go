package app

import (
	"context"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/scan"
	"github.com/rchehd/rehome/internal/status"
)

func Status(ctx context.Context, o Options) (*status.Status, error) {
	cfg, root, err := LoadConfig(o)

	if err != nil {
		return nil, err
	}

	existing, err := config.Marshal(cfg)

	if err != nil {
		return nil, err
	}

	p, err := platform.Detect()

	if err != nil {
		return nil, err
	}

	cat, err := catalog.Load()

	if err != nil {
		return nil, err
	}

	repo, err := lock.Load(lock.Path(root, o.Lock))

	if err != nil {
		return nil, err
	}

	localPath, err := lock.LocalPath()

	if err != nil {
		return nil, err
	}

	local, err := lock.Load(localPath)

	if err != nil {
		return nil, err
	}

	return status.Compute(ctx, o.Runner, cfg, existing, root, p, cat, repo, local, scan.Options{HarnessData: o.HarnessData, Sessions: o.Sessions, SessionPaths: o.SessionPaths})
}
