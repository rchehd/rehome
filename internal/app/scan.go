package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/scan"
	"github.com/rchehd/rehome/internal/secrets"
)

// ScanPlan is a finished scan that has not been written yet.
type ScanPlan struct {
	Root     string
	Existing []byte
	Merged   []byte
	Report   config.MergeReport
	Result   *scan.Result
	Lock     *LockPlan
}

// LockPlan is what a scan writes to the repo lock and to this machine's local lock.
type LockPlan struct {
	Path      string
	LocalPath string
	Merged    *lock.Lock
	Local     *lock.Lock
	Current   map[string]lock.Entry
	Changes   []string
}

func PrepareScan(ctx context.Context, o Options) (*ScanPlan, error) {
	p, err := platform.Detect()

	if err != nil {
		return nil, err
	}

	cat, err := catalog.Load()

	if err != nil {
		return nil, err
	}

	root, err := config.ResolveRoot(o.Config)

	if err != nil {
		return nil, err
	}

	existing, err := os.ReadFile(filepath.Join(root, config.FileName))

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	cfg := &config.Config{}

	if len(existing) > 0 {
		cfg, err = config.Parse(existing)

		if err != nil {
			return nil, err
		}
	}

	o.logf("scanning %s …", p)
	res, err := scan.Run(ctx, o.Runner, p, cat, root, cfg)

	if err != nil {
		return nil, err
	}

	merged, report, err := config.Merge(existing, res.Config, p.Key())

	if err != nil {
		return nil, err
	}

	// Install specs come from URLs and scripts too; the config file passes the same gate as files/.
	merged, findings := secrets.Redact(config.FileName, merged)
	res.Findings = append(res.Findings, findings...)

	sp := &ScanPlan{Root: root, Existing: existing, Merged: merged, Report: report, Result: res}
	sp.Lock, err = prepareLock(ctx, o, sp, p)

	return sp, err
}

// Changed reports whether writing would change anything on disk.
func (sp *ScanPlan) Changed() bool {
	return sp.Report.Changed() || len(sp.Result.FileChanges) > 0 || len(sp.Lock.Changes) > 0
}

// Write stores the accepted settings files, the merged config and both locks.
func (sp *ScanPlan) Write(changes []files.Change) error {
	if err := files.WriteChanges(sp.Root, changes); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(sp.Root, config.FileName), sp.Merged, 0o644); err != nil {
		return err
	}

	if err := sp.Lock.Merged.Save(sp.Lock.Path); err != nil {
		return err
	}

	return sp.Lock.Local.Save(sp.Lock.LocalPath)
}

func prepareLock(ctx context.Context, o Options, sp *ScanPlan, p platform.Platform) (*LockPlan, error) {
	cfg, err := config.Parse(sp.Merged)

	if err != nil {
		return nil, err
	}

	found := map[string]bool{}

	for _, a := range sp.Result.Config.Apps {
		found[a.ID] = true
	}

	lp := &LockPlan{Path: lock.Path(sp.Root, o.Lock)}

	if lp.LocalPath, err = lock.LocalPath(); err != nil {
		return nil, err
	}

	repo, err := lock.Load(lp.Path)

	if err != nil {
		return nil, err
	}

	local, err := lock.Load(lp.LocalPath)

	if err != nil {
		return nil, err
	}

	o.logf("reading installed versions …")
	lp.Current = scan.Versions(ctx, o.Runner, cfg, found, p)
	lp.Merged, lp.Changes = lock.Sync(repo, local, lp.Current, p.Key())
	lp.Local = lock.Snapshot(local, lp.Current, p.Key())

	return lp, nil
}
