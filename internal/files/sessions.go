package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/sessions"
)

func CaptureSessions(ctx context.Context, entry *catalog.Entry, key, root string, paths map[string]string) ([]Change, []config.FileMap, []error) {
	rule, ok := entry.Files[key]
	if !ok {
		return nil, nil, nil
	}
	base := sessions.Root(entry.SessionFormat)
	rels, err := Select(base, rule)
	if err != nil {
		return nil, nil, []error{err}
	}
	var changes []Change
	var mappings []config.FileMap
	var warnings []error
	for _, rel := range rels {
		src := filepath.Join(base, rel)
		info, err := os.Stat(src)
		if err != nil {
			warnings = append(warnings, err)
			continue
		}
		if info.Size() > int64(rule.MaxSizeMB)<<20 {
			warnings = append(warnings, fmt.Errorf("%s exceeds the session limit of %d MiB", src, rule.MaxSizeMB))
			continue
		}
		snapshot, findings, err := sessions.Read(ctx, entry.SessionFormat, src, paths)
		if errors.Is(err, sessions.ErrEmpty) {
			continue
		}
		if err != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", src, err))
			continue
		}
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			warnings = append(warnings, err)
			continue
		}
		identity := snapshot.Relative
		parts := strings.Split(identity, "/")
		if entry.SessionFormat == "claude-code" {
			identity = strings.Join(parts[2:], "/")
		}
		if entry.SessionFormat == "cursor" {
			identity = parts[0] + "/" + snapshot.ID()
		}
		hash := sha256.Sum256([]byte(identity))
		dst := fmt.Sprintf("files/%s/%x.json", entry.ID, hash[:12])
		mappings = append(mappings, config.FileMap{Src: dst, Session: entry.SessionFormat})
		old, _ := os.ReadFile(filepath.Join(root, dst))
		if bytes.Equal(old, data) {
			continue
		}
		for i := range findings {
			findings[i].Path = src
		}
		changes = append(changes, Change{Src: src, Dst: dst, Old: old, New: data, Mode: 0o600, Findings: findings})
	}
	return changes, mappings, warnings
}

func applySession(ctx context.Context, root string, m config.FileMap, opts ApplyOptions) ApplyResult {
	res := ApplyResult{Src: m.Src}
	data, err := os.ReadFile(filepath.Join(root, m.Src))
	if os.IsNotExist(err) {
		res.Status = StatusNoSource
		return res
	}
	var snapshot sessions.Snapshot
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		err = decoder.Decode(&snapshot)
	}
	if err == nil && snapshot.Format != m.Session {
		err = fmt.Errorf("session snapshot format differs from its mapping")
	}
	var copied bool
	if err == nil {
		res.Dest, copied, err = snapshot.Restore(ctx, opts.DryRun, opts.SessionPaths)
	}
	if err != nil {
		res.Status, res.Problem = StatusSessionError, err.Error()
		return res
	}
	res.Status = StatusKeptLocal
	if copied {
		res.Status = StatusCopied
		res.Redacted = snapshot.Redacted
	}
	return res
}
