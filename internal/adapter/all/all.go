package all

import (
	"fmt"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/apt"
	"github.com/rchehd/rehome/internal/adapter/brew"
	"github.com/rchehd/rehome/internal/adapter/cargo"
	"github.com/rchehd/rehome/internal/adapter/flatpak"
	"github.com/rchehd/rehome/internal/adapter/gotool"
	"github.com/rchehd/rehome/internal/adapter/mas"
	"github.com/rchehd/rehome/internal/adapter/npm"
	"github.com/rchehd/rehome/internal/adapter/pipx"
	"github.com/rchehd/rehome/internal/adapter/script"
	"github.com/rchehd/rehome/internal/adapter/snap"
	"github.com/rchehd/rehome/internal/adapter/uvtool"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
)

// registry lists every adapter in install order (spec §10).
func registry() []adapter.Adapter {
	return []adapter.Adapter{
		apt.Apt{},
		snap.Snap{},
		flatpak.Flatpak{},
		npm.Npm{},
		pipx.Pipx{},
		uvtool.UvTool{},
		cargo.Cargo{},
		gotool.GoTool{},
		brew.Brew{},
		brew.Cask{},
		mas.Mas{},
		script.Script{},
	}
}

// For returns the adapters usable on the platform, in install order.
func For(p platform.Platform) []adapter.Adapter {
	var out []adapter.Adapter

	for _, a := range registry() {
		if a.Available(p) {
			out = append(out, a)
		}
	}

	return out
}

// ByManager returns the adapter that handles the spec.
func ByManager(s config.InstallSpec) (adapter.Adapter, error) {
	name, _ := s.Manager()

	for _, a := range registry() {
		if a.Name() == name {
			return a, nil
		}
	}

	return nil, fmt.Errorf("no adapter for manager %q", name)
}

// Order is the install position of a manager; unknown managers sort last.
func Order(manager string) int {
	for i, a := range registry() {
		if a.Name() == manager {
			return i
		}
	}

	return len(registry())
}
