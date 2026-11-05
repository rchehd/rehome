package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxArchiveSize = 128 << 20

type Options struct {
	CheckOnly bool
	DryRun    bool
}

type Result struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Updated   bool   `json:"updated"`
	Path      string `json:"path"`
	CheckOnly bool   `json:"check_only"`
	DryRun    bool   `json:"dry_run"`
}

type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

type updater struct {
	client                                               *http.Client
	releaseURL, assetBase, current, executable, os, arch string
}

func Run(ctx context.Context, current string, options Options) (Result, error) {
	executable, err := os.Executable()
	if err != nil {
		return Result{}, err
	}
	u := updater{
		client: &http.Client{Timeout: time.Minute}, current: current, executable: executable,
		releaseURL: "https://api.github.com/repos/rchehd/rehome/releases/latest",
		assetBase:  "https://github.com/rchehd/rehome/releases/download",
		os:         runtime.GOOS, arch: runtime.GOARCH,
	}
	return u.run(ctx, options)
}

func (u updater) run(ctx context.Context, options Options) (Result, error) {
	result := Result{Current: u.current, CheckOnly: options.CheckOnly, DryRun: options.DryRun}
	current, err := parseVersion(u.current)
	if err != nil {
		return result, fmt.Errorf("cannot update version %q; install a release with install.sh first: %w", u.current, err)
	}
	if (u.os != "linux" && u.os != "darwin") || (u.arch != "amd64" && u.arch != "arm64") {
		return result, fmt.Errorf("self-update is unavailable for %s/%s", u.os, u.arch)
	}
	result.Path, err = filepath.EvalSymlinks(u.executable)
	if err != nil {
		return result, fmt.Errorf("resolve executable: %w", err)
	}
	info, err := os.Stat(result.Path)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, fmt.Errorf("executable is not a regular file: %s", result.Path)
	}
	var metadata bytes.Buffer
	if err := u.fetch(ctx, u.releaseURL, &metadata, 1<<20); err != nil {
		return result, err
	}
	var latest release
	if err := json.Unmarshal(metadata.Bytes(), &latest); err != nil {
		return result, fmt.Errorf("invalid release metadata: %w", err)
	}
	target, err := parseVersion(latest.Tag)
	if err != nil || latest.Draft || latest.Prerelease || target.prerelease != "" {
		return result, fmt.Errorf("GitHub did not return a valid stable release: %q", latest.Tag)
	}
	result.Latest = strings.TrimPrefix(latest.Tag, "v")
	result.Available = current.olderThan(target)
	if !result.Available {
		return result, nil
	}
	archive := fmt.Sprintf("rehome_%s_%s.tar.gz", u.os, u.arch)
	for _, name := range []string{archive, "checksums.txt"} {
		found := false
		for _, asset := range latest.Assets {
			found = found || asset.Name == name
		}
		if !found {
			return result, fmt.Errorf("release %s is missing %s", latest.Tag, name)
		}
	}
	if options.CheckOnly || options.DryRun {
		return result, nil
	}
	staged, err := os.CreateTemp(filepath.Dir(result.Path), ".rehome-update-*")
	if err != nil {
		return result, fmt.Errorf("cannot stage update next to %s; its directory must be writable: %w", result.Path, err)
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	base := u.assetBase + "/" + url.PathEscape(latest.Tag) + "/"
	if err := u.prepare(ctx, base, archive, staged); err != nil {
		return result, err
	}
	if err := staged.Chmod(info.Mode().Perm()); err != nil {
		return result, err
	}
	if err := staged.Sync(); err != nil {
		return result, err
	}
	if err := staged.Close(); err != nil {
		return result, err
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(verifyCtx, staged.Name(), "version").Output()
	if err != nil || strings.TrimSpace(string(output)) != "rehome "+result.Latest {
		return result, fmt.Errorf("downloaded binary failed version verification for %s", result.Latest)
	}
	unchanged, err := os.Stat(result.Path)
	if err != nil || !os.SameFile(info, unchanged) || info.Size() != unchanged.Size() || !info.ModTime().Equal(unchanged.ModTime()) {
		return result, errors.New("installed executable changed during update; run rehome update again")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := os.Rename(staged.Name(), result.Path); err != nil {
		return result, fmt.Errorf("replace executable %s: %w", result.Path, err)
	}
	result.Updated = true
	return result, nil
}

func (u updater) prepare(ctx context.Context, base, archive string, staged *os.File) error {
	var checksums bytes.Buffer
	if err := u.fetch(ctx, base+"checksums.txt", &checksums, 1<<20); err != nil {
		return err
	}
	expected, err := checksum(checksums.String(), archive)
	if err != nil {
		return err
	}
	download, err := os.CreateTemp("", "rehome-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(download.Name())
	defer download.Close()
	hash := sha256.New()
	if err := u.fetch(ctx, base+archive, io.MultiWriter(download, hash), maxArchiveSize); err != nil {
		return err
	}
	if !bytes.Equal(hash.Sum(nil), expected) {
		return fmt.Errorf("checksum mismatch for %s; installed binary was kept", archive)
	}
	if _, err := download.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return extractBinary(download, staged)
}

func (u updater) fetch(ctx context.Context, address string, writer io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "rehome/"+u.current)
	response, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", address, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %s", address, response.Status)
	}
	n, err := io.Copy(writer, io.LimitReader(response.Body, limit+1))
	if err != nil {
		return fmt.Errorf("download %s: %w", address, err)
	}
	if n > limit {
		return fmt.Errorf("download %s exceeds size limit", address)
	}
	return nil
}

func checksum(text, archive string) ([]byte, error) {
	var found []byte
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != archive {
			continue
		}
		value, err := hex.DecodeString(fields[0])
		if err != nil || len(value) != sha256.Size || found != nil {
			return nil, fmt.Errorf("invalid or duplicate checksum for %s", archive)
		}
		found = value
	}
	if found == nil {
		return nil, fmt.Errorf("checksum missing for %s", archive)
	}
	return found, nil
}
