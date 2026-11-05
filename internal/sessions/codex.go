package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func codexDB(root string) string {
	paths, _ := filepath.Glob(filepath.Join(root, "state_*.sqlite"))
	sort.Slice(paths, func(i, j int) bool {
		version := func(path string) int {
			n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "state_"), ".sqlite"))
			return n
		}
		return version(paths[i]) > version(paths[j])
	})
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func readCodexThread(ctx context.Context, root, id string) (map[string]any, error) {
	path := codexDB(root)
	if path == "" {
		return nil, nil
	}
	db, err := openDB(path, "ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT * FROM threads WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]any, len(columns))
	refs := make([]any, len(columns))
	for i := range values {
		refs[i] = &values[i]
	}
	if err := rows.Scan(refs...); err != nil {
		return nil, err
	}
	result := map[string]any{}
	for i, key := range columns {
		result[key] = values[i]
	}
	return result, nil
}

func (s *Snapshot) indexCodex(ctx context.Context, dest string) error {
	path := codexDB(Root("codex"))
	if path == "" {
		return nil
	}
	db, err := openDB(path, "rw")
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM threads WHERE id=?", s.ID()).Scan(&exists)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	values := s.Thread
	if values == nil {
		values = s.codexMetadata()
	}
	values["id"], values["rollout_path"], values["cwd"] = s.ID(), dest, s.Project
	delete(values, "project_id")
	delete(values, "thread_section_id")
	delete(values, "attached_thread_parent_id")
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info(threads)")
	if err != nil {
		return err
	}
	var columns, placeholders []string
	var args []any
	for rows.Next() {
		var number, notNull, primary int
		var name, kind string
		var fallback any
		if err := rows.Scan(&number, &name, &kind, &notNull, &fallback, &primary); err != nil {
			rows.Close()
			return err
		}
		value, ok := values[name]
		if !ok {
			if notNull != 0 && fallback == nil {
				rows.Close()
				return fmt.Errorf("target Codex schema requires missing column %s", name)
			}
			continue
		}
		if n, ok := value.(json.Number); ok {
			value = string(n)
		}
		columns = append(columns, `"`+strings.ReplaceAll(name, `"`, `""`)+`"`)
		placeholders = append(placeholders, "?")
		args = append(args, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	query := "INSERT INTO threads (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ") ON CONFLICT(id) DO NOTHING"
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Snapshot) codexMetadata() map[string]any {
	created := time.Now().Unix()
	var updated int64
	seenUser := false
	provider := "openai"
	source := "cli"
	title := "Imported session"
	for _, line := range s.Lines {
		var record struct {
			Type      string         `json:"type"`
			Timestamp string         `json:"timestamp"`
			Payload   map[string]any `json:"payload"`
		}
		if json.Unmarshal(line, &record) != nil {
			continue
		}
		if timestamp, err := time.Parse(time.RFC3339Nano, record.Timestamp); err == nil && timestamp.Unix() > updated {
			updated = timestamp.Unix()
		}
		if record.Type == "session_meta" {
			if timestamp, err := time.Parse(time.RFC3339Nano, record.Timestamp); err == nil {
				created = timestamp.Unix()
			}
			if v, ok := record.Payload["model_provider"].(string); ok {
				provider = v
			}
			if v, ok := record.Payload["source"].(string); ok {
				source = v
			}
		}
		if !seenUser && record.Type == "event_msg" && record.Payload["type"] == "user_message" {
			if message, ok := record.Payload["message"].(string); ok {
				title = message
				seenUser = true
			}
		}
	}
	archived := 0
	var archivedAt any
	if strings.HasPrefix(s.Relative, "archived_sessions/") {
		archived = 1
		archivedAt = created
	}
	if updated < created {
		updated = created
	}
	return map[string]any{
		"id":                 s.ID(),
		"created_at":         created,
		"updated_at":         updated,
		"source":             source,
		"model_provider":     provider,
		"cwd":                s.Project,
		"rollout_path":       "",
		"title":              title,
		"sandbox_policy":     `{"type":"read-only"}`,
		"approval_mode":      "on-request",
		"has_user_event":     1,
		"first_user_message": title,
		"archived":           archived,
		"archived_at":        archivedAt,
	}
}
