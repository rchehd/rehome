package install

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/node"
)

// Request names what to install: IDs, or every app the nodes have when IDs is empty.
// From limits the sources to one node; Prefer puts nodes first when several have an app.
type Request struct {
	IDs        []string
	From       *node.Node
	Prefer     []*node.Node
	NoSettings bool
}

// Choice is one app and the node it comes from, or why it is left out.
type Choice struct {
	ID     string `json:"id"`
	Node   string `json:"node,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Pick is what Select decided: the apps to plan, where each comes from, and the versions and
// settings of those nodes.
type Pick struct {
	IDs     []string
	Taken   []Choice
	Skipped []Choice
	Lock    *lock.Lock
	// Files is the node whose settings a group takes, or nil for none.
	Files func(group string) *node.Node
}

// Select resolves a request against the recipes and the nodes (live ones, self included).
// Sources are ordered by Prefer, then nodes of this platform, then the most recently seen;
// versions come from the preferred node that has one, otherwise the newest any node has.
func Select(cfg *config.Config, nodes []*node.Node, platformKey, fileKey string, req Request) (*Pick, error) {
	order := sources(nodes, platformKey, req)
	recipes := map[string]config.App{}

	for _, a := range cfg.Apps {
		recipes[a.ID] = a
	}

	pick := &Pick{Lock: versions(order, req.Prefer), Files: settingsFrom(order, fileKey, req)}

	if len(req.IDs) > 0 {
		for _, id := range req.IDs {
			if _, ok := recipes[id]; !ok {
				return nil, unknownApp(id, cfg, nodes)
			}

			from := owner(order, id)

			if req.From != nil && from == nil {
				return nil, fmt.Errorf("node %s does not have %s", req.From.Name, id)
			}

			pick.take(id, from)
		}

		return pick, nil
	}

	for _, id := range candidates(order, recipes, nodes, req.From == nil) {
		app, ok := recipes[id]

		switch {
		case !ok:
			pick.Skipped = append(pick.Skipped, Choice{ID: id, Node: nameOf(owner(order, id)), Reason: "no recipe in rehome.yml"})
		case !app.IsEnabled():
			pick.Skipped = append(pick.Skipped, Choice{ID: id, Node: nameOf(owner(order, id)), Reason: "disabled in rehome.yml"})
		case removed(cfg, id) != "":
			pick.Skipped = append(pick.Skipped, Choice{ID: id, Node: nameOf(owner(order, id)), Reason: removed(cfg, id)})
		default:
			pick.take(id, owner(order, id))
		}
	}

	return pick, nil
}

func removed(cfg *config.Config, id string) string {
	if t, ok := cfg.Tombstone(id); ok {
		return "removed everywhere by " + t.By + " on " + t.At.Format(time.DateOnly)
	}

	return ""
}

func (p *Pick) take(id string, from *node.Node) {
	p.IDs = append(p.IDs, id)
	p.Taken = append(p.Taken, Choice{ID: id, Node: nameOf(from)})
}

// From maps each taken app to the node it comes from.
func (p *Pick) From() map[string]string {
	from := map[string]string{}

	for _, c := range p.Taken {
		if c.Node != "" {
			from[c.ID] = c.Node
		}
	}

	return from
}

func sources(nodes []*node.Node, platformKey string, req Request) []*node.Node {
	if req.From != nil {
		return []*node.Node{req.From}
	}

	rest := slices.DeleteFunc(slices.Clone(nodes), func(n *node.Node) bool { return slices.Contains(req.Prefer, n) })
	slices.SortStableFunc(rest, func(a, b *node.Node) int {
		if sa, sb := a.Platform == platformKey, b.Platform == platformKey; sa != sb {
			if sa {
				return -1
			}

			return 1
		}

		return b.LastSeen.Compare(a.LastSeen)
	})

	return append(slices.Clone(req.Prefer), rest...)
}

// candidates is every app the source nodes have, plus, unless from is set, recipes no node has
// at all (a hand-written config nobody scanned yet).
func candidates(order []*node.Node, recipes map[string]config.App, all []*node.Node, unclaimed bool) []string {
	seen := map[string]bool{}

	for _, n := range order {
		for id, e := range n.Apps {
			if !e.Unsupported {
				seen[id] = true
			}
		}
	}

	if unclaimed {
		for id := range recipes {
			if owner(all, id) == nil {
				seen[id] = true
			}
		}
	}

	return slices.Sorted(maps.Keys(seen))
}

func owner(order []*node.Node, id string) *node.Node {
	for _, n := range order {
		if e, ok := n.Apps[id]; ok && !e.Unsupported {
			return n
		}
	}

	return nil
}

func nameOf(n *node.Node) string {
	if n == nil {
		return ""
	}

	return n.Name
}

// versions is the newest version per platform among the sources, overridden by the first
// preferred node that recorded one.
func versions(order, prefer []*node.Node) *lock.Lock {
	lk := node.Versions(order)

	for _, n := range slices.Backward(prefer) {
		for id, e := range n.Apps {
			if !e.Unsupported && (e.Version != "" || e.Commit != "") {
				lk.Set(id, n.Platform, e)
			}
		}
	}

	return lk
}

// settingsFrom takes a group from --from, else the first preferred node with it, else the most
// recently seen node with it, preferring nodes whose settings paths fit this OS.
func settingsFrom(order []*node.Node, fileKey string, req Request) func(string) *node.Node {
	return func(group string) *node.Node {
		has := func(n *node.Node) bool { _, ok := n.Files[group]; return ok }

		switch {
		case req.NoSettings:
			return nil
		case req.From != nil:
			if has(req.From) {
				return req.From
			}

			return nil
		}

		for _, n := range req.Prefer {
			if has(n) {
				return n
			}
		}

		if n := node.Freshest(order, func(n *node.Node) bool { return has(n) && n.FileKey == fileKey }); n != nil {
			return n
		}

		return node.Freshest(order, has)
	}
}

func unknownApp(id string, cfg *config.Config, nodes []*node.Node) error {
	known := map[string]bool{}

	for _, a := range cfg.Apps {
		known[a.ID] = true
	}

	for _, n := range nodes {
		for k := range n.Apps {
			known[k] = true
		}
	}

	if near := Suggest(id, slices.Collect(maps.Keys(known))); near != "" {
		return fmt.Errorf("unknown app %q; did you mean %s?", id, near)
	}

	return fmt.Errorf("unknown app %q: no recipe in rehome.yml", id)
}

// Suggest is the known id closest to id, if it is close enough to be a typo.
func Suggest(id string, known []string) string {
	best, dist := "", len(id)/2+1

	for _, k := range slices.Sorted(slices.Values(known)) {
		if d := distance(strings.ToLower(id), strings.ToLower(k)); d < dist {
			best, dist = k, d
		}
	}

	return best
}

// distance is the Levenshtein edit distance.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i

		for j := 1; j <= len(b); j++ {
			cost := 1

			if a[i-1] == b[j-1] {
				cost = 0
			}

			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}

		prev = cur
	}

	return prev[len(b)]
}
