package status

import (
	"fmt"

	"github.com/rchehd/rehome/internal/install"
	"github.com/rchehd/rehome/internal/lock"
)

func (s *Status) compareVersions(steps []install.Step, current map[string]lock.Entry, repo, local *lock.Lock, platform string) {
	_, s.NotSynced = lock.Sync(repo, local, current, platform)
	s.Versions = map[string]AppVersions{}
	for id, installed := range current {
		recorded, _ := repo.Get(id, platform)
		s.Versions[id] = AppVersions{Installed: installed, Recorded: recorded}
	}
	for _, step := range steps {
		if step.Status == install.StatusDisabled || step.Status == install.StatusIgnored || step.Status == install.StatusUnavailable {
			continue
		}
		v := s.Versions[step.App.ID]
		v.Pinned = step.App.Version
		v.Recorded, _ = repo.Get(step.App.ID, platform)
		if v.Installed.Version == "" && step.Installed != "" {
			manager, _ := step.Spec.Manager()
			v.Installed = lock.Entry{Manager: manager, Version: step.Installed}
		}
		s.Versions[step.App.ID] = v
		if step.Upgrade != (lock.Entry{}) {
			s.Behind = append(s.Behind, fmt.Sprintf("%s %s → %s", step.App.ID, step.Installed, step.Upgrade.Version))
		}
	}
}

func versionText(entry lock.Entry, fallback string) string {
	if entry.Unsupported {
		return "unavailable"
	}
	text := entry.Version
	if entry.Commit != "" {
		if text != "" {
			text += " "
		}
		text += "@" + entry.Commit[:min(12, len(entry.Commit))]
	}
	if text == "" {
		return fallback
	}
	return text
}
