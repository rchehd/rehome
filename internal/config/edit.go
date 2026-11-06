package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const emptyConfig = "version: 2\napps: []\n"

// Edit changes the config file through its YAML tree, so comments and order survive; the
// result must validate before it is written.
func Edit(path string, fn func(root *yaml.Node) error) error {
	data, err := os.ReadFile(path)

	if errors.Is(err, os.ErrNotExist) {
		data, err = []byte(emptyConfig), nil
	}

	if err != nil {
		return err
	}

	var doc yaml.Node

	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("config root is not a mapping")
	}

	if err := fn(doc.Content[0]); err != nil {
		return err
	}

	out, err := encode(&doc)

	if err != nil {
		return err
	}

	cfg, err := Parse(out)

	if err != nil {
		return err
	}

	if errs := cfg.Validate(); len(errs) > 0 {
		return fmt.Errorf("change rejected, config would be invalid: %w", errors.Join(errs...))
	}

	return os.WriteFile(path, out, 0o644)
}

// AddInstall sets how an app installs on a platform key, creating the app if needed.
func AddInstall(path, id, platformKey string, spec InstallSpec, group string) error {
	return Edit(path, func(root *yaml.Node) error {
		app := ensureApp(root, id, group)
		install := child(app, "install")

		if install == nil {
			install = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setChild(app, "install", install)
		}

		var v yaml.Node

		if err := v.Encode(spec); err != nil {
			return err
		}

		v.Style = yaml.FlowStyle
		setChild(install, platformKey, &v)

		return nil
	})
}

// AddCapture sets a settings rule for an app and file key (linux, macos).
func AddCapture(path, id, fileKey string, rule FileRule) error {
	return Edit(path, func(root *yaml.Node) error {
		capture := child(root, "capture")

		if capture == nil {
			capture = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setChild(root, "capture", capture)
		}

		byKey := child(capture, id)

		if byKey == nil {
			byKey = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setChild(capture, id, byKey)
		}

		var v yaml.Node

		if err := v.Encode(rule); err != nil {
			return err
		}

		flowLeaves(&v)
		v.Style = yaml.FlowStyle
		setChild(byKey, fileKey, &v)

		return nil
	})
}

// SetApp changes simple app fields: enabled, skip (comma list), version, group; an empty
// value removes the field.
func SetApp(path, id string, fields map[string]string) error {
	return Edit(path, func(root *yaml.Node) error {
		app := appNode(appsSeq(root), id)

		if app == nil {
			return fmt.Errorf("no app %q in the config", id)
		}

		for key, value := range fields {
			if value == "" {
				removeChild(app, key)

				continue
			}

			node, err := fieldNode(key, value)

			if err != nil {
				return err
			}

			setChild(app, key, node)
		}

		return nil
	})
}

func fieldNode(key, value string) (*yaml.Node, error) {
	switch key {
	case "enabled":
		b, err := strconv.ParseBool(value)

		if err != nil {
			return nil, fmt.Errorf("enabled must be true or false, got %q", value)
		}

		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(b)}, nil
	case "skip":
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}

		for _, v := range strings.Split(value, ",") {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.TrimSpace(v)})
		}

		return seq, nil
	case "version", "group":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}, nil
	}

	return nil, fmt.Errorf("unknown field %q (enabled, skip, version, group)", key)
}

func appsSeq(root *yaml.Node) *yaml.Node {
	seq := child(root, "apps")

	if seq == nil || seq.Kind != yaml.SequenceNode {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setChild(root, "apps", seq)
	}

	return seq
}

func ensureApp(root *yaml.Node, id, group string) *yaml.Node {
	seq := appsSeq(root)

	if node := appNode(seq, id); node != nil {
		return node
	}

	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setChild(node, "id", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: id})

	if group != "" {
		setChild(node, "group", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: group})
	}

	seq.Style = 0
	insertInGroup(seq, node, group)

	return node
}

// RenameIgnore moves a node's ignore list to its new name; a config without one is left alone.
func RenameIgnore(path, from, to string) error {
	data, err := os.ReadFile(path)

	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	cfg, err := Parse(data)

	if err != nil || cfg.Ignore[from] == nil {
		return err
	}

	return Edit(path, func(root *yaml.Node) error {
		ignore := child(root, "ignore")

		for i := 0; i+1 < len(ignore.Content); i += 2 {
			if ignore.Content[i].Value == from {
				ignore.Content[i].Value = to
			}
		}

		return nil
	})
}

// AddRemoved records an everywhere-removal, replacing an older one for the same app.
func AddRemoved(path string, t Tombstone) error {
	return Edit(path, func(root *yaml.Node) error {
		var n yaml.Node

		if err := n.Encode(t); err != nil {
			return err
		}

		seq := removedSeq(root, true)
		seq.Content = append(slices.DeleteFunc(seq.Content, func(e *yaml.Node) bool { return entryID(e) == t.ID }), &n)

		return nil
	})
}

// DropRemoved lifts an app's everywhere-removal.
func DropRemoved(path, id string) error {
	return Edit(path, func(root *yaml.Node) error {
		if seq := removedSeq(root, false); seq != nil {
			seq.Content = slices.DeleteFunc(seq.Content, func(e *yaml.Node) bool { return entryID(e) == id })
		}

		return nil
	})
}

func removedSeq(root *yaml.Node, create bool) *yaml.Node {
	if seq := child(root, "removed"); seq != nil || !create {
		return seq
	}

	seq := &yaml.Node{Kind: yaml.SequenceNode}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "removed"}, seq)

	return seq
}

func entryID(e *yaml.Node) string {
	if id := child(e, "id"); id != nil {
		return id.Value
	}

	return ""
}
