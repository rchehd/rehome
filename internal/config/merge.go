package config

import (
	"bytes"
	"fmt"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"
)

const missingKey = "missing_on_scan"

type MergeReport struct {
	Added         []string
	MissingOnScan []string
	Returned      []string
	Updated       []string
	Unchanged     []string
}

func (r MergeReport) Changed() bool {
	return len(r.Added)+len(r.MissingOnScan)+len(r.Returned)+len(r.Updated) > 0
}

// Merge folds a scan into the config without touching user edits; see mergeApps for the rules.
func Merge(existing []byte, scanned *Config, platformKey string) ([]byte, MergeReport, error) {
	var report MergeReport

	if len(bytes.TrimSpace(existing)) == 0 {
		for _, a := range scanned.Apps {
			report.Added = append(report.Added, a.ID)
		}

		out, err := Marshal(scanned)

		return out, report, err
	}

	var doc yaml.Node

	if err := yaml.Unmarshal(existing, &doc); err != nil {
		return nil, report, err
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, report, fmt.Errorf("config root is not a mapping")
	}

	root := doc.Content[0]
	changed, err := mergeApps(root, scanned, platformKey, &report)

	if err != nil {
		return nil, report, err
	}

	reposChanged, err := mergeMap(root, "apt_repos", scanned.AptRepos)

	if err != nil {
		return nil, report, err
	}

	reposUpdated, err := completeRepos(root, scanned.AptRepos)

	if err != nil {
		return nil, report, err
	}

	keysChanged := mergeList(root, "apt_keys", scanned.AptKeys)
	reposChanged = reposChanged || reposUpdated || keysChanged

	filesChanged, err := mergeFiles(root, scanned.Files)

	if err != nil {
		return nil, report, err
	}

	if !changed && !reposChanged && !filesChanged {
		return existing, report, nil
	}

	out, err := encode(&doc)

	return out, report, err
}

// Marshal renders a config in the compact style used by generated files.
func Marshal(cfg *Config) ([]byte, error) {
	var n yaml.Node

	if err := n.Encode(cfg); err != nil {
		return nil, err
	}

	flowLeaves(&n)

	return encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{&n}})
}

func mergeApps(root *yaml.Node, scanned *Config, platformKey string, report *MergeReport) (bool, error) {
	seq := child(root, "apps")

	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setChild(root, "apps", seq)
	}

	var existing Config

	if err := root.Decode(&existing); err != nil {
		return false, err
	}

	found := map[string]bool{}
	changed := false

	for _, a := range scanned.Apps {
		found[a.ID] = true
		node := appNode(seq, a.ID)

		if node == nil {
			n, err := encodeApp(a)

			if err != nil {
				return false, err
			}

			insertInGroup(seq, n, a.Group)
			report.Added = append(report.Added, a.ID)
			changed = true

			continue
		}

		returned := removeChild(node, missingKey)
		updated, err := addMissingFields(node, a, platformKey)

		if err != nil {
			return false, err
		}

		switch {
		case returned:
			report.Returned = append(report.Returned, a.ID)
		case updated:
			report.Updated = append(report.Updated, a.ID)
		default:
			report.Unchanged = append(report.Unchanged, a.ID)
		}

		changed = changed || returned || updated
	}

	for _, a := range existing.Apps {
		if found[a.ID] || a.MissingOnScan || !belongsTo(a, platformKey) {
			continue
		}

		setChild(appNode(seq, a.ID), missingKey, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		report.MissingOnScan = append(report.MissingOnScan, a.ID)
		changed = true
	}

	return changed, nil
}

// addMissingFields only adds what the user's entry lacks: a settings group, or an install
// method for this platform (a config made on Linux, rescanned on a Mac); it never edits.
func addMissingFields(node *yaml.Node, a App, platformKey string) (bool, error) {
	updated := false

	if a.Files != "" && child(node, "files") == nil {
		setChild(node, "files", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: a.Files})
		updated = true
	}

	spec, ok := a.Install[platformKey]

	if !ok {
		return updated, nil
	}

	install := child(node, "install")

	if install == nil {
		install = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setChild(node, "install", install)
	}

	if child(install, platformKey) != nil {
		return updated, nil
	}

	var v yaml.Node

	if err := v.Encode(spec); err != nil {
		return false, err
	}

	v.Style = yaml.FlowStyle
	setChild(install, platformKey, &v)

	return true, nil
}

// belongsTo reports whether a scan on this platform is expected to find the app.
func belongsTo(a App, platformKey string) bool {
	if len(a.Install) == 0 {
		return len(a.Detected) > 0
	}

	_, ok := a.Spec(platformKey)

	return ok
}

func mergeMap[V any](root *yaml.Node, key string, scanned map[string]V) (bool, error) {
	if len(scanned) == 0 {
		return false, nil
	}

	m := child(root, key)

	if m == nil {
		m = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setChild(root, key, m)
	}

	changed := false

	for _, k := range sortedKeys(scanned) {
		if child(m, k) != nil {
			continue
		}

		var v yaml.Node

		if err := v.Encode(scanned[k]); err != nil {
			return false, err
		}

		flowLeaves(&v)
		setChild(m, k, &v)
		changed = true
	}

	return changed, nil
}

// completeRepos adds a missing release or keyrings to repo entries made by older scans.
func completeRepos(root *yaml.Node, scanned map[string]AptRepo) (bool, error) {
	repos := child(root, "apt_repos")
	changed := false

	for _, name := range sortedKeys(scanned) {
		node := child(repos, name)

		if node == nil || node.Kind != yaml.MappingNode {
			continue
		}

		s := scanned[name]

		if s.Release != "" && child(node, "release") == nil {
			setChild(node, "release", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s.Release})
			changed = true
		}

		if len(s.Keyrings) > 0 && child(node, "keyring") == nil && child(node, "keyrings") == nil {
			var v yaml.Node

			if err := v.Encode(s.Keyrings); err != nil {
				return false, err
			}

			v.Style = yaml.FlowStyle
			setChild(node, "keyrings", &v)
			changed = true
		}
	}

	return changed, nil
}

func mergeList(root *yaml.Node, key string, scanned []string) bool {
	if len(scanned) == 0 {
		return false
	}

	seq := child(root, key)

	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setChild(root, key, seq)
	}

	have := map[string]bool{}

	for _, item := range seq.Content {
		have[item.Value] = true
	}

	changed := false

	for _, v := range scanned {
		if !have[v] {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
			changed = true
		}
	}

	return changed
}

func mergeFiles(root *yaml.Node, scanned map[string][]FileMap) (bool, error) {
	changed, err := mergeMap(root, "files", scanned)

	if err != nil {
		return false, err
	}

	groups := child(root, "files")

	if groups == nil {
		return changed, nil
	}

	for _, name := range sortedKeys(scanned) {
		seq := child(groups, name)

		if seq == nil || seq.Kind != yaml.SequenceNode {
			continue
		}

		var have []FileMap

		if err := seq.Decode(&have); err != nil {
			return false, err
		}

		known := map[string]bool{}

		for _, f := range have {
			known[f.Src] = true
		}

		for _, f := range scanned[name] {
			if known[f.Src] {
				continue
			}

			var v yaml.Node

			if err := v.Encode(f); err != nil {
				return false, err
			}

			flowLeaves(&v)
			v.Style = yaml.FlowStyle
			seq.Content = append(seq.Content, &v)
			changed = true
		}
	}

	return changed, nil
}

func encodeApp(a App) (*yaml.Node, error) {
	var n yaml.Node

	if err := n.Encode(a); err != nil {
		return nil, err
	}

	flowLeaves(&n)

	return &n, nil
}

// insertInGroup places n after the last app of the same group, or at the end.
func insertInGroup(seq *yaml.Node, n *yaml.Node, group string) {
	at := len(seq.Content)

	for i, item := range seq.Content {
		if g := child(item, "group"); g != nil && g.Value == group {
			at = i + 1
		}
	}

	seq.Content = append(seq.Content[:at], append([]*yaml.Node{n}, seq.Content[at:]...)...)
}

func appNode(seq *yaml.Node, id string) *yaml.Node {
	for _, item := range seq.Content {
		if v := child(item, "id"); v != nil && v.Value == id {
			return item
		}
	}

	return nil
}

// flowLeaves renders mappings whose values are all scalars inline: { apt: htop }.
func flowLeaves(n *yaml.Node) {
	for _, c := range n.Content {
		flowLeaves(c)
	}

	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		return
	}

	for i := 1; i < len(n.Content); i += 2 {
		if n.Content[i].Kind != yaml.ScalarNode {
			return
		}
	}

	if !hasKey(n, "id") {
		n.Style = yaml.FlowStyle
	}
}

func child(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	return nil
}

func hasKey(m *yaml.Node, key string) bool {
	return child(m, key) != nil
}

func setChild(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = v

			return
		}
	}

	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	m.Content = append(m.Content, k, v)
}

func removeChild(m *yaml.Node, key string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)

			return true
		}
	}

	return false
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

func encode(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(doc); err != nil {
		return nil, err
	}

	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
