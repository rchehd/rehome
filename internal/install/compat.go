package install

import (
	"context"
	"fmt"
	"strings"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/all"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/platform"
	"github.com/rchehd/rehome/internal/resolve"
)

// Reason codes say why an app cannot be installed on this machine.
const (
	ReasonNoRecipe = "no-recipe"
	ReasonRepo     = "needs-repo"
	ReasonArch     = "wrong-arch"
	ReasonManager  = "no-manager"
	ReasonWSL      = "wsl"
)

type Reason struct {
	Code string
	Text string
}

var platformNames = map[string]string{"debian": "Debian/Ubuntu", "macos": "macOS", "fedora": "Fedora", "arch": "Arch Linux"}

// compatible finds how app installs here: its recipe, then the catalog, then a portable manager
// it uses elsewhere, then the package indexes; otherwise it says why it cannot.
func compatible(ctx context.Context, cfg *config.Config, app config.App, p platform.Platform, res *resolve.Resolver) (config.InstallSpec, adapter.Adapter, string, Reason) {
	spec, ok := app.Spec(p.Key())
	mapped := ""

	if reason := res.Catalog.WSLReason(p, app, spec); reason != "" {
		return spec, nil, "", Reason{ReasonWSL, reason}
	}

	switch {
	case !ok && len(app.Install) == 0:
		return spec, nil, "", Reason{ReasonNoRecipe, "no recipe: add one (rehome config add-install " + app.ID + " …)"}
	case !ok:
		if spec, mapped, ok = res.Resolve(ctx, app); !ok {
			name := platformNames[p.Key()]

			if name == "" {
				name = p.Key()
			}

			return spec, nil, "", Reason{ReasonNoRecipe, fmt.Sprintf("no recipe for %s: add install.%s (rehome config add-install %s …)", name, p.Key(), app.ID)}
		}
	}

	if reason := res.Catalog.WSLReason(p, app, spec); reason != "" {
		return spec, nil, mapped, Reason{ReasonWSL, reason}
	}

	if spec.Repo != "" && cfg.AptRepos[spec.Repo].Source == "" {
		return spec, nil, mapped, Reason{ReasonRepo, fmt.Sprintf("needs the apt repo %s, which rehome.yml does not define", spec.Repo)}
	}

	if arch := builtFor(cfg, spec); arch != "" && arch != p.Arch {
		return spec, nil, mapped, Reason{ReasonArch, fmt.Sprintf("built for %s, this machine is %s", arch, p.Arch)}
	}

	a, err := all.ByManager(spec)

	if err != nil || !a.Available(p) {
		return spec, nil, mapped, Reason{ReasonManager, fmt.Sprintf("manager %s is not available here", managerName(spec))}
	}

	return spec, a, mapped, Reason{}
}

// builtFor is the architecture a .deb URL or an apt source line is limited to, if any.
func builtFor(cfg *config.Config, spec config.InstallSpec) string {
	for _, arch := range []string{"amd64", "arm64", "armhf", "i386"} {
		if strings.Contains(spec.DebURL, "_"+arch+".deb") {
			return goArch(arch)
		}
	}

	if spec.Repo == "" {
		return ""
	}

	source := cfg.AptRepos[spec.Repo].Source
	i := strings.Index(source, "arch=")

	if i < 0 {
		return ""
	}

	arch := source[i+len("arch="):]
	arch = arch[:strings.IndexAny(arch+" ]", " ]")]

	if strings.Contains(arch, ",") {
		return ""
	}

	return goArch(arch)
}

// goArch names a Debian architecture the way Go does.
func goArch(debian string) string {
	switch debian {
	case "armhf":
		return "arm"
	case "i386":
		return "386"
	}

	return debian
}
