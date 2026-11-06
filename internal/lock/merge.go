package lock

import "slices"

// Sync folds this machine's current entries into the repo lock without rolling back what
// another machine recorded: a version wins only if it is newer; anything not orderable
// (a git commit, a switched manager, unsupported) wins only if it changed here since the
// last sync recorded in local.
func Sync(repo, local *Lock, current map[string]Entry, platform string) (*Lock, []string) {
	merged := repo.Clone()
	var changed []string

	for id, cur := range current {
		r, inRepo := repo.Get(id, platform)

		if inRepo && !replaces(cur, r, local, id, platform) {
			continue
		}

		merged.Set(id, platform, cur)
		changed = append(changed, id)
	}

	slices.Sort(changed)

	return merged, changed
}

func replaces(cur, repo Entry, local *Lock, id, platform string) bool {
	if cur == repo {
		return false
	}

	if orderable(cur, repo) {
		return Compare(cur.Manager, cur.Version, repo.Version) > 0
	}

	l, inLocal := local.Get(id, platform)

	return !inLocal || l != cur
}

func orderable(a, b Entry) bool {
	return a.Manager == b.Manager && a.Version != "" && b.Version != "" &&
		a.Commit == "" && b.Commit == "" && !a.Unsupported && !b.Unsupported
}
