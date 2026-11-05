package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type updateFixture struct {
	u           updater
	release     release
	archive     []byte
	checksums   string
	requests    []string
	requestMu   sync.Mutex
	failPath    string
	assetName   string
	duringFetch func()
}

func fixture(t *testing.T, platform, arch string) *updateFixture {
	t.Helper()
	f := &updateFixture{assetName: fmt.Sprintf("rehome_%s_%s.tar.gz", platform, arch)}
	f.archive = archiveFixture(t, []*tar.Header{{Name: "rehome", Mode: 0o755}}, "#!/bin/sh\nprintf 'rehome 0.1.1\\n'\n")
	f.checksums = fmt.Sprintf("%x  %s\n", sha256.Sum256(f.archive), f.assetName)
	f.release.Tag = "v0.1.1"
	for _, name := range []string{f.assetName, "checksums.txt"} {
		f.release.Assets = append(f.release.Assets, struct {
			Name string `json:"name"`
		}{Name: name})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requestMu.Lock()
		f.requests = append(f.requests, r.URL.Path)
		f.requestMu.Unlock()
		if r.URL.Path == f.failPath {
			http.Error(w, "fixture failure", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/latest":
			json.NewEncoder(w).Encode(f.release)
		case "/download/v0.1.1/checksums.txt":
			io.WriteString(w, f.checksums)
		case "/download/v0.1.1/" + f.assetName:
			if f.duringFetch != nil {
				f.duringFetch()
			}
			w.Write(f.archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	executable := filepath.Join(t.TempDir(), "rehome")
	if err := os.WriteFile(executable, []byte("original binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.u = updater{client: server.Client(), current: "0.1.0", releaseURL: server.URL + "/latest", assetBase: server.URL + "/download", executable: executable, os: platform, arch: arch}
	return f
}

func (f *updateFixture) requestPaths() []string {
	f.requestMu.Lock()
	defer f.requestMu.Unlock()
	return append([]string(nil), f.requests...)
}

func archiveFixture(t *testing.T, headers []*tar.Header, body string) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tarWriter := tar.NewWriter(gz)
	for _, original := range headers {
		header := *original
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			header.Size = int64(len(body))
		}
		if err := tarWriter.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := io.WriteString(tarWriter, body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func assertKept(t *testing.T, f *updateFixture) {
	t.Helper()
	data, err := os.ReadFile(f.u.executable)
	if err != nil || string(data) != "original binary" {
		t.Fatalf("installed binary was changed: %q, %v", data, err)
	}
	staged, err := filepath.Glob(filepath.Join(filepath.Dir(f.u.executable), ".rehome-update-*"))
	if err != nil || len(staged) != 0 {
		t.Fatalf("staged files were left behind: %v, %v", staged, err)
	}
}

func TestSelfUpdateChecksAndAvoidsDowngrades(t *testing.T) {
	for _, tc := range []struct {
		current   string
		options   Options
		available bool
	}{
		{"0.1.0", Options{CheckOnly: true}, true},
		{"0.1.0", Options{DryRun: true}, true},
		{"0.1.1", Options{}, false},
		{"0.2.0", Options{}, false},
		{"0.2.0-rc.1", Options{}, false},
		{"0.1.1-rc.1", Options{CheckOnly: true}, true},
	} {
		t.Run(fmt.Sprintf("%s/check=%t/dry=%t", tc.current, tc.options.CheckOnly, tc.options.DryRun), func(t *testing.T) {
			f := fixture(t, "linux", "amd64")
			f.u.current = tc.current
			result, err := f.u.run(t.Context(), tc.options)
			if err != nil || result.Available != tc.available || result.Updated || result.Latest != "0.1.1" {
				t.Fatalf("unexpected check result: %+v, %v", result, err)
			}
			if paths := f.requestPaths(); len(paths) != 1 || paths[0] != "/latest" {
				t.Fatalf("check downloaded assets: %v", paths)
			}
			assertKept(t, f)
		})
	}
}

func TestSelfUpdateSelectsAssetAndPreservesSymlinkAndMode(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			t.Run(platform+"/"+arch, func(t *testing.T) {
				f := fixture(t, platform, arch)
				target := f.u.executable
				resolvedTarget, err := filepath.EvalSymlinks(target)
				if err != nil {
					t.Fatal(err)
				}
				alias := filepath.Join(filepath.Dir(target), "alias")
				if err := os.Symlink(target, alias); err != nil {
					t.Fatal(err)
				}
				f.u.executable = alias
				result, err := f.u.run(t.Context(), Options{})
				if err != nil || !result.Updated || result.Path != resolvedTarget {
					t.Fatalf("update failed: %+v, %v", result, err)
				}
				info, err := os.Lstat(alias)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("symlink replaced: %v", err)
				}
				info, err = os.Stat(target)
				if err != nil || info.Mode().Perm() != 0o700 {
					t.Fatalf("mode changed: %v", err)
				}
				out, err := exec.Command(alias, "version").Output()
				if err != nil || strings.TrimSpace(string(out)) != "rehome 0.1.1" {
					t.Fatalf("updated executable does not run: %q, %v", out, err)
				}
			})
		}
	}
}

func TestSelfUpdateFailureKeepsInstalledBinary(t *testing.T) {
	for _, name := range []string{"metadata-http", "checksum-http", "archive-http", "checksum-mismatch", "checksum-missing", "checksum-duplicate", "asset-missing", "draft", "prerelease", "invalid-tag", "bad-archive", "truncated-gzip", "no-binary", "binary-symlink", "duplicate-binary", "wrong-version", "bad-executable", "canceled"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t, "linux", "amd64")
			ctx := t.Context()
			switch name {
			case "metadata-http":
				f.failPath = "/latest"
			case "checksum-http":
				f.failPath = "/download/v0.1.1/checksums.txt"
			case "archive-http":
				f.failPath = "/download/v0.1.1/" + f.assetName
			case "checksum-mismatch":
				f.checksums = strings.Repeat("0", 64) + "  " + f.assetName
			case "checksum-missing":
				f.checksums = ""
			case "checksum-duplicate":
				f.checksums += f.checksums
			case "asset-missing":
				f.release.Assets = f.release.Assets[1:]
			case "draft":
				f.release.Draft = true
			case "prerelease":
				f.release.Prerelease = true
			case "invalid-tag":
				f.release.Tag = "dev"
			case "bad-archive":
				f.archive = []byte("invalid archive")
			case "truncated-gzip":
				f.archive = f.archive[:len(f.archive)-4]
			case "no-binary":
				f.archive = archiveFixture(t, []*tar.Header{{Name: "other"}}, "other")
			case "binary-symlink":
				f.archive = archiveFixture(t, []*tar.Header{{Name: "rehome", Typeflag: tar.TypeSymlink, Linkname: "other"}}, "")
			case "duplicate-binary":
				f.archive = archiveFixture(t, []*tar.Header{{Name: "rehome"}, {Name: "rehome"}}, "fixture")
			case "wrong-version":
				f.archive = archiveFixture(t, []*tar.Header{{Name: "rehome"}}, "#!/bin/sh\nprintf 'rehome 9.0.0\\n'\n")
			case "bad-executable":
				f.archive = archiveFixture(t, []*tar.Header{{Name: "rehome"}}, "not executable")
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if name == "bad-archive" || name == "truncated-gzip" || name == "no-binary" || name == "binary-symlink" || name == "duplicate-binary" || name == "wrong-version" || name == "bad-executable" {
				f.checksums = fmt.Sprintf("%x  %s\n", sha256.Sum256(f.archive), f.assetName)
			}
			result, err := f.u.run(ctx, Options{})
			if err == nil || result.Updated {
				t.Fatalf("invalid update accepted: %+v, %v", result, err)
			}
			assertKept(t, f)
		})
	}
}

func TestSelfUpdateRunningProcess(t *testing.T) {
	if address := os.Getenv("REHOME_UPDATE_TEST_SERVER"); address != "" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		u := updater{client: http.DefaultClient, current: "0.1.0", executable: executable, releaseURL: address + "/latest", assetBase: address + "/download", os: runtime.GOOS, arch: runtime.GOARCH}
		result, err := u.run(t.Context(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(0)
	}
	f := fixture(t, runtime.GOOS, runtime.GOARCH)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.u.executable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(f.u.executable, "-test.run=^TestSelfUpdateRunningProcess$")
	cmd.Env = append(os.Environ(), "REHOME_UPDATE_TEST_SERVER="+strings.TrimSuffix(f.u.releaseURL, "/latest"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running updater failed: %s, %v", output, err)
	}
	var result Result
	if err := json.Unmarshal(output, &result); err != nil || !result.Updated {
		t.Fatalf("running process was not updated: %s, %v", output, err)
	}
	output, err = exec.Command(f.u.executable, "version").Output()
	if err != nil || strings.TrimSpace(string(output)) != "rehome 0.1.1" {
		t.Fatalf("replacement did not survive process exit: %s, %v", output, err)
	}
}

func TestSelfUpdatePreservesConcurrentChange(t *testing.T) {
	f := fixture(t, "linux", "amd64")
	f.duringFetch = func() {
		if err := os.WriteFile(f.u.executable, []byte("concurrent replacement"), 0o700); err != nil {
			t.Error(err)
		}
	}
	result, err := f.u.run(t.Context(), Options{})
	if err == nil || !strings.Contains(err.Error(), "changed during update") || result.Updated {
		t.Fatalf("concurrent change was overwritten: %+v, %v", result, err)
	}
	data, err := os.ReadFile(f.u.executable)
	if err != nil || string(data) != "concurrent replacement" {
		t.Fatalf("other update was lost: %s, %v", data, err)
	}
}

func TestSelfUpdateRejectsUnsupportedPlatformAndDevelopmentBuild(t *testing.T) {
	for _, name := range []string{"os", "arch", "dev"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t, "linux", "amd64")
			switch name {
			case "os":
				f.u.os = "windows"
			case "arch":
				f.u.arch = "386"
			case "dev":
				f.u.current = "dev"
			}
			if _, err := f.u.run(t.Context(), Options{}); err == nil {
				t.Fatal("unsupported updater configuration accepted")
			}
			if len(f.requestPaths()) > 0 {
				t.Fatal("unsupported build contacted GitHub")
			}
			assertKept(t, f)
		})
	}
}

func TestReleaseVersionOrdering(t *testing.T) {
	stable, _ := parseVersion("v1.10.0")
	for _, tc := range []struct {
		text  string
		older bool
	}{{"1.9.9", true}, {"1.10.0-rc.1", true}, {"1.10.0", false}, {"v1.10.0+build", false}, {"1.11.0-rc.1", false}, {"2.0.0", false}} {
		current, err := parseVersion(tc.text)
		if err != nil || current.olderThan(stable) != tc.older {
			t.Errorf("wrong comparison for %s: %v", tc.text, err)
		}
	}
	for _, text := range []string{"dev", "", "1.2", "v01.2.3", "../v1.2.3", "999999999999999999999999999999.0.0"} {
		if _, err := parseVersion(text); err == nil {
			t.Errorf("invalid version accepted: %q", text)
		}
	}
}

func TestArchiveIgnoresPathsOutsideExecutable(t *testing.T) {
	archive := archiveFixture(t, []*tar.Header{{Name: "../outside"}, {Name: "/absolute"}, {Name: "rehome"}}, "executable bytes")
	var destination bytes.Buffer
	if err := extractBinary(bytes.NewReader(archive), &destination); err != nil || destination.String() != "executable bytes" {
		t.Fatalf("unexpected archive contents extracted: %s, %v", &destination, err)
	}
}
