package apt

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

const (
	dpkgOnly   = "dpkg-only"
	dpkgStatus = "/var/lib/dpkg/status"
)

type Apt struct{}

func (Apt) Name() string { return "apt" }

func (Apt) Available(p platform.Platform) bool {
	return p.Key() == "debian" && adapter.HasCmd("apt-get")
}

// Scan lists manually installed packages that are not part of the base OS.
func (Apt) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "apt-mark", Args: []string{"showmanual"}})

	if err != nil {
		return nil, err
	}

	base, err := Baseline(ctx, r)

	if err != nil {
		return nil, err
	}

	var pkgs []string

	for _, p := range adapter.Lines(out) {
		if !base[p] {
			pkgs = append(pkgs, p)
		}
	}

	pkgs, err = dropDependedLibs(ctx, r, pkgs)

	if err != nil {
		return nil, err
	}

	pkgs, err = dropDataPackages(ctx, r, pkgs)

	if err != nil {
		return nil, err
	}

	origins, err := Origins(ctx, r, pkgs)

	if err != nil {
		return nil, err
	}

	repos, err := ListRepos()

	if err != nil {
		return nil, err
	}

	found := make([]adapter.Detected, 0, len(pkgs))

	for _, p := range pkgs {
		// A .deb installed by hand has no repo to reinstall from; the catalog may know a URL.
		if origins[p] == dpkgOnly {
			meta := map[string]string{"package": p, "origin": "installed from a .deb file; add install.debian.deb_url"}
			found = append(found, adapter.Detected{Manager: "unknown", Name: p, Meta: meta})

			continue
		}

		d := adapter.Detected{Manager: "apt", Name: p, Spec: config.InstallSpec{Apt: p}, Meta: map[string]string{"origin": origins[p]}}
		d.Spec.Repo = repos.NameFor(origins[p])
		found = append(found, d)
	}

	return found, nil
}

func (Apt) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f=${db:Status-Status}", s.Apt}})

	if err != nil {
		return false, nil
	}

	return strings.TrimSpace(out) == "installed", nil
}

func (Apt) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	target := s.Apt

	if s.DebURL != "" {
		path, cleanup, err := download(ctx, r, s.DebURL)

		if err != nil {
			return err
		}

		defer cleanup()
		target = path
	}

	_, err := r.Run(ctx, AptGet("install", "-y", "--", target))

	return err
}

// AptGet builds a non-interactive `sudo apt-get` command.
func AptGet(args ...string) runner.Cmd {
	return runner.Cmd{Name: "env", Args: append([]string{"DEBIAN_FRONTEND=noninteractive", "apt-get"}, args...), Sudo: true}
}

// Unavailable returns the packages apt has no installation candidate for.
func Unavailable(ctx context.Context, r *runner.Runner, pkgs []string) ([]string, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "apt-cache", Args: append([]string{"policy", "--"}, pkgs...)})

	if err != nil {
		return nil, err
	}

	candidate := map[string]bool{}
	var pkg string

	for _, l := range strings.Split(out, "\n") {
		switch {
		case l != "" && l[0] != ' ':
			pkg = strings.TrimSuffix(strings.TrimSpace(l), ":")
		case strings.HasPrefix(strings.TrimSpace(l), "Candidate:"):
			candidate[pkg] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "Candidate:")) != "(none)"
		}
	}

	var missing []string

	for _, p := range pkgs {
		if !candidate[p] {
			missing = append(missing, p)
		}
	}

	return missing, nil
}

// Origins maps each package to the archive URI its installed version came from, or dpkg-only.
func Origins(ctx context.Context, r *runner.Runner, pkgs []string) (map[string]string, error) {
	origins := map[string]string{}

	if len(pkgs) == 0 {
		return origins, nil
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "apt-cache", Args: append([]string{"policy"}, pkgs...)})

	if err != nil {
		return nil, err
	}

	var pkg string
	inInstalled := false

	for _, l := range strings.Split(out, "\n") {
		switch {
		case l != "" && l[0] != ' ':
			pkg = strings.TrimSuffix(strings.TrimSpace(l), ":")
			origins[pkg] = dpkgOnly
			inInstalled = false
		case strings.HasPrefix(l, " *** "):
			inInstalled = true
		case strings.HasPrefix(l, "     ") && !strings.HasPrefix(l, "        "):
			inInstalled = false
		case inInstalled && strings.HasPrefix(l, "        "):
			f := strings.Fields(l)

			if len(f) >= 2 && f[1] != dpkgStatus && origins[pkg] == dpkgOnly {
				origins[pkg] = f[1]
			}
		}
	}

	return origins, nil
}

func download(ctx context.Context, r *runner.Runner, url string) (string, func(), error) {
	if r.DryRun {
		r.Run(ctx, runner.Cmd{Name: "download", Args: []string{url}})

		return url, func() {}, nil
	}

	f, err := os.CreateTemp("", "rehome-*.deb")

	if err != nil {
		return "", nil, err
	}

	cleanup := func() { os.Remove(f.Name()) }
	defer f.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)

	if err != nil {
		cleanup()

		return "", nil, err
	}

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		cleanup()

		return "", nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cleanup()

		return "", nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		cleanup()

		return "", nil, err
	}

	// apt runs as _apt and must be able to read the file.
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		cleanup()

		return "", nil, err
	}

	return f.Name(), cleanup, nil
}

// IsOfficial reports whether an archive URI belongs to the distribution itself.
func IsOfficial(uri string) bool {
	hosts := []string{"archive.ubuntu.com", "security.ubuntu.com", "ports.ubuntu.com", "deb.debian.org", "security.debian.org"}

	return slices.ContainsFunc(hosts, func(h string) bool { return strings.Contains(uri, h) })
}

// Binaries lists the commands a package installs; their names hint where settings live.
func Binaries(ctx context.Context, r *runner.Runner, pkg string) []string {
	out, err := r.Output(ctx, runner.Cmd{Name: "dpkg", Args: []string{"-L", "--", pkg}})

	if err != nil {
		return nil
	}

	var bins []string

	for _, l := range adapter.Lines(out) {
		for _, dir := range []string{"/usr/bin/", "/usr/sbin/", "/usr/games/", "/bin/"} {
			if name, ok := strings.CutPrefix(l, dir); ok && name != "" && !strings.Contains(name, "/") {
				bins = append(bins, name)
			}
		}
	}

	return bins
}

func (Apt) Remove(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, AptGet("remove", "-y", "--", s.Apt))

	return err
}
