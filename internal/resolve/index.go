package resolve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/apt"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/runner"
)

// BrewIndex asks Homebrew whether a formula or cask exists.
type BrewIndex struct {
	Runner  *runner.Runner
	Offline bool
	// APIURL is formulae.brew.sh's API root; tests point it elsewhere.
	APIURL string
}

func (b BrewIndex) api(ctx context.Context, kind, name string) string {
	base := b.APIURL

	if base == "" {
		base = "https://formulae.brew.sh/api/"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+kind+"/"+url.PathEscape(name)+".json", nil)

	if err != nil {
		return ""
	}

	req.Header.Set("User-Agent", userAgent)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)

	if err != nil {
		return ""
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	return name
}

func (b BrewIndex) Lookup(ctx context.Context, name string, preferGUI bool) (config.InstallSpec, bool) {
	var formula, cask string

	// On a fresh Mac Homebrew is not installed yet; its public API answers the same question.
	if adapter.HasCmd("brew") {
		out, err := b.Runner.Output(ctx, runner.Cmd{Name: "brew", Args: []string{"info", "--json=v2", name}})

		if err != nil {
			return config.InstallSpec{}, false
		}

		formula, cask = parseBrewInfo(out)
	} else if !b.Offline {
		formula, cask = b.api(ctx, "formula", name), b.api(ctx, "cask", name)
	}

	switch {
	case cask != "" && (preferGUI || formula == ""):
		return config.InstallSpec{Cask: cask}, true
	case formula != "":
		return config.InstallSpec{Brew: formula}, true
	}

	return config.InstallSpec{}, false
}

// parseBrewInfo reads the formula and cask names from `brew info --json=v2`.
func parseBrewInfo(out string) (string, string) {
	var info struct {
		Formulae []struct {
			Name string `json:"name"`
		} `json:"formulae"`
		Casks []struct {
			Token string `json:"token"`
		} `json:"casks"`
	}

	if json.Unmarshal([]byte(out), &info) != nil {
		return "", ""
	}

	formula, cask := "", ""

	if len(info.Formulae) > 0 {
		formula = info.Formulae[0].Name
	}

	if len(info.Casks) > 0 {
		cask = info.Casks[0].Token
	}

	return formula, cask
}

// AptIndex asks apt whether a package has an installation candidate.
type AptIndex struct {
	Runner *runner.Runner
}

func (a AptIndex) Lookup(ctx context.Context, name string, _ bool) (config.InstallSpec, bool) {
	missing, err := apt.Unavailable(ctx, a.Runner, []string{name})

	if err != nil || len(missing) > 0 {
		return config.InstallSpec{}, false
	}

	return config.InstallSpec{Apt: name}, true
}
