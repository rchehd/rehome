package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// User bin dirs that installers in this same run populate before the shell's PATH knows them.
var extraDirs = []string{"~/.local/bin", "~/.cargo/bin", "~/go/bin", "/usr/local/go/bin", "/snap/bin"}

// SearchPath is PATH plus the user bin dirs that are missing from it.
func SearchPath() string {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	home, _ := os.UserHomeDir()

	for _, d := range extraDirs {
		d = strings.Replace(d, "~", home, 1)

		if !contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}

	return strings.Join(dirs, string(os.PathListSeparator))
}

// LookPath finds a command on SearchPath.
func LookPath(name string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}

	for _, dir := range filepath.SplitList(SearchPath()) {
		path := filepath.Join(dir, name)

		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}

	return "", exec.ErrNotFound
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}

	return false
}
