package sessions

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	_ "modernc.org/sqlite"
)

func openDB(path, mode string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", mode)
	q.Set("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

func (s *Snapshot) readCursor(ctx context.Context, path string, paths map[string]string) error {
	db, err := openDB(path, "ro")
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var meta string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM meta WHERE key='0'").Scan(&meta); err != nil {
		return err
	}
	raw, err := hex.DecodeString(meta)
	if err != nil {
		return errors.New("unsupported Cursor metadata encoding")
	}
	if err := json.Unmarshal(raw, &s.Meta); err != nil {
		return err
	}
	if root, _ := s.Meta["latestRootBlobId"].(string); root == "" {
		return ErrEmpty
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,data FROM blobs ORDER BY id")
	if err != nil {
		return err
	}
	s.Blobs = map[string][]byte{}
	var totalBytes int
	for rows.Next() {
		var id string
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			rows.Close()
			return err
		}
		totalBytes += len(data)
		if totalBytes > 128<<20 {
			rows.Close()
			return errors.New("Cursor session exceeds 128 MiB")
		}
		s.Blobs[id] = data
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.Sidecar = map[string]any{}
	if raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "meta.json")); err == nil {
		_ = json.Unmarshal(raw, &s.Sidecar)
	}
	if cwd, ok := s.Sidecar["cwd"].(string); ok {
		s.Project = cwd
	}
	workspace := filepath.Base(filepath.Dir(filepath.Dir(path)))
	acp := workspace == "acp-sessions"
	matches := func(candidate string) bool {
		hash := md5.Sum([]byte(filepath.Clean(candidate)))
		return hex.EncodeToString(hash[:]) == workspace
	}
	if !acp && !matches(s.Project) {
		s.Project = ""
	}
	for from := range paths {
		if matches(from) {
			s.Project = from
		}
	}
	if cwd, err := os.Getwd(); err == nil && matches(cwd) {
		s.Project = cwd
	}
	if s.Project == "" && !acp {
		for _, blob := range s.Blobs {
			for _, value := range absolutePaths.FindAllString(string(blob), -1) {
				value = strings.TrimRight(value, ",;:)]}")
				for candidate := value; candidate != "/" && candidate != "."; candidate = filepath.Dir(candidate) {
					if matches(candidate) {
						s.Project = candidate
						break
					}
				}
				if s.Project != "" {
					break
				}
			}
			if s.Project != "" {
				break
			}
		}
	}
	if s.Project == "" {
		return fmt.Errorf("cannot resolve Cursor workspace %s; supply --session-path-map /original/project=/target/project", workspace)
	}
	s.Sidecar["schemaVersion"] = 1
	s.Sidecar["hasConversation"] = true
	if s.Meta["subagentInfo"] != nil {
		s.Sidecar["isSubagent"] = true
	}
	s.Sidecar["createdAtMs"] = s.Meta["createdAt"]
	s.Sidecar["title"] = s.Meta["name"]
	s.Sidecar["cwd"] = s.Project
	return nil
}

var absolutePaths = regexp.MustCompile(`/[^\x00-\x20<>"'\\]+`)

func (s *Snapshot) transformBlobs(t *transformer) error {
	old := s.Blobs
	result := map[string][]byte{}
	ids := map[string]string{}
	visiting := map[string]bool{}
	var visit func(string) (string, error)
	visit = func(id string) (string, error) {
		data, exists := old[id]
		if !exists {
			return id, nil
		}
		if next, ok := ids[id]; ok {
			return next, nil
		}
		if visiting[id] {
			return "", errors.New("Cursor session contains a cyclic blob graph")
		}
		if len(visiting) > 256 {
			return "", errors.New("Cursor blob graph exceeds 256 levels")
		}
		visiting[id] = true
		data, err := t.blob(data, 0)
		if err != nil {
			return "", err
		}
		hash := sha256.Sum256(data)
		next := hex.EncodeToString(hash[:])
		ids[id] = next
		result[next] = data
		delete(visiting, id)
		return next, nil
	}
	t.ref = visit
	defer func() { t.ref = nil }()
	keys := make([]string, 0, len(old))
	for id := range old {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	for _, id := range keys {
		if _, err := visit(id); err != nil {
			return err
		}
	}
	metadata, err := (&transformer{ref: visit}).value("metadata", s.Meta)
	if err != nil {
		return err
	}
	s.Meta = metadata.(map[string]any)
	root, _ := s.Meta["latestRootBlobId"].(string)
	if _, ok := result[root]; !ok {
		return errors.New("Cursor root blob is missing")
	}
	s.Blobs = result
	return nil
}

func (s *Snapshot) writeCursor(ctx context.Context, path string) error {
	db, err := openDB(path, "rwc")
	if err != nil {
		return err
	}
	defer db.Close()
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "CREATE TABLE blobs (id TEXT PRIMARY KEY,data BLOB); CREATE TABLE meta (key TEXT PRIMARY KEY,value TEXT); PRAGMA user_version=1;"); err != nil {
		return err
	}
	for id, data := range s.Blobs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO blobs VALUES (?,?)", id, data); err != nil {
			return err
		}
	}
	metadata, err := json.Marshal(s.Meta)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO meta VALUES ('0',?)", hex.EncodeToString(metadata)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
