package apt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

const (
	filesSources  = "files/apt/sources"
	filesKeyrings = "files/apt/keyrings"
)

var codename = regexp.MustCompile(`\b(xenial|bionic|focal|jammy|kinetic|lunar|mantic|noble|oracular|plucky|questing|resolute|buster|bullseye|bookworm|trixie|forky)\b`)

// CaptureRepos describes the used third-party repos for the config and returns
// the files to copy as config-relative destination → system source.
func CaptureRepos(used map[string]bool) (map[string]config.AptRepo, map[string]string, error) {
	repos, err := ListRepos()

	if err != nil {
		return nil, nil, err
	}

	cfg := map[string]config.AptRepo{}
	copies := map[string]string{}

	for _, r := range repos {
		if !used[r.Name] {
			continue
		}

		src := filepath.Join(filesSources, filepath.Base(r.File))
		entry := config.AptRepo{Source: src}
		copies[src] = r.File

		for _, k := range r.Keyrings {
			if _, err := os.Stat(k); err == nil {
				entry.Keyring = filepath.Join(filesKeyrings, filepath.Base(k))
				copies[entry.Keyring] = k

				break
			}
		}

		cfg[r.Name] = entry
	}

	return cfg, copies, nil
}

// ApplyRepos installs repo sources and keyrings, retargets them to this release and
// refreshes apt. It returns the repos that could not be used, with the reason (spec §15.6);
// the error is only an apt-get update failure, which the caller reports and survives.
func ApplyRepos(ctx context.Context, r *runner.Runner, root string, repos map[string]config.AptRepo, p platform.Platform) (map[string]string, error) {
	installed := map[string]string{}
	failed := map[string]string{}

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

	source := Retarget(string(data), p.Codename)

	if repo.Keyring != "" {
		keyDest := keyringPath(source, filepath.Base(repo.Keyring))

		if err := installFile(ctx, r, filepath.Join(root, repo.Keyring), keyDest); err != nil {
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

// Retarget swaps release codenames in suite fields for the target codename.
func Retarget(source, target string) string {
	if target == "" {
		return source
	}

	lines := strings.Split(source, "\n")

	for i, l := range lines {
		if strings.HasPrefix(l, "Suites:") || strings.HasPrefix(l, "deb ") || strings.HasPrefix(l, "deb-src ") {
			lines[i] = codename.ReplaceAllString(l, target)
		}
	}

	return strings.Join(lines, "\n")
}

// keyringPath finds where the source expects the keyring, defaulting to /etc/apt/keyrings.
func keyringPath(source, base string) string {
	for _, m := range signedBy.FindAllStringSubmatch(source, -1) {
		if filepath.Base(m[1]) == base {
			return m[1]
		}
	}

	for _, l := range strings.Split(source, "\n") {
		if v, ok := strings.CutPrefix(l, "Signed-By:"); ok && filepath.Base(strings.TrimSpace(v)) == base {
			return strings.TrimSpace(v)
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
