package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/app"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Change the config without editing YAML (comments are kept)",
	}
	cmd.AddCommand(newAddInstallCmd(), newAddCaptureCmd(), newSetCmd())

	return cmd
}

func newAddInstallCmd() *cobra.Command {
	var spec config.InstallSpec
	var platformKey, group string

	cmd := &cobra.Command{
		Use:   "add-install <app>",
		Short: "Set how an app installs on a platform (creates the app if needed)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if name, _ := spec.Manager(); name == "" {
				return errors.New("give one of --apt, --snap, --flatpak, --npm, --pipx, --uv-tool, --cargo, --go, --script")
			}

			if platformKey == "" {
				p, err := platform.Detect()

				if err != nil {
					return err
				}

				platformKey = p.Key()
			}

			return editConfig(cmd, func(path string) error {
				return config.AddInstall(path, args[0], platformKey, spec, group)
			})
		},
	}

	fl := cmd.Flags()
	fl.StringVar(&platformKey, "platform", "", "debian, macos, fedora, arch or any (default: this machine)")
	fl.StringVar(&group, "group", "", "group for a new app")

	for _, f := range []struct {
		name  string
		value *string
		usage string
	}{
		{"apt", &spec.Apt, "apt package"}, {"snap", &spec.Snap, "snap name"}, {"flatpak", &spec.Flatpak, "flatpak app id"},
		{"npm", &spec.Npm, "global npm package"}, {"pipx", &spec.Pipx, "pipx package"}, {"uv-tool", &spec.UvTool, "uv tool"},
		{"cargo", &spec.Cargo, "cargo crate"}, {"go", &spec.GoPkg, "go install package"}, {"script", &spec.Script, "install command"},
		{"repo", &spec.Repo, "apt repo name (from apt_repos)"}, {"deb-url", &spec.DebURL, "https URL of a .deb (with --apt)"},
		{"channel", &spec.Channel, "snap channel"}, {"remote", &spec.Remote, "flatpak remote"}, {"remote-url", &spec.RemoteURL, "flatpak remote URL"},
		{"bin", &spec.Bin, "binary name or path that proves the install"},
	} {
		fl.StringVar(f.value, f.name, "", f.usage)
	}

	fl.BoolVar(&spec.Classic, "classic", false, "snap classic confinement")

	return cmd
}

func newAddCaptureCmd() *cobra.Command {
	var rule config.FileRule
	var fileKey string

	cmd := &cobra.Command{
		Use:   "add-capture <app>",
		Short: "Add a settings rule so scan copies these files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if fileKey == "" {
				p, err := platform.Detect()

				if err != nil {
					return err
				}

				fileKey = p.FileKey()
			}

			return editConfig(cmd, func(path string) error {
				return config.AddCapture(path, args[0], fileKey, rule)
			})
		},
	}

	fl := cmd.Flags()
	fl.StringVar(&rule.Base, "base", "", "folder the globs are relative to, e.g. ~/.config/myapp")
	fl.StringSliceVar(&rule.Include, "include", nil, "glob to copy (repeatable; ** spans folders)")
	fl.StringSliceVar(&rule.Exclude, "exclude", nil, "glob to skip (repeatable)")
	fl.StringVar(&fileKey, "file-key", "", "linux or macos (default: this machine)")

	return cmd
}

func newSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <app> key=value...",
		Short: "Set enabled, skip (comma list), version or group; an empty value removes it",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fields := map[string]string{}

			for _, kv := range args[1:] {
				key, value, ok := strings.Cut(kv, "=")

				if !ok {
					return errors.New("expected key=value, got " + kv)
				}

				fields[key] = value
			}

			return editConfig(cmd, func(path string) error {
				return config.SetApp(path, args[0], fields)
			})
		},
	}
}

func editConfig(cmd *cobra.Command, edit func(path string) error) error {
	path, err := app.ConfigPath(opts(cmd))

	if err != nil {
		return err
	}

	if err := edit(path); err != nil {
		return err
	}

	return report(cmd, map[string]string{"updated": path}, "updated "+path)
}
