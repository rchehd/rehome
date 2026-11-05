package files

import (
	"bytes"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rchehd/rehome/internal/config"
)

func TestAutoCaptureFiltersRuntimeAndDesktopSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settings := []string{"settings.json", "Preferences", "themes/dark.json", "public.pem", "client.pem"}
	runtime := []string{
		"claude-code-sessions/account/session/local_123.json", "claude-code-sessions/account/archived-sessions.idx",
		"Crashpad/client_id", "VideoDecodeStats/LOCK", "VideoDecodeStats/LOG", "logs/output.log",
		"LOCK", "LOG", "LOG.old", "output.lock", "output.LOG", "output.LOCK",
		"SharedStorage-wal", "SharedStorage/state", "claude-code/2.1.308/hash/.payload", "claude-code/2.1.308/hash/.verified",
		"buddy-tokens.json", "ant-device-registry.json", "ant-did", "ccd-ids.json",
		"credentials/cakey.pem", "certs/default_key.key", "other-key.pem", "cakey.pem", "Cache/data", "binary.json",
		"user-excluded.json",
	}
	for _, rel := range append(slices.Clone(settings), runtime...) {
		body := []byte("fixture")
		if rel == "binary.json" {
			body = []byte("binary\x00data")
		}
		if rel == "client.pem" {
			body = []byte("-----BEGIN PRIVATE KEY-----\nprivate-fixture\n-----END PRIVATE KEY-----")
		}
		writeHarnessFixture(t, home, ".config/Claude/"+rel, string(body), 0o600)
	}

	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "legacy"}[existing], func(t *testing.T) {
			rule := config.FileRule{Base: "~/.config/Claude", Include: []string{"**"}, Exclude: []string{"user-excluded.json"}, Origin: config.OriginAuto}
			if !existing {
				rule = rule.WithAutoDefaults()
			}
			changes, mappings, err := CaptureRules("claude-desktop", map[string]config.FileRule{"linux": rule}, "linux", t.TempDir(), true)
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) != len(settings) || len(mappings) != len(settings) {
				t.Fatalf("unexpected capture: %+v", changes)
			}
			for _, change := range changes {
				rel := filepath.ToSlash(relativePath(t, filepath.Join(home, ".config/Claude"), change.Src))
				if !slices.Contains(settings, rel) || bytes.Contains(change.New, []byte("private-fixture")) {
					t.Errorf("unexpected or unredacted capture: %s", change.Src)
				}
			}
			if existing && (len(rule.Exclude) != 1 || rule.TextOnly) {
				t.Fatal("capture mutated the original rule")
			}
		})
	}
}

func relativePath(t *testing.T, base, path string) string {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestManualCaptureStillAllowsExplicitRuntimeFile(t *testing.T) {
	home := t.TempDir()
	writeHarnessFixture(t, home, "Crashpad/client_id", "explicit file", 0o600)
	rule := config.FileRule{Base: home, Include: []string{"Crashpad/client_id"}}
	changes, _, err := CaptureRules("custom", map[string]config.FileRule{"linux": rule}, "linux", t.TempDir(), false)
	if err != nil || len(changes) != 1 {
		t.Fatalf("manual rule was filtered: %+v, %v", changes, err)
	}
}
