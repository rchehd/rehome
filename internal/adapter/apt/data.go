package apt

import (
	"bufio"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/runner"
)

var (
	dpkgInfoDir    = "/var/lib/dpkg/info"
	aptHistoryGlob = "/var/log/apt/history.log*"
)

var dataSections = []string{"libs", "oldlibs", "fonts", "localization", "translations", "text"}

var dataPrefixes = []string{"ibus-table-", "m17n-", "fonts-", "xfonts-", "hunspell-", "hyphen-", "mythes-"}

var programPath = regexp.MustCompile(`^/(usr/)?(s?bin|games)/[^/]+$|^/usr/share/applications/[^/]+\.desktop$|^/opt/`)

// dropDataPackages removes libraries, fonts and language data that the user never asked for and that ship no program.
func dropDataPackages(ctx context.Context, r *runner.Runner, pkgs []string) ([]string, error) {
	if len(pkgs) == 0 {
		return pkgs, nil
	}

	secs, err := sections(ctx, r, pkgs)

	if err != nil {
		return pkgs, nil
	}

	asked := requested()
	kept := pkgs[:0]

	for _, p := range pkgs {
		name, _, _ := strings.Cut(p, ":")

		if isDataPackage(name, secs[name]) && !asked[name] && !shipsProgram(name) {
			continue
		}

		kept = append(kept, p)
	}

	return kept, nil
}

func sections(ctx context.Context, r *runner.Runner, pkgs []string) (map[string]string, error) {
	args := append([]string{"-W", "-f=${Package}\t${Section}\n"}, pkgs...)
	out, err := r.Output(ctx, runner.Cmd{Name: "dpkg-query", Args: args})

	if err != nil {
		return nil, err
	}

	secs := map[string]string{}

	for _, l := range adapter.Lines(out) {
		if name, sec, ok := strings.Cut(l, "\t"); ok {
			secs[name] = strings.TrimSpace(sec)
		}
	}

	return secs, nil
}

func isDataPackage(name, section string) bool {
	// Sections may carry an archive area prefix such as universe/libs.
	section = section[strings.LastIndex(section, "/")+1:]

	return slices.Contains(dataSections, section) ||
		slices.ContainsFunc(dataPrefixes, func(p string) bool { return strings.HasPrefix(name, p) })
}

// shipsProgram reports whether a package installs a command, a launcher or an /opt tree; unknown packages count as programs.
func shipsProgram(name string) bool {
	files, ok := packageFiles(name)

	return !ok || slices.ContainsFunc(files, programPath.MatchString)
}

// packageFiles reads the paths dpkg recorded for a package; ok is false when it has no readable file list.
func packageFiles(name string) ([]string, bool) {
	lists, _ := filepath.Glob(filepath.Join(dpkgInfoDir, name+".list"))
	archLists, _ := filepath.Glob(filepath.Join(dpkgInfoDir, name+":*.list"))
	lists = append(lists, archLists...)

	if len(lists) == 0 {
		return nil, false
	}

	var files []string

	for _, path := range lists {
		lines, err := readLines(path)

		if err != nil {
			return nil, false
		}

		files = append(files, lines...)
	}

	return files, true
}

// requested returns the packages named on an apt install command line in the apt history.
func requested() map[string]bool {
	asked := map[string]bool{}
	logs, _ := filepath.Glob(aptHistoryGlob)

	for _, path := range logs {
		lines, err := readLines(path)

		if err != nil {
			continue
		}

		for _, l := range lines {
			cmd, ok := strings.CutPrefix(l, "Commandline: ")

			if !ok {
				continue
			}

			words := strings.Fields(cmd)
			i := slices.Index(words, "install")

			if i < 0 {
				continue
			}

			for _, w := range words[i+1:] {
				if strings.HasPrefix(w, "-") {
					continue
				}

				name, _, _ := strings.Cut(w, "=")
				name, _, _ = strings.Cut(name, ":")
				asked[name] = true
			}
		}
	}

	return asked
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	var src io.Reader = f

	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)

		if err != nil {
			return nil, err
		}

		defer gz.Close()

		src = gz
	}

	var lines []string
	sc := bufio.NewScanner(src)

	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	return lines, sc.Err()
}
