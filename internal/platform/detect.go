package platform

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
)

const osReleasePath = "/etc/os-release"

type Platform struct {
	OS       string
	ID       string
	IDLike   []string
	Version  string
	Codename string
	Arch     string
	WSL      bool
	Systemd  bool
}

// Detect identifies the running OS, distribution and architecture.
func Detect() (Platform, error) {
	p := Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}

	if p.OS == "darwin" {
		p.ID = "macos"

		return p, nil
	}

	if p.OS != "linux" {
		return p, fmt.Errorf("unsupported platform: %s", p.OS)
	}

	fields, err := readOSRelease(osReleasePath)

	if err != nil {
		return p, err
	}

	p.ID = fields["ID"]
	p.IDLike = strings.Fields(fields["ID_LIKE"])
	p.Version = fields["VERSION_ID"]
	p.Codename = fields["VERSION_CODENAME"]

	kernel, _ := os.ReadFile("/proc/version")
	p.WSL = isWSL(string(kernel)) || os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != ""

	if p.WSL {
		init, _ := os.ReadFile("/proc/1/comm")
		p.Systemd = strings.TrimSpace(string(init)) == "systemd"
	}

	if p.Key() == "" {
		return p, fmt.Errorf("unsupported platform: %s", p.ID)
	}

	return p, nil
}

// Key is the config key for install specs on this platform.
func (p Platform) Key() string {
	if p.OS == "darwin" {
		return "macos"
	}

	if p.Is("debian", "ubuntu") {
		return "debian"
	}

	return ""
}

// Is reports whether the distribution or one it derives from is among ids.
func (p Platform) Is(ids ...string) bool {
	for _, id := range ids {
		if p.ID == id || slices.Contains(p.IDLike, id) {
			return true
		}
	}

	return false
}

// FileKey selects the destination key of a file mapping.
func (p Platform) FileKey() string {
	if p.OS == "darwin" {
		return "macos"
	}

	return "linux"
}

func (p Platform) String() string {
	name := strings.TrimSpace(fmt.Sprintf("%s %s %s %s %s", p.Key(), p.ID, p.Version, p.Codename, p.Arch))

	if p.WSL {
		name += " (WSL)"
	}

	return name
}

func isWSL(kernel string) bool {
	return strings.Contains(strings.ToLower(kernel), "microsoft")
}

func readOSRelease(path string) (map[string]string, error) {
	f, err := os.Open(path)

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	defer f.Close()

	fields := map[string]string{}
	sc := bufio.NewScanner(f)

	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), "=")

		if ok {
			fields[key] = strings.Trim(val, `"'`)
		}
	}

	return fields, sc.Err()
}
