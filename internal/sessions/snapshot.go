package sessions

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/klauspost/compress/zstd"
	"github.com/rchehd/rehome/internal/secrets"
)

var ErrEmpty = errors.New("session has no saved history")

type Snapshot struct {
	Redacted bool              `json:"redacted,omitempty"`
	Version  int               `json:"version"`
	Format   string            `json:"format"`
	Relative string            `json:"relative"`
	Home     string            `json:"home"`
	Project  string            `json:"project"`
	Lines    []json.RawMessage `json:"lines,omitempty"`
	Thread   map[string]any    `json:"thread,omitempty"`
	Meta     map[string]any    `json:"meta,omitempty"`
	Sidecar  map[string]any    `json:"sidecar,omitempty"`
	Blobs    map[string][]byte `json:"blobs,omitempty"`
}

func Root(format string) string {
	home, _ := os.UserHomeDir()
	switch format {
	case "claude-code":
		if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
			return dir
		}
		return filepath.Join(home, ".claude")
	case "codex":
		if dir := os.Getenv("CODEX_HOME"); dir != "" {
			return dir
		}
		return filepath.Join(home, ".codex")
	case "cursor":
		if dir := os.Getenv("CURSOR_CONFIG_DIR"); strings.TrimSpace(dir) != "" {
			return dir
		}
		if dir := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(dir) != "" {
			return filepath.Join(dir, "cursor")
		}
		return filepath.Join(home, ".cursor")
	}
	return ""
}

func Read(ctx context.Context, format, path string, paths map[string]string) (*Snapshot, []secrets.Finding, error) {
	root := Root(format)
	if root == "" {
		return nil, nil, errors.New("unknown session format")
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, nil, errors.New("session outside its storage directory")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	s := &Snapshot{Version: 1, Format: format, Relative: filepath.ToSlash(rel), Home: home}
	if format == "cursor" {
		if err := s.readCursor(ctx, path, paths); err != nil {
			return nil, nil, err
		}
	} else {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		if format == "codex" && strings.HasSuffix(path, ".jsonl.zst") {
			decoder, err := zstd.NewReader(bytes.NewReader(data), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(128<<20))
			if err != nil {
				return nil, nil, err
			}
			data, err = io.ReadAll(io.LimitReader(decoder, (128<<20)+1))
			decoder.Close()
			if err != nil {
				return nil, nil, err
			}
			if len(data) > 128<<20 {
				return nil, nil, errors.New("decompressed Codex session exceeds 128 MiB")
			}
			s.Relative = strings.TrimSuffix(s.Relative, ".zst")
		}
		for _, line := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			if !json.Valid(line) {
				return nil, nil, errors.New("session contains an incomplete or invalid JSONL record; retry after the agent finishes writing")
			}
			s.Lines = append(s.Lines, json.RawMessage(line))
			var record struct {
				Cwd     string `json:"cwd"`
				Payload struct {
					Cwd string `json:"cwd"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(line, &record); err != nil {
				return nil, nil, err
			}
			if s.Project == "" {
				s.Project = record.Cwd
			}
			if s.Project == "" {
				s.Project = record.Payload.Cwd
			}
		}
		if format == "codex" {
			s.Thread, err = readCodexThread(ctx, root, s.ID())
			if err != nil {
				return nil, nil, err
			}
		}
		if len(s.Lines) == 0 {
			return nil, nil, ErrEmpty
		}
	}
	if err := s.Validate(); err != nil {
		return nil, nil, err
	}
	t := transformer{redact: true}
	if err := s.transform(&t); err != nil {
		return nil, nil, err
	}
	s.Redacted = len(t.findings) > 0
	return s, t.findings, nil
}

var sessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func (s *Snapshot) ID() string {
	if s.Format == "cursor" {
		return filepath.Base(filepath.Dir(s.Relative))
	}
	for _, line := range s.Lines {
		var record struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
			Payload   struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &record) == nil {
			if record.Type == "session_meta" && record.Payload.ID != "" {
				return record.Payload.ID
			}
			if record.SessionID != "" {
				return record.SessionID
			}
		}
	}
	return ""
}

func (s *Snapshot) Validate() error {
	if s.Version != 1 || !filepath.IsLocal(s.Relative) || path.Clean(s.Relative) != s.Relative || strings.Contains(s.Relative, `\`) || !filepath.IsAbs(s.Home) || !filepath.IsAbs(s.Project) || !sessionID.MatchString(s.ID()) {
		return errors.New("invalid session snapshot metadata")
	}
	parts := strings.Split(filepath.ToSlash(s.Relative), "/")
	switch s.Format {
	case "claude-code":
		if len(parts) < 3 || parts[0] != "projects" || !strings.HasSuffix(s.Relative, ".jsonl") {
			return errors.New("invalid Claude session path")
		}
	case "codex":
		if len(parts) < 2 || (parts[0] != "sessions" && parts[0] != "archived_sessions") || !strings.HasSuffix(s.Relative, ".jsonl") {
			return errors.New("invalid Codex session path")
		}
	case "cursor":
		validPath := len(parts) == 4 && parts[0] == "chats" && parts[3] == "store.db" || len(parts) == 3 && parts[0] == "acp-sessions" && parts[2] == "store.db"
		if !validPath || len(s.Meta) == 0 || len(s.Blobs) == 0 {
			return errors.New("invalid Cursor session snapshot")
		}
	default:
		return errors.New("unknown session format")
	}
	return nil
}

func (s *Snapshot) Destination() string {
	parts := strings.Split(s.Relative, "/")
	switch s.Format {
	case "claude-code":
		parts[1] = claudeProjectDir(s.Project)
	case "cursor":
		if parts[0] == "chats" {
			hash := md5.Sum([]byte(filepath.Clean(s.Project)))
			parts[1] = hex.EncodeToString(hash[:])
		}
	}
	return filepath.Join(append([]string{Root(s.Format)}, parts...)...)
}

func claudeProjectDir(project string) string {
	var name strings.Builder
	var hash int32
	for _, char := range utf16.Encode([]rune(project)) {
		hash = hash*31 + int32(char)
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			name.WriteByte(byte(char))
		} else {
			name.WriteByte('-')
		}
	}
	if name.Len() <= 200 {
		return name.String()
	}
	value := int64(hash)
	if value < 0 {
		value = -value
	}
	return name.String()[:200] + "-" + strconv.FormatInt(value, 36)
}

func (s *Snapshot) Restore(ctx context.Context, dryRun bool, paths map[string]string) (string, bool, error) {
	copy := *s
	copy.Lines = append([]json.RawMessage(nil), s.Lines...)
	s = &copy
	if err := ValidatePaths(paths); err != nil {
		return "", false, err
	}
	if err := s.Validate(); err != nil {
		return "", false, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	if err := s.transform(&transformer{sourceHome: s.Home, targetHome: home, paths: paths}); err != nil {
		return "", false, err
	}
	dest := s.Destination()
	if _, err := os.Lstat(dest); err == nil {
		if s.Format == "codex" && !dryRun {
			if err := s.indexCodex(ctx, dest); err != nil {
				return dest, false, err
			}
		}
		return dest, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return dest, false, err
	}
	if dryRun {
		return dest, true, nil
	}
	parent := filepath.Dir(dest)
	if s.Format == "cursor" {
		parent = filepath.Dir(parent)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return dest, false, err
	}
	stage, err := os.MkdirTemp(parent, ".rehome-session-")
	if err != nil {
		return dest, false, err
	}
	defer os.RemoveAll(stage)
	file := filepath.Join(stage, filepath.Base(dest))
	if s.Format == "cursor" {
		if err := s.writeCursor(ctx, file); err != nil {
			return dest, false, err
		}
		metadata, err := json.Marshal(s.Sidecar)
		if err != nil {
			return dest, false, err
		}
		if err := os.WriteFile(filepath.Join(stage, "meta.json"), metadata, 0o600); err != nil {
			return dest, false, err
		}
		if err := os.Rename(stage, filepath.Dir(dest)); err != nil {
			if _, exists := os.Stat(dest); exists == nil {
				return dest, false, nil
			}
			return dest, false, err
		}
		return dest, true, nil
	}
	var data bytes.Buffer
	for _, line := range s.Lines {
		data.Write(line)
		data.WriteByte('\n')
	}
	if err := os.WriteFile(file, data.Bytes(), 0o600); err != nil {
		return dest, false, err
	}
	if err := os.Link(file, dest); errors.Is(err, os.ErrExist) {
		return dest, false, nil
	} else if err != nil {
		return dest, false, err
	}
	if s.Format == "codex" {
		if err := s.indexCodex(ctx, dest); err != nil {
			os.Remove(dest)
			return dest, false, fmt.Errorf("index imported Codex session: %w", err)
		}
	}
	return dest, true, nil
}
