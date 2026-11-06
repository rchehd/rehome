package app

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/node"
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
	Node     *NodePlan
}

// NodePlan is this machine's node as the scan found it.
type NodePlan struct {
	Node    *node.Node
	Changes []string
	// Gone are apps the last scan here found that this one did not.
	Gone []string
}

func PrepareScan(ctx context.Context, o Options) (*ScanPlan, error) {
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

	w, err := openWorkspace(cfg, root)

	if err != nil {
		return nil, err
	}

	own, _ := w.WithFiles(w.OwnFiles)
	o.logf("scanning %s as node %s …", w.Platform, w.Self.Name)
	res, err := scan.Run(ctx, o.Runner, w.Platform, cat, root, own, scan.Options{HarnessData: o.HarnessData, Sessions: o.Sessions, SessionPaths: o.SessionPaths, FilesRoot: w.Self.Path})

	if err != nil {
		return nil, err
	}

	recipes := *res.Config
	recipes.Files = nil
	merged, report, err := config.Merge(existing, &recipes, w.Platform.Key())

	if err != nil {
		return nil, err
	}

	// Install specs come from URLs and scripts too; the config file passes the same gate as files/.
	merged, findings := secrets.Redact(config.FileName, merged)
	res.Findings = append(res.Findings, findings...)

	sp := &ScanPlan{Root: root, Existing: existing, Merged: merged, Report: report, Result: res}
	sp.Node, err = prepareNode(ctx, o, sp, w)

	return sp, err
}

// Changed reports whether writing would change anything on disk.
func (sp *ScanPlan) Changed() bool {
	return sp.Report.Changed() || len(sp.Result.FileChanges) > 0 || len(sp.Node.Changes) > 0
}

// Write stores the accepted settings files, the merged recipes and this machine's node.
func (sp *ScanPlan) Write(changes []files.Change) error {
	var shared, own []files.Change

	for _, c := range changes {
		if c.Shared {
			shared = append(shared, c)
		} else {
			own = append(own, c)
		}
	}

	if err := files.WriteChanges(sp.Root, shared); err != nil {
		return err
	}

	if err := files.WriteChanges(sp.Node.Node.Path, own); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(sp.Root, config.FileName), sp.Merged, 0o644); err != nil {
		return err
	}

	sp.Node.Node.LastSeen = time.Now().UTC().Truncate(time.Second)

	return sp.Node.Node.Save()
}

func prepareNode(ctx context.Context, o Options, sp *ScanPlan, w *Workspace) (*NodePlan, error) {
	cfg, err := config.Parse(sp.Merged)

	if err != nil {
		return nil, err
	}

	found := map[string]bool{}

	for _, a := range sp.Result.Config.Apps {
		found[a.ID] = true
	}

	o.logf("reading installed versions …")
	current := scan.Versions(ctx, o.Runner, cfg, found, w.Platform)
	next := *w.Self
	next.System, next.Platform, next.FileKey, next.Arch = w.Platform.String(), w.Platform.Key(), w.Platform.FileKey(), w.Platform.Arch
	next.Rehome = o.Version
	next.Apps = current
	next.Files = mergeFiles(w.Self.Files, sp.Result.Config.Files)
	next.Unknown = sp.Result.Unknown
	np := &NodePlan{Node: &next}

	for _, id := range slices.Sorted(maps.Keys(found)) {
		if old, ok := w.Self.Apps[id]; !ok || old != current[id] {
			np.Changes = append(np.Changes, id)
		}
	}

	for _, id := range slices.Sorted(maps.Keys(w.Self.Apps)) {
		if !found[id] {
			np.Changes = append(np.Changes, id)
			np.Gone = append(np.Gone, id)
		}
	}

	if w.Self.LastSeen.IsZero() || !reflect.DeepEqual(w.Self.Files, next.Files) || !reflect.DeepEqual(w.Self.Unknown, next.Unknown) {
		np.Changes = append(np.Changes, "node "+next.Name)
	}

	return np, nil
}

// mergeFiles keeps every mapping the node had, such as hand-written Bitwarden ones, and adds the
// scanned mappings whose src it does not have yet.
func mergeFiles(have, scanned map[string][]config.FileMap) map[string][]config.FileMap {
	out := maps.Clone(have)

	if out == nil {
		out = map[string][]config.FileMap{}
	}

	for group, found := range scanned {
		merged := slices.Clone(out[group])

		for _, f := range found {
			if !slices.ContainsFunc(merged, func(h config.FileMap) bool { return h.Src == f.Src }) {
				merged = append(merged, f)
			}
		}

		out[group] = merged
	}

	return out
}
