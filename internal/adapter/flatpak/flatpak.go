package flatpak

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

type Flatpak struct{}

func (Flatpak) Name() string { return "flatpak" }

// Available does not require the binary: an apt step earlier in the same run may install it.
func (Flatpak) Available(p platform.Platform) bool {
	return p.OS == "linux"
}

func (Flatpak) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	if !adapter.HasCmd("flatpak") {
		return nil, nil
	}

	remotes, err := listRemotes(ctx, r)

	if err != nil {
		return nil, err
	}

	out, err := r.Output(ctx, runner.Cmd{Name: "flatpak", Args: []string{"list", "--app", "--columns=application,origin"}})

	if err != nil {
		return nil, err
	}

	var found []adapter.Detected

	for _, l := range adapter.Lines(out) {
		f := strings.Fields(l)

		if len(f) < 2 {
			continue
		}

		spec := config.InstallSpec{Flatpak: f[0], Remote: f[1], RemoteURL: repoFile(f[1], remotes[f[1]])}
		found = append(found, adapter.Detected{Manager: "flatpak", Name: f[0], Spec: spec, Requires: []string{"flatpak"}})
	}

	return found, nil
}

func (Flatpak) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	_, err := r.Output(ctx, runner.Cmd{Name: "flatpak", Args: []string{"info", s.Flatpak}})

	return err == nil, nil
}

func (Flatpak) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	remote := s.Remote

	if remote == "" {
		remote = "flathub"
	}

	if s.RemoteURL != "" {
		add := runner.Cmd{Name: "flatpak", Args: []string{"remote-add", "--if-not-exists", remote, s.RemoteURL}, Sudo: true}

		if _, err := r.Run(ctx, add); err != nil {
			return err
		}
	}

	_, err := r.Run(ctx, runner.Cmd{Name: "flatpak", Args: []string{"install", "-y", "--noninteractive", remote, s.Flatpak}, Sudo: true})

	return err
}

// repoFile points remote-add at the .flatpakrepo descriptor, which carries the GPG key;
// a bare repo URL would leave the remote unverifiable.
func repoFile(name, url string) string {
	if url == "" || strings.HasSuffix(url, ".flatpakrepo") {
		return url
	}

	return strings.TrimSuffix(url, "/") + "/" + name + ".flatpakrepo"
}

func listRemotes(ctx context.Context, r *runner.Runner) (map[string]string, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "flatpak", Args: []string{"remotes", "--columns=name,url"}})

	if err != nil {
		return nil, err
	}

	remotes := map[string]string{}

	for _, l := range adapter.Lines(out) {
		if f := strings.Fields(l); len(f) >= 2 {
			remotes[f[0]] = f[1]
		}
	}

	return remotes, nil
}
