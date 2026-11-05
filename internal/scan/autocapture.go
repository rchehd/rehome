package scan

import (
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/rchehd/rehome/internal/adapter"
	"github.com/rchehd/rehome/internal/adapter/apt"
	"github.com/rchehd/rehome/internal/catalog"
	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/discover"
	"github.com/rchehd/rehome/internal/files"
	"github.com/rchehd/rehome/internal/progress"
)

// captureByRules takes settings of apps the catalog does not cover: the config's own or
// earlier capture rule first, else what discover finds; what it cannot take becomes Attention.
func (s *scanner) captureByRules() {
	key := s.p.FileKey()
	claimed := discover.Claimed(catalogRules(s.cat, key))

	for _, id := range slices.Sorted(maps.Keys(s.apps)) {
		app := s.apps[id]

		if app.Files != "" {
			continue
		}

		progress.Step(s.ctx, "reading settings: %s", id)
		rules, ok := s.existing.Capture[id]

		if !ok {
			rule, warn := discover.ForApp(id, s.names(app), claimed)

			// Browser profiles are reported once, by unclaimedConfigDirs.
			if warn != nil && !slices.Contains(browsers, filepath.Base(warn.Path)) {
				s.res.Attention = append(s.res.Attention, *warn)
			}

			if rule == nil {
				continue
			}

			rules = map[string]config.FileRule{key: *rule}
		}

		rules = maps.Clone(rules)
		rules[key] = rules[key].WithAutoDefaults()

		changes, fileMaps, err := files.CaptureRules(id, rules, key, s.root, s.options.HarnessData)

		if err != nil {
			s.warn("%s settings: %v", id, err)

			continue
		}

		if len(fileMaps) == 0 {
			continue
		}

		if s.res.Config.Capture == nil {
			s.res.Config.Capture = map[string]map[string]config.FileRule{}
		}

		s.res.Config.Capture[id] = rules
		s.res.Config.Files[id] = fileMaps
		claimed = append(claimed, discover.Claimed([]config.FileRule{rules[key]})...)
		app.Files = id
		s.addChanges(changes)
	}
}

func (s *scanner) names(app *config.App) []string {
	pkg := ""
	var bins []string

	if spec, ok := app.Install[s.p.Key()]; ok {
		_, pkg = spec.Manager()

		if spec.Apt != "" {
			bins = apt.Binaries(s.ctx, s.r, spec.Apt)
		}
	}

	return discover.Names(app.ID, pkg, append(bins, s.hints[app.ID]...))
}

// Directories under ~/.config that hold desktop plumbing rather than app settings.
var plumbing = []string{"pulse", "ibus", "dconf", "systemd", "gnome-session", "enchant", "procps", "menus", "autostart", "goa-1.0", "evolution", "rehome", "session", "update-notifier", "yelp"}

// Browser profiles are huge and synced by the browser itself.
var browsers = []string{"google-chrome", "google-chrome-beta", "google-chrome-for-testing", "google-chrome-headless", "chromium", "BraveSoftware", "vivaldi", "opera", "microsoft-edge", "mozilla"}

// unclaimedConfigDirs reports ~/.config directories no rule takes, so nothing is lost silently.
func unclaimedConfigDirs(cfg *config.Config, cat *catalog.Catalog, key string) []discover.Warning {
	entries, err := os.ReadDir(adapter.Home("~/.config"))

	if err != nil {
		return nil
	}

	rules := catalogRules(cat, key)

	for _, byKey := range cfg.Capture {
		rules = append(rules, byKey[key])
	}

	claimed := discover.Claimed(rules)
	var out []discover.Warning

	for _, e := range entries {
		dir := filepath.Join(adapter.Home("~/.config"), e.Name())

		if !e.IsDir() || slices.Contains(plumbing, e.Name()) || discover.Covered(dir, claimed) {
			continue
		}

		if slices.Contains(browsers, e.Name()) {
			out = append(out, discover.Warning{
				Path:   "~/.config/" + e.Name(),
				Reason: "browser profile, not copied",
				Action: "sign in to the browser's own sync on the new machine",
			})

			continue
		}

		if !discover.HasSettings(dir) {
			continue
		}

		out = append(out, discover.Warning{
			Path:   "~/.config/" + e.Name(),
			Reason: "no app claims it",
			Action: "add a capture rule if it holds settings you want to keep",
		})
	}

	return out
}

func catalogRules(cat *catalog.Catalog, key string) []config.FileRule {
	var rules []config.FileRule

	for _, e := range cat.Entries {
		if r, ok := e.Rule(key); ok {
			rules = append(rules, r)
		}
	}

	return rules
}
