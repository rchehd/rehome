#!/bin/bash
# Two machines share one repo: b installs what a has with a's settings, then removes an app everywhere.
# Runs after check.sh, which installed mc on this system; scan finds it and captures ~/.config/mc.
set -Eeuo pipefail

rehome="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
trap 'echo "Nodes E2E failed at line $LINENO" >&2; tail -n 25 "$work"/*.log >&2 2>/dev/null' ERR

export GIT_AUTHOR_NAME=e2e GIT_AUTHOR_EMAIL=e2e@example.test GIT_COMMITTER_NAME=e2e GIT_COMMITTER_EMAIL=e2e@example.test
unset XDG_CURRENT_DESKTOP DBUS_SESSION_BUS_ADDRESS WSL_DISTRO_NAME WSL_INTEROP

mkdir -p "$work/seed"
cat > "$work/seed/rehome.yml" <<'YAML'
version: 2
apps:
  - id: tool-y
    install: { any: { script: touch ~/.tool-y, bin: ~/.tool-y } }
YAML
git -C "$work/seed" init -q -b main
git -C "$work/seed" add -A
git -C "$work/seed" commit -qm recipes
git clone -q --bare "$work/seed" "$work/origin.git"

for name in a b; do
  mkdir -p "$work/$name/home" "$work/$name/config/rehome"
  git clone -q "$work/origin.git" "$work/$name/clone"
  printf 'repo_url: %s\nclone_path: %s\nnode_id: %s\nnode_name: %s\n' "$work/origin.git" "$work/$name/clone" "$name" "$name" > "$work/$name/config/rehome/settings.yml"
done

as() {
  local name="$1"
  shift
  HOME="$work/$name/home" XDG_CONFIG_HOME="$work/$name/config" XDG_STATE_HOME="$work/$name/state" "$rehome" "$@"
}

mkdir -p "$work/a/home/.config/mc"
printf '[Midnight-Commander]\nskin=node-a\n' > "$work/a/home/.config/mc/ini"
as a scan --yes > "$work/a-scan.log"
# Scan records what it detects; a script install is recorded by installing it through rehome.
as a install tool-y --yes > "$work/a-install.log"
as a push > "$work/a-push.log"
git -C "$work/origin.git" show main:nodes/a/node.yml > "$work/a-node.yml"
grep -q 'tool-y' "$work/a-node.yml"
grep -q 'mc/ini' "$work/a-node.yml"

as b pull > /dev/null
as b nodes --json > "$work/nodes.json"
grep -q '"name": "a"' "$work/nodes.json"

as b install --dry-run --json --from a > "$work/plan.json"
grep -q '"from": "a"' "$work/plan.json"

as b install --from a --yes --version latest --push > "$work/b-install.log"
[[ -e "$work/b/home/.tool-y" ]]
cmp -s "$work/a/home/.config/mc/ini" "$work/b/home/.config/mc/ini"
git -C "$work/origin.git" show main:nodes/b/node.yml | grep -q 'tool-y'

as b remove tool-y --everywhere --yes > "$work/b-remove.log"
grep -q 'removed tool-y from this node and marked it removed everywhere' "$work/b-remove.log"
as b push > /dev/null
git -C "$work/origin.git" show main:rehome.yml | grep -A2 'removed:' | grep -q 'id: tool-y'
if git -C "$work/origin.git" show main:nodes/b/node.yml | grep -q 'tool-y'; then
  echo "remove kept tool-y in node b" >&2
  exit 1
fi

as a sync --yes --json > "$work/a-sync.json"
sed -n '/"removed_elsewhere"/,/]/p' "$work/a-sync.json" | grep -q '"tool-y"'
[[ -e "$work/a/home/.tool-y" ]]

as a install --all --dry-run --json > "$work/a-plan.json"
sed -n '/"skipped"/,/]/p' "$work/a-plan.json" | grep -q 'removed everywhere by b'

as a status --json > "$work/a-status.json" || true
grep -q '"unpublished": false' "$work/a-status.json"
