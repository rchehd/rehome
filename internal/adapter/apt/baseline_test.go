package apt

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/rchehd/rehome/internal/runner"
)

func TestBaselineWithMissingMetapackageAndInstallerSnapshot(t *testing.T) {
	for _, state := range []string{"fresh", "upgraded", "missing", "corrupt"} {
		t.Run(state, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PATH", dir)
			priorities := "installed ubuntu-standard optional\ninstalled base-tool optional\ninstalled core-tool required\ninstalled gnome-shell optional\ninstalled firefox optional\n"
			if state == "fresh" {
				priorities += "installed ubuntu-desktop optional\n"
			}
			commands := map[string]string{
				"dpkg-query": "printf %s " + runner.Quote(priorities),
				"apt-cache":  "case \"$*\" in *ubuntu-desktop*) printf 'ubuntu-desktop\\ngnome-shell\\nbase-tool\\n' ;; *) printf 'ubuntu-standard\\nbase-tool\\n' ;; esac",
			}
			for name, body := range commands {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := filepath.Join(dir, "initial-status.gz")
			if state == "upgraded" {
				writeSnapshot(t, snapshot, "Package: gnome-shell\n\n")
			} else if state == "corrupt" {
				if err := os.WriteFile(snapshot, []byte("invalid gzip"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			base, err := baseline(t.Context(), &runner.Runner{}, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if !base["base-tool"] || !base["core-tool"] || base["firefox"] {
				t.Fatalf("baseline lost known packages or hid a user app: %v", base)
			}
			if want := state == "fresh" || state == "upgraded"; base["gnome-shell"] != want {
				t.Fatalf("desktop baseline = %t, want %t", base["gnome-shell"], want)
			}
		})
	}
}

func TestInstallerSnapshotRejectsTruncatedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "initial-status.gz")
	writeSnapshot(t, path, "Package: gnome-shell\n\n")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data[:len(data)-4], 0o600); err != nil {
		t.Fatal(err)
	}
	if packages, err := readInitialStatus(path); err == nil || len(packages) != 0 {
		t.Fatalf("trusted a partial snapshot: %v, %v", packages, err)
	}
}

func TestLimitedBaselineDiagnostic(t *testing.T) {
	installed := map[string]string{"ubuntu-standard": "optional", "gnome-shell": "optional"}
	if baselineWarning(installed) == "" {
		t.Fatal("missing baseline was not explained")
	}
	installed["ubuntu-desktop"] = "optional"
	if warning := baselineWarning(installed); warning != "" {
		t.Fatalf("installed desktop seed was ignored: %s", warning)
	}
}

func writeSnapshot(t *testing.T, path, body string) {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	if _, err := gz.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
