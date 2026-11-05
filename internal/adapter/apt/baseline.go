package apt

import (
	"bufio"
	"compress/gzip"
	"context"
	"os"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/runner"
)

const initialStatus = "/var/log/installer/initial-status.gz"

var basePriorities = map[string]bool{"required": true, "important": true, "standard": true}

// Base system packages that installers mark manual.
var basePrefixes = []string{"linux-", "language-pack-", "ubuntu-", "grub-", "shim-", "oem-", "task-"}

// Metapackages whose dependency trees make up a stock install (spec §15.4).
var baseMetas = []string{
	"ubuntu-minimal", "ubuntu-standard", "ubuntu-server",
	"ubuntu-desktop", "ubuntu-desktop-minimal",
	"task-desktop", "task-gnome-desktop", "task-kde-desktop", "task-xfce-desktop",
}

// Baseline returns the packages a fresh OS install already has.
func Baseline(ctx context.Context, r *runner.Runner) (map[string]bool, error) {
	return baseline(ctx, r, initialStatus)
}

func baseline(ctx context.Context, r *runner.Runner, snapshot string) (map[string]bool, error) {
	base := map[string]bool{}
	installed, err := installedPriorities(ctx, r)

	if err != nil {
		return nil, err
	}

	var metas []string

	for pkg, prio := range installed {
		if basePriorities[prio] || hasBasePrefix(pkg) {
			base[pkg] = true
		}
	}

	for _, m := range baseMetas {
		if _, ok := installed[m]; ok {
			metas = append(metas, m)
		}
	}

	if len(metas) > 0 {
		deps, err := recursiveDeps(ctx, r, metas)

		if err != nil {
			return nil, err
		}

		for _, d := range deps {
			base[d] = true
		}
	}

	initial, _ := readInitialStatus(snapshot)
	for _, pkg := range initial {
		base[pkg] = true
	}

	return base, nil
}

func installedPriorities(ctx context.Context, r *runner.Runner) (map[string]string, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f=${db:Status-Status} ${Package} ${Priority}\n"}})

	if err != nil {
		return nil, err
	}

	pkgs := map[string]string{}

	for _, l := range adapter.Lines(out) {
		f := strings.Fields(l)

		if len(f) >= 2 && f[0] == "installed" {
			pkgs[f[1]] = strings.Join(f[2:], "")
		}
	}

	return pkgs, nil
}

func recursiveDeps(ctx context.Context, r *runner.Runner, metas []string) ([]string, error) {
	args := []string{"depends", "--recurse", "--installed", "--no-suggests", "--no-conflicts", "--no-breaks", "--no-replaces", "--no-enhances"}
	out, err := r.Output(ctx, runner.Cmd{Name: "apt-cache", Args: append(args, metas...)})

	if err != nil {
		return nil, err
	}

	var deps []string

	for _, l := range strings.Split(out, "\n") {
		if l == "" || l[0] == ' ' || strings.HasPrefix(l, "<") {
			continue
		}

		deps = append(deps, strings.TrimSpace(l))
	}

	return deps, nil
}

// dropDependedLibs removes runtime libraries marked manual that another installed package depends on.
func dropDependedLibs(ctx context.Context, r *runner.Runner, pkgs []string) ([]string, error) {
	var libs []string

	for _, p := range pkgs {
		if isRuntimeLib(p) {
			libs = append(libs, p)
		}
	}

	if len(libs) == 0 {
		return pkgs, nil
	}

	args := []string{"rdepends", "--installed", "--no-suggests", "--no-enhances", "--no-conflicts", "--no-breaks", "--no-replaces"}
	out, err := r.Output(ctx, runner.Cmd{Name: "apt-cache", Args: append(args, libs...)})

	if err != nil {
		return nil, err
	}

	depended := map[string]bool{}
	var current string

	for _, l := range strings.Split(out, "\n") {
		switch {
		case l == "" || strings.HasPrefix(l, "Reverse Depends:"):
		case l[0] != ' ':
			current = strings.TrimSpace(l)
		default:
			depended[current] = true
		}
	}

	kept := pkgs[:0]

	for _, p := range pkgs {
		if !depended[p] {
			kept = append(kept, p)
		}
	}

	return kept, nil
}

func isRuntimeLib(pkg string) bool {
	name, _, _ := strings.Cut(pkg, ":")

	return strings.HasPrefix(name, "lib") && !strings.HasSuffix(name, "-dev") ||
		strings.HasSuffix(name, "-data") ||
		strings.HasPrefix(name, "steam-libs-")
}

func readInitialStatus(path string) ([]string, error) {
	f, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	gz, err := gzip.NewReader(f)

	if err != nil {
		return nil, err
	}

	defer gz.Close()

	var pkgs []string
	sc := bufio.NewScanner(gz)

	for sc.Scan() {
		if name, ok := strings.CutPrefix(sc.Text(), "Package: "); ok {
			pkgs = append(pkgs, name)
		}
	}

	if err := sc.Err(); err != nil {
		return nil, err
	}
	return pkgs, nil
}

func hasBasePrefix(pkg string) bool {
	for _, p := range basePrefixes {
		if strings.HasPrefix(pkg, p) {
			return true
		}
	}

	return false
}

func BaselineWarning(ctx context.Context, r *runner.Runner) string {
	if initial, err := readInitialStatus(initialStatus); err == nil && len(initial) > 0 {
		return ""
	}
	installed, err := installedPriorities(ctx, r)
	if err != nil {
		return ""
	}
	return baselineWarning(installed)
}

func baselineWarning(installed map[string]string) string {
	for _, meta := range baseMetas {
		if meta != "ubuntu-minimal" && meta != "ubuntu-standard" {
			if _, ok := installed[meta]; ok {
				return ""
			}
		}
	}
	return "installer package snapshot is unavailable and no desktop/server metapackage is installed; additional base-system packages may appear as new"
}
