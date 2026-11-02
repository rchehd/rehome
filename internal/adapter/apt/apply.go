package apt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

const (
	filesSources    = "files/apt/sources"
	filesKeyrings   = "files/apt/keyrings"
	filesLegacyKeys = "files/apt/trusted.gpg.d"
	legacyKeysDir   = "/etc/apt/trusted.gpg.d"
)

var codename = regexp.MustCompile(`\b(xenial|bionic|focal|jammy|kinetic|lunar|mantic|noble|oracular|plucky|questing|resolute|buster|bullseye|bookworm|trixie|forky)\b`)

// The distribution's own keys in trusted.gpg.d ship with the OS.
var distroKey = regexp.MustCompile(`^(ubuntu|debian)-`)

type RepoCapture struct {
	Repos map[string]config.AptRepo
	// Keys are config-relative legacy keys for trusted.gpg.d.
	Keys []string
	// Copies maps config-relative destinations to system sources.
	Copies   map[string]string
	Warnings []string
}

// CaptureRepos describes the used third-party repos, their keyrings and legacy keys.
func CaptureRepos(used map[string]bool, release string) (*RepoCapture, error) {
	repos, err := ListRepos()

	if err != nil {
		return nil, err
	}

	c := &RepoCapture{Repos: map[string]config.AptRepo{}, Copies: map[string]string{}}

	for _, r := range repos {
		if !used[r.Name] {
			continue
		}

		src := filepath.Join(filesSources, filepath.Base(r.File))
		entry := config.AptRepo{Source: src, Release: release}
		c.Copies[src] = r.File

		for _, k := range r.Keyrings {
			info, err := os.Stat(k)

			if err != nil || info.Size() == 0 {
				c.Warnings = append(c.Warnings, fmt.Sprintf("apt repo %s: keyring %s is missing or empty; the repo is broken here too", r.Name, k))

				continue
			}

			dst := filepath.Join(filesKeyrings, filepath.Base(k))
			entry.Keyrings = append(entry.Keyrings, dst)
			c.Copies[dst] = k
		}

		c.Repos[r.Name] = entry
	}

	if len(c.Repos) > 0 {
		c.captureLegacyKeys()
	}

	return c, nil
}

func (c *RepoCapture) captureLegacyKeys() {
	keys, _ := filepath.Glob(legacyKeysDir + "/*")

	for _, k := range keys {
		if distroKey.MatchString(filepath.Base(k)) {
			continue
		}

		dst := filepath.Join(filesLegacyKeys, filepath.Base(k))
		c.Keys = append(c.Keys, dst)
		c.Copies[dst] = k
	}
}

// ApplyRepos installs legacy keys, repo sources and keyrings, retargets them to this release
// and refreshes apt. It returns the repos that could not be used, with the reason (spec §15.6);
// the error is only an apt-get update failure, which the caller reports and survives.
func ApplyRepos(ctx context.Context, r *runner.Runner, root string, repos map[string]config.AptRepo, keys []string, p platform.Platform) (map[string]string, error) {
	installed := map[string]string{}
	failed := map[string]string{}

	for _, k := range keys {
		if err := installFile(ctx, r, filepath.Join(root, k), filepath.Join(legacyKeysDir, filepath.Base(k))); err != nil {
			return failed, err
		}
	}

	for name, repo := range repos {
		dest, err := installRepo(ctx, r, root, repo, p)

		if err != nil {
			failed[name] = "not installed: " + err.Error()

			continue
		}

		installed[name] = dest
	}

	// apt-get update exits 0 when a source is unreachable, so the output decides.
	res, err := r.Run(ctx, AptGet("update"))
	output := res.Stdout + "\n" + res.StderrTail

	if err != nil {
		output += "\n" + errText(err)
	}

	disabled := false

	for name, dest := range installed {
		if !repoFailed(output, repos[name], root) {
			continue
		}

		failed[name] = "failed to update and was disabled"
		disabled = true

		if _, mvErr := r.Run(ctx, runner.Cmd{Name: "mv", Args: []string{dest, dest + ".disabled"}, Sudo: true}); mvErr != nil {
			failed[name] = "failed to update; disabling it also failed: " + mvErr.Error()
		}
	}

	if disabled {
		_, err = r.Run(ctx, AptGet("update"))
	}

	return failed, err
}

func errText(err error) string {
	var ce *runner.CmdError

	if errors.As(err, &ce) {
		return ce.StderrTail
	}

	return err.Error()
}

func installRepo(ctx context.Context, r *runner.Runner, root string, repo config.AptRepo, p platform.Platform) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, repo.Source))

	if err != nil {
		return "", err
	}

	source := Retarget(string(data), repo.Release, p.Codename)

	for _, k := range repo.AllKeyrings() {
		if err := installFile(ctx, r, filepath.Join(root, k), keyringPath(source, filepath.Base(k))); err != nil {
			return "", err
		}
	}

	tmp, err := os.CreateTemp("", "rehome-source-")

	if err != nil {
		return "", err
	}

	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(source); err != nil {
		return "", err
	}

	tmp.Close()
	dest := filepath.Join(sourcesDir, filepath.Base(repo.Source))

	return dest, installFile(ctx, r, tmp.Name(), dest)
}

// Retarget swaps the capture-time codename (from) in suite fields for the target codename.
// A repo pinned to another distro's release (ngrok's "bookworm" on Ubuntu) is left alone.
// Without a recorded release, any known codename is swapped (configs from older scans).
func Retarget(source, from, target string) string {
	if target == "" || from == target {
		return source
	}

	match := codename

	if from != "" {
		match = regexp.MustCompile(`\b` + regexp.QuoteMeta(from) + `\b`)
	}

	lines := strings.Split(source, "\n")

	for i, l := range lines {
		if strings.HasPrefix(l, "Suites:") || strings.HasPrefix(l, "deb ") || strings.HasPrefix(l, "deb-src ") {
			lines[i] = match.ReplaceAllString(l, target)
		}
	}

	return strings.Join(lines, "\n")
}

// keyringPath finds where the source expects the keyring, defaulting to /etc/apt/keyrings.
func keyringPath(source, base string) string {
	for _, m := range signedBy.FindAllStringSubmatch(source, -1) {
		for _, p := range appendPaths(nil, m[1]) {
			if filepath.Base(p) == base {
				return p
			}
		}
	}

	for _, l := range strings.Split(source, "\n") {
		if v, ok := strings.CutPrefix(l, "Signed-By:"); ok {
			for _, p := range appendPaths(nil, v) {
				if filepath.Base(p) == base {
					return p
				}
			}
		}
	}

	return filepath.Join("/etc/apt/keyrings", base)
}

func installFile(ctx context.Context, r *runner.Runner, src, dest string) error {
	_, err := r.Run(ctx, runner.Cmd{Name: "install", Args: []string{"-D", "-m", "0644", src, dest}, Sudo: true})

	return err
}

func repoFailed(output string, repo config.AptRepo, root string) bool {
	data, err := os.ReadFile(filepath.Join(root, repo.Source))

	if err != nil {
		return false
	}

	parsed, err := parseRepoText(string(data), filepath.Ext(repo.Source) == ".sources")

	if err != nil {
		return false
	}

	for _, l := range strings.Split(output, "\n") {
		if !strings.HasPrefix(l, "Err:") && !strings.HasPrefix(l, "E:") && !strings.HasPrefix(l, "W: GPG") {
			continue
		}

		for _, u := range parsed.URIs {
			if strings.Contains(l, strings.TrimSuffix(u, "/")) {
				return true
			}
		}
	}

	return false
}
