package lan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/node"
)

// PullOptions says where to pull into and from whom; Confirm approves recipes that run code or add sources.
type PullOptions struct {
	Root     string
	OwnID    string
	Client   *Client
	Found    []Found
	Settings config.Settings
	Confirm  func(app config.App) bool
}

type PullReport struct {
	Updated      []string          `json:"updated"`
	Skipped      map[string]string `json:"skipped,omitempty"`
	RecipesAdded []string          `json:"recipes_added,omitempty"`
	// Declined are recipes that run code or add sources, which the user did not approve.
	Declined    []string `json:"declined,omitempty"`
	Unreachable []string `json:"unreachable,omitempty"`
	// OwnRevision is the highest revision of this machine's node a peer still holds.
	OwnRevision int `json:"own_revision,omitempty"`
}

// Pull takes newer node snapshots signed by machines paired directly, and recipes, from every found peer.
func Pull(ctx context.Context, o PullOptions) (PullReport, error) {
	rep := PullReport{Updated: []string{}, Skipped: map[string]string{}}
	revisions, err := localRevisions(o.Root)

	if err != nil {
		return rep, err
	}

	for _, f := range o.Found {
		entries, _, err := o.Client.Index(ctx, f.Addr)

		if err != nil {
			rep.Unreachable = append(rep.Unreachable, f.Addr+": "+err.Error())

			continue
		}

		for _, e := range entries {
			if e.ID == o.OwnID {
				rep.OwnRevision = max(rep.OwnRevision, e.Revision)

				continue
			}

			revision, err := pullNode(ctx, o, f.Addr, e, revisions[e.ID])

			switch {
			case err != nil:
				rep.Skipped[e.Name] = err.Error()
			case revision > 0:
				revisions[e.ID] = revision
				rep.Updated = append(rep.Updated, e.Name)
				delete(rep.Skipped, e.Name)
			}
		}

		if data, err := o.Client.Recipes(ctx, f.Addr); err == nil {
			if err := mergeRecipes(o, data, &rep); err != nil {
				rep.Skipped["recipes from "+f.Addr] = err.Error()
			}
		}
	}

	return rep, nil
}

// pullNode installs e if it is newer than have and signed by its directly paired key; 0 means nothing new.
func pullNode(ctx context.Context, o PullOptions, addr string, e NodeEntry, have int) (int, error) {
	key, ok := o.Settings.PairedKey(e.ID)

	if !ok {
		return 0, errors.New("not paired directly")
	}

	m, err := VerifySigned(e.Signed, key)

	if err != nil {
		return 0, err
	}

	if m.ID != e.ID {
		return 0, errors.New("signed for another node")
	}

	if m.Revision <= have {
		return 0, nil
	}

	data, err := o.Client.Node(ctx, addr, e.ID)

	if err != nil {
		return 0, err
	}

	return m.Revision, install(o, e.ID, m.Revision, data, key)
}

// install unpacks a node archive, checks it again, then swaps it in for the old copy.
func install(o PullOptions, id string, revision int, data []byte, key []byte) error {
	root := o.Root
	dir := filepath.Join(root, node.Dir)

	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- config repo folders are world-readable, like git clones
		return err
	}

	// Beside nodes/, not in it, so listing nodes never takes the half-written copy for a node.
	tmp, err := os.MkdirTemp(root, ".lan-*")

	if err != nil {
		return err
	}

	defer os.RemoveAll(tmp)

	if err := Extract(bytes.NewReader(data), tmp, DefaultLimits); err != nil {
		return err
	}

	m, err := VerifyFolder(tmp, key)

	if err != nil {
		return err
	}

	if m.ID != id || m.Revision != revision {
		return errors.New("archive differs from the signed index entry")
	}

	n, err := node.Load(tmp)

	if err != nil {
		return err
	}

	// A name equal to this machine's name or to any node id could make it pass for another machine.
	if !safeName(n.Name) || config.IsNodeID(n.Name) || n.Name == o.Settings.NodeName {
		return fmt.Errorf("refused node name %q", n.Name)
	}

	target := filepath.Join(dir, n.Name)

	if other, err := node.Load(target); err == nil && other.ID != id {
		return fmt.Errorf("another node is already called %s", n.Name)
	}

	return swap(root, id, tmp, target)
}

// swap replaces the node's current folder, wherever its name put it, with tmp at target.
func swap(root, id, tmp, target string) error {
	nodes, err := node.List(root)

	if err != nil {
		return err
	}

	for _, n := range nodes {
		if n.ID == id && n.Path != target {
			if err := os.RemoveAll(n.Path); err != nil {
				return err
			}
		}
	}

	old := tmp + ".old"

	if err := os.Rename(target, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(tmp, target); err != nil {
		_ = os.Rename(old, target)

		return err
	}

	return os.RemoveAll(old)
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.IsLocal(name) && !strings.ContainsAny(name, `/\`) && !strings.HasPrefix(name, ".")
}

func localRevisions(root string) (map[string]int, error) {
	nodes, err := node.List(root)

	if err != nil {
		return nil, err
	}

	revisions := map[string]int{}

	for _, n := range nodes {
		revisions[n.ID] = n.Revision
	}

	return revisions, nil
}

// mergeRecipes adds the peer's apps, repos and capture rules this config lacks, and newer removals; scripts need a yes.
func mergeRecipes(o PullOptions, data []byte, rep *PullReport) error {
	peer, err := config.Parse(data)

	if err != nil {
		return err
	}

	path := config.FilePath(o.Root)
	existing, err := os.ReadFile(path) // #nosec G304 -- this machine's own rehome.yml

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	local := &config.Config{}

	if len(existing) > 0 {
		if local, err = config.Parse(existing); err != nil {
			return err
		}
	}

	var apps []config.App
	repos := map[string]config.AptRepo{}

	for _, app := range peer.Apps {
		if needsConfirm(app, local) && (o.Confirm == nil || !o.Confirm(app)) {
			rep.Declined = append(rep.Declined, app.ID)

			continue
		}

		apps = append(apps, app)

		for _, spec := range app.Install {
			if r, ok := peer.AptRepos[spec.Repo]; ok && spec.Repo != "" {
				repos[spec.Repo] = r
			}
		}
	}

	merged := existing
	// Capture rules and legacy apt keys never come from peers: they could pull private files or trust any repo.
	incoming := &config.Config{Version: peer.Version, Apps: apps, AptRepos: repos}

	for _, key := range platformKeys(apps) {
		out, report, err := config.Merge(merged, incoming, key)

		if err != nil {
			return err
		}

		merged = out
		rep.RecipesAdded = append(rep.RecipesAdded, report.Added...)
	}

	if !bytes.Equal(merged, existing) {
		if err := os.WriteFile(path, merged, 0o644); err != nil { // #nosec G703 G306 -- this machine's own rehome.yml, shared and world-readable like the rest of the config repo
			return err
		}
	}

	return mergeRemovals(path, local, peer)
}

// needsConfirm reports whether merging app brings a method that runs code or adds a source this config lacks.
func needsConfirm(app config.App, local *config.Config) bool {
	have, ok := local.App(app.ID)

	if !ok && len(app.PostInstall) > 0 {
		return true
	}

	for key, spec := range app.Install {
		if !risky(spec) {
			continue
		}

		if !ok {
			return true
		}

		if _, has := have.Install[key]; !has {
			return true
		}
	}

	return false
}

// risky install methods run a script, a downloaded package or a third-party source.
func risky(spec config.InstallSpec) bool {
	return spec.Script != "" || spec.DebURL != "" || spec.Tap != "" || spec.Repo != "" || spec.RemoteURL != ""
}

func platformKeys(apps []config.App) []string {
	keys := map[string]bool{}

	for _, a := range apps {
		for k := range a.Install {
			keys[k] = true
		}
	}

	if len(keys) == 0 && len(apps) > 0 {
		keys[config.AnyPlatform] = true
	}

	return slices.Sorted(maps.Keys(keys))
}

// mergeRemovals keeps, for each app, the newest everywhere-removal of the two configs.
func mergeRemovals(path string, local, peer *config.Config) error {
	for _, t := range peer.Removed {
		if have, ok := local.Tombstone(t.ID); ok && !t.At.After(have.At) {
			continue
		}

		if _, err := os.Stat(path); err != nil {
			return nil
		}

		if err := config.AddRemoved(path, t); err != nil {
			return err
		}
	}

	return nil
}
