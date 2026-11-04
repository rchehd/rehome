package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rchehd/rehome/internal/lock"
	"github.com/rchehd/rehome/internal/runner"
	"github.com/rchehd/rehome/internal/secrets"
)

const (
	DconfSnapshot = "files/gnome-settings/dconf.ini"
	dconfPath     = "/org/gnome/"
)

var (
	dconfSections = []string{"desktop", "shell", "mutter", "settings-daemon/plugins/media-keys", "terminal/legacy"}
	dconfSection  = regexp.MustCompile(`^[A-Za-z0-9_:-]+(/[A-Za-z0-9_:-]+)*$`)
	dconfKey      = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type dconfSettings map[string]map[string]string

func GNOMEAvailable() bool {
	for _, desktop := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if strings.EqualFold(desktop, "GNOME") {
			return true
		}
	}

	_, err := runner.LookPath("gnome-shell")

	return err == nil
}

func CaptureDconf(ctx context.Context, r *runner.Runner, root string) (*Change, bool, error) {
	settings, err := readDconf(ctx, r)

	if err != nil {
		return nil, false, err
	}

	raw := formatDconf(settings)
	old, err := os.ReadFile(filepath.Join(root, DconfSnapshot))

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}

	data, findings := secrets.Redact(DconfSnapshot, raw)

	if bytes.Equal(old, data) {
		return nil, old != nil, nil
	}

	return &Change{Src: "dconf:" + dconfPath, Dst: DconfSnapshot, Old: old, New: data, Mode: 0o600, Findings: findings}, true, nil
}

func applyDconf(ctx context.Context, root, src string, opts ApplyOptions) ApplyResult {
	res := ApplyResult{Src: src, Dest: "dconf:" + dconfPath}

	if opts.FileKey != "linux" {
		res.Status = StatusNoDest

		return res
	}

	if !GNOMEAvailable() {
		res.Status, res.Problem = StatusDconfSkip, "GNOME is not installed on this machine"

		return res
	}

	status, backup, redacted, err := restoreDconf(ctx, root, src, opts)
	res.Status, res.Backup, res.Redacted = status, backup, redacted

	if err != nil {
		res.Status, res.Problem = StatusDconfError, err.Error()
	}

	return res
}

func restoreDconf(ctx context.Context, root, src string, opts ApplyOptions) (status, backup string, redacted bool, err error) {
	dir, err := os.OpenRoot(root)

	if err != nil {
		return "", "", false, err
	}

	defer dir.Close()

	data, err := dir.ReadFile(src)

	if err != nil {
		return "", "", false, err
	}

	wanted := parseDconf(string(data))
	r := opts.Runner

	if r == nil {
		r = &runner.Runner{}
	}

	local, err := readDconf(ctx, r)

	if err != nil {
		return "", "", false, err
	}

	conflict := false

	for section, keys := range wanted {
		for key, value := range keys {
			current, exists := local[section][key]

			switch {
			case strings.Contains(value, secrets.Placeholder):
				redacted = redacted || !exists
				delete(keys, key)
			case exists && current == value:
				delete(keys, key)
			case exists:
				conflict = true
			}
		}

		if len(keys) == 0 {
			delete(wanted, section)
		}
	}

	switch {
	case len(wanted) == 0:
		return StatusIdentical, "", redacted, nil
	case conflict && !opts.Overwrite:
		return StatusKeptLocal, "", redacted, nil
	case opts.DryRun || r.DryRun:
		if conflict {
			return StatusOverwritten, "", redacted, nil
		}

		return StatusCopied, "", redacted, nil
	}

	cmd := runner.Cmd{Name: "dconf", Args: []string{"load", dconfPath}, Stdin: bytes.NewReader(formatDconf(wanted))}

	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		if _, err := runner.LookPath("dbus-run-session"); err != nil {
			return "", "", redacted, errors.New("GNOME settings need a D-Bus session or dbus-run-session")
		}

		cmd.Name, cmd.Args = "dbus-run-session", append([]string{"--", "dconf"}, cmd.Args...)
	}

	status = StatusCopied

	if conflict {
		status = StatusOverwritten
		backup, err = backupDconf(formatDconf(local))

		if err != nil {
			return "", "", redacted, fmt.Errorf("could not back up GNOME settings: %w", err)
		}
	}

	if _, err := r.Run(ctx, cmd); err != nil {
		return "", backup, redacted, errors.New("could not load GNOME settings through dconf")
	}

	return status, backup, redacted, nil
}

func readDconf(ctx context.Context, r *runner.Runner) (dconfSettings, error) {
	if _, err := runner.LookPath("dconf"); err != nil {
		return nil, errors.New("install dconf-cli to read GNOME settings")
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "dconf", Args: []string{"dump", dconfPath}})

	if err != nil {
		return nil, errors.New("could not read GNOME settings through dconf")
	}

	return parseDconf(out), nil
}

func parseDconf(data string) dconfSettings {
	settings := dconfSettings{}
	section := ""

	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = ""
			name := line[1 : len(line)-1]

			if dconfSection.MatchString(name) && slices.ContainsFunc(dconfSections, func(prefix string) bool {
				return name == prefix || strings.HasPrefix(name, prefix+"/")
			}) {
				section = name
			}

			continue
		}

		key, value, ok := strings.Cut(line, "=")

		if section == "" || !ok || !dconfKey.MatchString(key) {
			continue
		}

		if settings[section] == nil {
			settings[section] = map[string]string{}
		}

		settings[section][key] = value
	}

	return settings
}

func formatDconf(settings dconfSettings) []byte {
	var out bytes.Buffer

	for _, section := range slices.Sorted(maps.Keys(settings)) {
		fmt.Fprintf(&out, "[%s]\n", section)

		for _, key := range slices.Sorted(maps.Keys(settings[section])) {
			fmt.Fprintf(&out, "%s=%s\n", key, settings[section][key])
		}

		out.WriteByte('\n')
	}

	return out.Bytes()
}

func backupDconf(data []byte) (string, error) {
	path, err := lock.LocalPath()

	if err != nil {
		return "", err
	}

	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	file, err := os.CreateTemp(dir, "gnome-dconf-*.ini")

	if err != nil {
		return "", err
	}

	if err := writeSecretTemp(file, data); err != nil {
		os.Remove(file.Name())

		return "", err
	}

	return file.Name(), nil
}
