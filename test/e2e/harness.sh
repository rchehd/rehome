#!/bin/bash
set -Eeuo pipefail

rehome="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
trap 'echo "Harness E2E failed at line $LINENO" >&2; tail -n 25 "$work"/*.log >&2 2>/dev/null' ERR

export HOME="$work/source" XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state"
unset XDG_CURRENT_DESKTOP DBUS_SESSION_BUS_ADDRESS WSL_DISTRO_NAME WSL_INTEROP
mkdir -p "$HOME/.claude/agents" "$HOME/.codex/agents" "$HOME/.cursor/agents" \
  "$HOME/.local/share/claude/versions/fixture" "$HOME/.codex/packages/standalone/releases/fixture" \
  "$HOME/.agents/skills/check/scripts" "$work/bin" "$work/scanned" "$work/restore/nodes/source"
# rehome reads settings from os.UserConfigDir: XDG on Linux, ~/Library/Application Support on macOS.
settings="$XDG_CONFIG_HOME/rehome"
[[ "$(uname)" == Darwin ]] && settings="$HOME/Library/Application Support/rehome"
mkdir -p "$settings"
printf 'node_id: source\nnode_name: source\n' > "$settings/settings.yml"
captured="$work/scanned/nodes/source/files"

if command -v dpkg-query >/dev/null; then
  ln -s "$(command -v dpkg-query)" "$work/bin/dpkg-query"
fi

for harness in .claude .codex .cursor; do
  mkdir -p "$HOME/$harness/skills/check"
  printf '# Example skill\n' > "$HOME/$harness/skills/check/SKILL.md"
  printf '# Example subagent\n' > "$HOME/$harness/agents/review.md"
done
printf '# Shared skill\n' > "$HOME/.agents/skills/check/SKILL.md"
printf '#!/bin/sh\nexit 0\n' > "$HOME/.agents/skills/check/scripts/check.sh"
chmod 755 "$HOME/.agents/skills/check/scripts/check.sh"
printf 'sk-ant-abcdefghijklmnopqrstuvwxyz\n' > "$HOME/.cursor/agents/secret.md"

PATH="$work/bin" "$rehome" scan --yes --json --config "$work/scanned" > "$work/default.json"
[[ ! -e "$captured/claude-code/skills/check/SKILL.md" ]]
[[ ! -e "$captured/codex/agents/review.md" ]]
[[ ! -e "$captured/cursor-agent-data/agents/review.md" ]]
[[ ! -e "$captured/shared-agent-skills/skills/check/SKILL.md" ]]

PATH="$work/bin" "$rehome" scan --yes --json --harness-data --config "$work/scanned" > "$work/scan.json"
for id in claude-code codex cursor-agent-data shared-agent-skills; do
  [[ -f "$captured/$id/skills/check/SKILL.md" ]]
done
grep -q '<rehome:redacted>' "$captured/cursor-agent-data/agents/secret.md"

cp -R "$captured" "$work/restore/nodes/source/"
cat > "$work/restore/rehome.yml" <<'YAML'
version: 2
apps:
  - {id: cursor-agent-data, files: cursor-agent-data}
  - {id: shared-agent-skills, files: shared-agent-skills}
YAML
cat > "$work/restore/nodes/source/node.yml" <<YAML
id: source
name: source
last_seen: $(date -u +%Y-%m-%dT%H:%M:%SZ)
files:
  cursor-agent-data:
    - {src: files/cursor-agent-data/agents/review.md, dest: {linux: ~/.cursor/agents/review.md, macos: ~/.cursor/agents/review.md}}
  shared-agent-skills:
    - {src: files/shared-agent-skills/skills/check/SKILL.md, dest: {linux: ~/.agents/skills/check/SKILL.md, macos: ~/.agents/skills/check/SKILL.md}}
    - {src: files/shared-agent-skills/skills/check/scripts/check.sh, mode: "0755", dest: {linux: ~/.agents/skills/check/scripts/check.sh, macos: ~/.agents/skills/check/scripts/check.sh}}
YAML

export HOME="$work/target" XDG_CONFIG_HOME="$work/target-config" XDG_STATE_HOME="$work/target-state"
mkdir -p "$HOME"
PATH="$work/bin" "$rehome" sync --yes --offline --config "$work/restore" > "$work/default-restore.log"
[[ ! -e "$HOME/.cursor/agents/review.md" && ! -e "$HOME/.agents/skills/check/SKILL.md" ]]

PATH="$work/bin" "$rehome" sync --yes --offline --harness-data --config "$work/restore" > "$work/restore.log"
cmp -s "$work/source/.cursor/agents/review.md" "$HOME/.cursor/agents/review.md"
cmp -s "$work/source/.agents/skills/check/SKILL.md" "$HOME/.agents/skills/check/SKILL.md"
[[ -x "$HOME/.agents/skills/check/scripts/check.sh" ]]

printf 'local edit\n' > "$HOME/.cursor/agents/review.md"
PATH="$work/bin" "$rehome" install --all --yes --offline --harness-data --config "$work/restore" > "$work/repeat.log"
grep -q 'local edit' "$HOME/.cursor/agents/review.md"
