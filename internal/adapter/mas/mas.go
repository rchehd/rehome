package mas

import (
	"context"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/runner"
)

// Mas installs Mac App Store apps by id; it is used only when the mas CLI is present.
type Mas struct{}

func (Mas) Name() string { return "mas" }

func (Mas) Available(p platform.Platform) bool {
	return p.OS == "darwin" && adapter.HasCmd("mas")
}

func (Mas) Scan(ctx context.Context, r *runner.Runner) ([]adapter.Detected, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "mas", Args: []string{"list"}})

	if err != nil {
		return nil, err
	}

	var found []adapter.Detected

	for id, name := range parseList(out) {
		found = append(found, adapter.Detected{Manager: "mas", Name: name, Spec: config.InstallSpec{Mas: id}})
	}

	return found, nil
}

func (Mas) Installed(ctx context.Context, r *runner.Runner, s config.InstallSpec) (bool, error) {
	out, err := r.Output(ctx, runner.Cmd{Name: "mas", Args: []string{"list"}})

	if err != nil {
		return false, nil
	}

	_, ok := parseList(out)[s.Mas]

	return ok, nil
}

func (Mas) Install(ctx context.Context, r *runner.Runner, s config.InstallSpec) error {
	_, err := r.Run(ctx, runner.Cmd{Name: "mas", Args: []string{"install", s.Mas}})

	return err
}

// parseList reads "497799835  Xcode  (15.0)" lines into id → app name.
func parseList(out string) map[string]string {
	apps := map[string]string{}

	for _, l := range adapter.Lines(out) {
		id, rest, ok := strings.Cut(l, " ")

		if !ok {
			continue
		}

		name := strings.TrimSpace(rest)

		if i := strings.LastIndex(name, " ("); i > 0 {
			name = strings.TrimSpace(name[:i])
		}

		apps[id] = strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	}

	return apps
}
