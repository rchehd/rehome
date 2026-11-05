package sessions

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestJSONLSessionsMoveBetweenHomes(t *testing.T) {
	for _, format := range []string{"claude-code", "codex"} {
		t.Run(format, func(t *testing.T) {
			from, to := t.TempDir(), t.TempDir()
			t.Setenv("HOME", from)
			t.Setenv("CODEX_HOME", "")
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			project := filepath.Join(from, "Projects", "example")
			id := "01234567-1234-4123-8123-0123456789ab"
			rel := "projects/old-project/" + id + ".jsonl"
			line := map[string]any{"type": "user", "cwd": project, "sessionId": id, "message": map[string]any{"role": "user", "content": "sk-ant-abcdefghijklmnopqrstuvwxyz"}}
			if format == "codex" {
				rel = "sessions/2026/11/05/rollout-2026-11-05T00-00-00-" + id + ".jsonl"
				line = map[string]any{"timestamp": "2026-11-05T00:00:00Z", "type": "session_meta", "payload": map[string]any{"id": id, "cwd": project, "source": "cli", "model_provider": "openai", "base_instructions": map[string]any{"text": "sk-ant-abcdefghijklmnopqrstuvwxyz"}}}
			}
			source := filepath.Join(Root(format), rel)
			putSessionFile(t, source, mustJSON(line))
			snapshot, findings, err := Read(t.Context(), format, source, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) == 0 || bytes.Contains(mustJSON(snapshot), []byte("sk-ant-abcdefghijklmnopqrstuvwxyz")) {
				t.Fatal("session secret was not redacted")
			}
			t.Setenv("HOME", to)
			dest, copied, err := snapshot.Restore(t.Context(), true, nil)
			if err != nil || !copied {
				t.Fatalf("dry run: %v %v", copied, err)
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("dry run wrote a session")
			}
			dest, copied, err = snapshot.Restore(t.Context(), false, nil)
			if err != nil || !copied {
				t.Fatalf("restore: %v %v", copied, err)
			}
			data, err := os.ReadFile(dest)
			if err != nil || bytes.Contains(data, []byte(from)) || !bytes.Contains(data, []byte(filepath.Join(to, "Projects", "example"))) {
				t.Fatalf("paths were not adapted: %s %v", data, err)
			}
			putSessionFile(t, dest, []byte("local history"))
			if _, copied, err := snapshot.Restore(t.Context(), false, nil); err != nil || copied {
				t.Fatalf("repeat replaced history: %v %v", copied, err)
			}
			data, _ = os.ReadFile(dest)
			if string(data) != "local history" {
				t.Fatal("local history changed")
			}
		})
	}
}

func TestCursorSnapshotIncludesWALAndRebuildsBlobGraph(t *testing.T) {
	t.Setenv("CURSOR_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	from, to := t.TempDir(), t.TempDir()
	t.Setenv("HOME", from)
	project := filepath.Join(from, "Projects", "example")
	hash := md5.Sum([]byte(project))
	id := "01234567-1234-4123-8123-0123456789ab"
	source := filepath.Join(Root("cursor"), "chats", hex.EncodeToString(hash[:]), id, "store.db")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(source, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE meta(key TEXT PRIMARY KEY,value TEXT); CREATE TABLE blobs(id TEXT PRIMARY KEY,data BLOB)"); err != nil {
		t.Fatal(err)
	}
	leaf := mustJSON(map[string]any{"role": "user", "content": "sk-ant-abcdefghijklmnopqrstuvwxyz " + project})
	leafHash := sha256.Sum256(leaf)
	root := append([]byte{0x0a, 32}, leafHash[:]...)
	root = binary.AppendUvarint(root, 0x12)
	root = binary.AppendUvarint(root, uint64(len(project)))
	root = append(root, project...)
	rootHash := sha256.Sum256(root)
	meta := map[string]any{"agentId": id, "latestRootBlobId": hex.EncodeToString(rootHash[:]), "name": "Example", "createdAt": 1730000000000, "mode": "default", "blobEncryptionKey": id}
	if _, err := db.Exec("INSERT INTO meta VALUES('0',?)", hex.EncodeToString(mustJSON(meta))); err != nil {
		t.Fatal(err)
	}
	for _, blob := range [][]byte{leaf, root} {
		h := sha256.Sum256(blob)
		if _, err := db.Exec("INSERT INTO blobs VALUES(?,?)", hex.EncodeToString(h[:]), blob); err != nil {
			t.Fatal(err)
		}
	}
	putSessionFile(t, filepath.Join(filepath.Dir(source), "meta.json"), mustJSON(map[string]any{"cwd": project}))
	snapshot, findings, err := Read(t.Context(), "cursor", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Blobs) != 2 || len(findings) == 0 {
		t.Fatal("snapshot missed WAL data or secret redaction")
	}
	if snapshot.Meta["blobEncryptionKey"] != id {
		t.Fatal("Cursor session encryption metadata was lost")
	}
	t.Setenv("HOME", to)
	newProject := filepath.Join(to, "different-project")
	dest, copied, err := snapshot.Restore(t.Context(), false, map[string]string{project: newProject})
	if err != nil || !copied {
		t.Fatalf("restore: %v %v", copied, err)
	}
	newHash := md5.Sum([]byte(newProject))
	if !strings.Contains(dest, hex.EncodeToString(newHash[:])) {
		t.Fatal("workspace hash was not adapted")
	}
	restored, err := openDB(dest, "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	rows, err := restored.Query("SELECT id,data FROM blobs")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(value)
		if key != hex.EncodeToString(h[:]) || bytes.Contains(value, []byte(from)) || bytes.Contains(value, []byte("sk-ant-")) {
			t.Fatal("invalid transformed blob")
		}
		if fields, ok := wireFields(value); ok && len(fields) > 0 && len(fields[0].data) == 32 {
			var count int
			// Use a separate connection while rows are open.
			check, err := openDB(dest, "ro")
			if err != nil {
				t.Fatal(err)
			}
			err = check.QueryRow("SELECT count(*) FROM blobs WHERE id=?", hex.EncodeToString(fields[0].data)).Scan(&count)
			check.Close()
			if err != nil || count != 1 {
				t.Fatal("blob reference was not rebuilt")
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if _, copied, err := snapshot.Restore(t.Context(), false, map[string]string{project: newProject}); err != nil || copied {
		t.Fatal("repeat import changed a Cursor session")
	}
}

func TestCodexImportAddsOnlyMissingIndexRow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	path := filepath.Join(Root("codex"), "state_5.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(path, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE threads(id TEXT PRIMARY KEY,rollout_path TEXT NOT NULL,cwd TEXT NOT NULL,title TEXT NOT NULL,sandbox_policy TEXT NOT NULL DEFAULT '{}'); INSERT INTO threads(id,rollout_path,cwd,title) VALUES('local','local-path','local-cwd','local-title')"); err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{Version: 1, Format: "codex", Home: "/old/home", Project: "/old/home/project", Relative: "sessions/2026/11/05/rollout-id.jsonl", Lines: []json.RawMessage{json.RawMessage(`{"type":"session_meta","payload":{"id":"imported","cwd":"/old/home/project"}}`)}}
	dest, copied, err := snapshot.Restore(t.Context(), false, nil)
	if err != nil || !copied {
		t.Fatalf("restore: %v %v", copied, err)
	}
	var rollout, cwd string
	if err := db.QueryRow("SELECT rollout_path,cwd FROM threads WHERE id='imported'").Scan(&rollout, &cwd); err != nil || rollout != dest || cwd != filepath.Join(home, "project") {
		t.Fatalf("index not restored: %s %s %v", rollout, cwd, err)
	}
	var title string
	if err := db.QueryRow("SELECT title FROM threads WHERE id='local'").Scan(&title); err != nil || title != "local-title" {
		t.Fatal("existing index row changed")
	}
	var sandbox string
	if err := db.QueryRow("SELECT sandbox_policy FROM threads WHERE id='imported'").Scan(&sandbox); err != nil {
		t.Fatal(err)
	}
	var policy map[string]any
	if json.Unmarshal([]byte(sandbox), &policy) != nil || policy["type"] != "read-only" {
		t.Fatal("invalid native sandbox policy")
	}
}

func TestSessionRejectsIncompleteAndUnsafeInputs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	file := filepath.Join(Root("codex"), "sessions", "incomplete.jsonl")
	putSessionFile(t, file, []byte(`{"type":"session_meta"`))
	if _, _, err := Read(t.Context(), "codex", file, nil); err == nil {
		t.Fatal("accepted incomplete JSONL")
	}
	base := Snapshot{Version: 1, Format: "codex", Home: "/old/home", Project: "/old/home/project", Relative: "sessions/rollout.jsonl", Lines: []json.RawMessage{json.RawMessage(`{"type":"session_meta","payload":{"id":"fixture","cwd":"/old/home/project"}}`)}}
	for _, rel := range []string{"../outside.jsonl", "sessions/../outside.jsonl", "/outside.jsonl", `sessions\outside.jsonl`} {
		snapshot := base
		snapshot.Relative = rel
		if _, _, err := snapshot.Restore(t.Context(), false, nil); err == nil {
			t.Errorf("accepted unsafe path %s", rel)
		}
	}
	if _, _, err := base.Restore(t.Context(), false, map[string]string{"relative": "/target"}); err == nil {
		t.Fatal("accepted relative path mapping")
	}
}

func TestCodexIndexFailureRollsBackImportedFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	path := filepath.Join(Root("codex"), "state_5.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(path, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE threads(id TEXT PRIMARY KEY, required_future_column TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{Version: 1, Format: "codex", Home: home, Project: filepath.Join(home, "project"), Relative: "sessions/rollout.jsonl", Lines: []json.RawMessage{json.RawMessage(`{"type":"session_meta","payload":{"id":"fixture"}}`)}}
	dest, _, err := snapshot.Restore(t.Context(), false, nil)
	if err == nil {
		t.Fatal("unsupported target schema was accepted")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("failed import left a session file")
	}
}

func TestCompressedCodexArchiveRestoresAsJSONL(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	t.Setenv("HOME", from)
	t.Setenv("CODEX_HOME", "")
	line := mustJSON(map[string]any{"type": "session_meta", "payload": map[string]any{"id": "archived-fixture", "cwd": filepath.Join(from, "project")}})
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	compressed := encoder.EncodeAll(line, nil)
	encoder.Close()
	file := filepath.Join(Root("codex"), "archived_sessions", "rollout-fixture.jsonl.zst")
	putSessionFile(t, file, compressed)
	snapshot, _, err := Read(t.Context(), "codex", file, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", to)
	dest, copied, err := snapshot.Restore(t.Context(), false, nil)
	if err != nil || !copied || strings.HasSuffix(dest, ".zst") {
		t.Fatalf("restore: %s %v %v", dest, copied, err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || !json.Valid(bytes.TrimSpace(data)) {
		t.Fatal("archive did not restore as native JSONL")
	}
}

func TestConcurrentImportsKeepOneCompleteSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	snapshot := Snapshot{Version: 1, Format: "codex", Home: home, Project: filepath.Join(home, "project"), Relative: "sessions/rollout.jsonl", Lines: []json.RawMessage{json.RawMessage(`{"type":"session_meta","payload":{"id":"fixture"}}`)}}
	var jobs sync.WaitGroup
	for i := 0; i < 8; i++ {
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			if _, _, err := snapshot.Restore(t.Context(), false, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	jobs.Wait()
	data, err := os.ReadFile(snapshot.Destination())
	if err != nil || !json.Valid(bytes.TrimSpace(data)) {
		t.Fatal("concurrent import left an incomplete session")
	}
}

func putSessionFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
