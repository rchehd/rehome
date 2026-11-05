package sessions

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/rchehd/rehome/internal/secrets"
)

type transformer struct {
	redact                 bool
	sourceHome, targetHome string
	paths                  map[string]string
	findings               []secrets.Finding
	ref                    func(string) (string, error)
}

func ValidatePaths(paths map[string]string) error {
	for from, to := range paths {
		if !filepath.IsAbs(from) || !filepath.IsAbs(to) {
			return errors.New("--session-path-map requires absolute source and destination paths")
		}
	}
	return nil
}

func (t *transformer) text(key, value string) (string, error) {
	if t.ref != nil && len(value) == 64 {
		var err error
		value, err = t.ref(value)
		if err != nil {
			return "", err
		}
	}
	if t.sourceHome != "" {
		paths := map[string]string{t.sourceHome: t.targetHome}
		for from, to := range t.paths {
			paths[filepath.Clean(from)] = filepath.Clean(to)
		}
		var keys []string
		for from := range paths {
			keys = append(keys, from)
		}
		sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
		var pairs []string
		for _, from := range keys {
			if value == from {
				return paths[from], nil
			}
			pairs = append(pairs, from+"/", paths[from]+"/")
		}
		value = strings.NewReplacer(pairs...).Replace(value)
	}
	if !t.redact {
		return value, nil
	}
	raw, err := json.Marshal(map[string]string{key: value})
	if err != nil {
		return "", err
	}
	clean, findings := secrets.Redact("session.json", raw)
	t.findings = append(t.findings, findings...)
	var result map[string]string
	if err := json.Unmarshal(clean, &result); err != nil {
		return "", fmt.Errorf("redact session text: %w", err)
	}
	for _, text := range result {
		return text, nil
	}
	return "", errors.New("redacted session text is missing")
}

func (t *transformer) value(key string, value any) (any, error) {
	switch v := value.(type) {
	case string:
		return t.text(key, v)
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			next, err := t.value(key, item)
			if err != nil {
				return nil, err
			}
			result[i] = next
		}
		return result, nil
	case map[string]any:
		next := map[string]any{}
		for name, item := range v {
			newKey, err := t.text("label", name)
			if err != nil {
				return nil, err
			}
			if _, exists := next[newKey]; exists {
				return nil, errors.New("path mapping creates duplicate session keys")
			}
			newValue, err := t.value(name, item)
			if err != nil {
				return nil, err
			}
			next[newKey] = newValue
		}
		return next, nil
	}
	return value, nil
}

func (t *transformer) json(data []byte) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	value, err := t.value("text", value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func (s *Snapshot) transform(t *transformer) error {
	key, hasKey := s.Meta["blobEncryptionKey"]
	if s.Format == "cursor" {
		// Cursor's per-session encryption key is part of its resumable history.
		s.Meta = maps.Clone(s.Meta)
		delete(s.Meta, "blobEncryptionKey")
	}
	var err error
	s.Project, err = t.text("cwd", s.Project)
	if err != nil {
		return err
	}
	for i, line := range s.Lines {
		s.Lines[i], err = t.json(line)
		if err != nil {
			return err
		}
	}
	for _, object := range []*map[string]any{&s.Thread, &s.Meta, &s.Sidecar} {
		if *object == nil {
			continue
		}
		value, err := t.value("metadata", *object)
		if err != nil {
			return err
		}
		*object = value.(map[string]any)
	}
	if s.Format == "cursor" && hasKey {
		s.Meta["blobEncryptionKey"] = key
	}
	if len(s.Blobs) > 0 {
		return s.transformBlobs(t)
	}
	return nil
}

func (t *transformer) blob(data []byte, depth int) ([]byte, error) {
	if depth > 64 {
		return nil, errors.New("session protobuf nesting exceeds 64 levels")
	}
	if len(data) == 32 && t.ref != nil {
		old := hex.EncodeToString(data)
		next, err := t.ref(old)
		if err != nil {
			return nil, err
		}
		if next != old {
			return hex.DecodeString(next)
		}
	}
	if json.Valid(data) {
		return t.json(data)
	}
	if fields, ok := wireFields(data); ok {
		var out []byte
		for _, f := range fields {
			out = append(out, f.key...)
			if f.kind != 2 {
				out = append(out, f.data...)
				continue
			}
			value, err := t.blob(f.data, depth+1)
			if err != nil {
				return nil, err
			}
			out = binary.AppendUvarint(out, uint64(len(value)))
			out = append(out, value...)
		}
		return out, nil
	}
	if utf8.Valid(data) && !bytes.ContainsRune(data, '\x00') {
		text, err := t.text("text", string(data))
		return []byte(text), err
	}
	return data, nil
}

type wireField struct {
	key, data []byte
	kind      uint64
}

func wireFields(data []byte) ([]wireField, bool) {
	var fields []wireField
	for len(data) > 0 {
		key, n := binary.Uvarint(data)
		if n <= 0 || key>>3 == 0 || key>>3 > 1<<29-1 {
			return nil, false
		}
		f := wireField{key: data[:n], kind: key & 7}
		data = data[n:]
		length := 0
		switch f.kind {
		case 0:
			_, length = binary.Uvarint(data)
			if length <= 0 {
				return nil, false
			}
		case 1:
			length = 8
		case 5:
			length = 4
		case 2:
			count, n := binary.Uvarint(data)
			if n <= 0 || count > uint64(len(data)-n) {
				return nil, false
			}
			data = data[n:]
			length = int(count)
		default:
			return nil, false
		}
		if length > len(data) {
			return nil, false
		}
		f.data = data[:length]
		fields = append(fields, f)
		data = data[length:]
	}
	return fields, len(fields) > 0
}
