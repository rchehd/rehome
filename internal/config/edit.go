package config

import (
	"errors"
	"fmt"
	"os"
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
		removeChild(app, missingKey)

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
