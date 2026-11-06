#!/bin/bash
# A v1 config in a shared repo: the first machine migrates it, a later one only reuses it, and two
# machines migrating at once end with one migration and the other kept on a branch.
set -Eeuo pipefail

rehome="$1"
fixture="$2"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
trap 'echo "Migrate E2E failed at line $LINENO" >&2; tail -n 25 "$work"/*.log >&2 2>/dev/null' ERR

export GIT_AUTHOR_NAME=e2e GIT_AUTHOR_EMAIL=e2e@example.test GIT_COMMITTER_NAME=e2e GIT_COMMITTER_EMAIL=e2e@example.test
unset XDG_CURRENT_DESKTOP DBUS_SESSION_BUS_ADDRESS WSL_DISTRO_NAME WSL_INTEROP

# origin NAME: a bare repo holding the v1 fixture.
origin() {
  cp -r "$fixture" "$work/seed-$1"
  git -C "$work/seed-$1" init -q -b main
  git -C "$work/seed-$1" add -A
  git -C "$work/seed-$1" commit -qm v1
  git clone -q --bare "$work/seed-$1" "$work/$1.git"
}

# machine NAME ORIGIN: a home, settings linked to ORIGIN as node NAME, and a clone of it.
machine() {
  mkdir -p "$work/$1/home" "$work/$1/config/rehome"
  git clone -q "$work/$2.git" "$work/$1/clone"
  printf 'repo_url: %s\nclone_path: %s\nnode_id: %s\nnode_name: %s\n' "$work/$2.git" "$work/$1/clone" "$1" "$1" > "$work/$1/config/rehome/settings.yml"
}

as() {
  local name="$1"
  shift
  HOME="$work/$name/home" XDG_CONFIG_HOME="$work/$name/config" XDG_STATE_HOME="$work/$name/state" "$rehome" "$@"
}

origin shared
machine a shared
machine b shared

as a migrate --dry-run > "$work/dry.log"
grep -q 'would: set version: 2' "$work/dry.log"
grep -q '^version: 1' "$work/a/clone/rehome.yml"

as a migrate > "$work/a.log"
grep -q 'migrated to v2 as node a' "$work/a.log"
git -C "$work/shared.git" log --oneline main | grep -q 'Migrate config to v2'

git clone -q "$work/shared.git" "$work/check"
grep -q '^version: 2' "$work/check/rehome.yml"
[[ ! -e "$work/check/rehome.lock" && ! -e "$work/check/files/zsh" ]]
cmp -s "$fixture/files/zsh/.zshrc" "$work/check/nodes/a/files/zsh/.zshrc"
grep -A3 'htop:' "$work/check/nodes/a/node.yml" | grep -q '3.3.0'
grep -q 'unknown:' "$work/check/nodes/a/node.yml"
if grep -qE 'detected:|unknown-hermes|^files:' "$work/check/rehome.yml"; then
  echo "rehome.yml kept machine state after the migration" >&2
  exit 1
fi

# b cloned before a migrated: its pull brings the migration, so it only reuses it.
as b migrate > "$work/b.log"
grep -q 'already up to date' "$work/b.log"
grep -q '^version: 2' "$work/b/clone/rehome.yml"
[[ ! -e "$work/b/clone/nodes/b" ]]

# c and d migrate at once: d's hook runs c's migration between d's commit and d's push.
origin race
machine c race
machine d race
printf '#!/bin/sh\n[ -e %s/c.done ] && exit 0\ntouch %s/c.done\nunset GIT_DIR GIT_INDEX_FILE GIT_WORK_TREE\nHOME=%s XDG_CONFIG_HOME=%s %s migrate > %s 2>&1\n' \
  "$work" "$work" "$work/c/home" "$work/c/config" "$rehome" "$work/c.log" > "$work/d/clone/.git/hooks/post-commit"
chmod +x "$work/d/clone/.git/hooks/post-commit"

as d migrate < /dev/null > "$work/d.log" 2>&1
grep -q 'migrated to v2 as node c' "$work/c.log"
grep -q 'another machine migrated the config first' "$work/d.log"
git -C "$work/d/clone" branch | grep -q 'rehome-migration-'
[[ "$(git -C "$work/d/clone" rev-parse HEAD)" == "$(git -C "$work/race.git" rev-parse main)" ]]
[[ -z "$(git -C "$work/d/clone" status --porcelain)" && ! -d "$work/d/clone/.git/rebase-merge" ]]
[[ -d "$work/d/clone/nodes/c" ]]
