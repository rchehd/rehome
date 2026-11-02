package cli

import (
	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/scan"
)

// lockPlan is what a scan will write: the repo lock and this machine's local lock.
type lockPlan struct {
	path      string
	localPath string
	merged    *lock.Lock
	local     *lock.Lock
	current   map[string]lock.Entry
	changes   []string
}

func prepareLock(cmd *cobra.Command, sp *scanPlan, p platform.Platform) (*lockPlan, error) {
	cfg, err := config.Parse(sp.merged)

	if err != nil {
		return nil, err
	}

	found := map[string]bool{}

	for _, a := range sp.result.Config.Apps {
		found[a.ID] = true
	}

	lp := &lockPlan{path: lock.Path(sp.root, flags.lock)}

	if lp.localPath, err = lock.LocalPath(); err != nil {
		return nil, err
	}

	repo, err := lock.Load(lp.path)

	if err != nil {
		return nil, err
	}

	local, err := lock.Load(lp.localPath)

	if err != nil {
		return nil, err
	}

	cmd.PrintErrln("reading installed versions …")
	lp.current = scan.Versions(cmd.Context(), newRunner(), cfg, found, p)
	lp.merged, lp.changes = lock.Sync(repo, local, lp.current, p.Key())
	lp.local = lock.Snapshot(local, lp.current, p.Key())

	return lp, nil
}

func (lp *lockPlan) changed() bool {
	return len(lp.changes) > 0
}

func (lp *lockPlan) write() error {
	if err := lp.merged.Save(lp.path); err != nil {
		return err
	}

	return lp.local.Save(lp.localPath)
}
