#!/bin/bash
set -Eeuo pipefail

rehome="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
trap 'echo "Autocapture E2E failed at line $LINENO" >&2; tail -n 25 "$work"/*.log >&2 2>/dev/null' ERR

export HOME="$work/home" XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state"
unset CODEX_HOME CLAUDE_CONFIG_DIR XDG_CURRENT_DESKTOP DBUS_SESSION_BUS_ADDRESS WSL_DISTRO_NAME WSL_INTEROP
mkdir -p "$HOME/.local/bin" "$work/bin" "$work/fresh" "$work/legacy" "$XDG_CONFIG_HOME/rehome"
printf 'node_id: e2e\nnode_name: e2e\n' > "$XDG_CONFIG_HOME/rehome/settings.yml"
if command -v dpkg-query >/dev/null; then
  ln -s "$(command -v dpkg-query)" "$work/bin/dpkg-query"
fi
printf '#!/bin/sh\nexit 0\n' > "$HOME/.local/bin/claude-desktop"
chmod 755 "$HOME/.local/bin/claude-desktop"

for rel in settings.json Preferences Crashpad/client_id VideoDecodeStats/LOCK VideoDecodeStats/LOG \
  claude-code/2.1.308/hash/.payload claude-code/2.1.308/hash/.verified \
  claude-code-sessions/account/session/local_123.json buddy-tokens.json credentials/cakey.pem; do
  mkdir -p "$(dirname "$HOME/.config/Claude/$rel")"
  printf '{"fixture":true}\n' > "$HOME/.config/Claude/$rel"
done

cat > "$work/legacy/rehome.yml" <<'YAML'
version: 2
apps: []
capture:
  unknown-claude-desktop:
    linux:
      base: "~"
      include: [".config/Claude/**"]
      exclude: ["**/cache/**"]
      origin: auto
      text_only: true
YAML

for config in fresh legacy; do
  for option in "" --sessions; do
    PATH="$work/bin" "$rehome" scan $option --yes --json --config "$work/$config" > "$work/scan.json"
    captured="$work/$config/nodes/e2e/files/unknown-claude-desktop/.config/Claude"
    [[ -f "$captured/settings.json" && -f "$captured/Preferences" ]]
    [[ "$(find "$captured" -type f | wc -l)" -eq 2 ]]
    grep -q 'Desktop session format is unsupported' "$work/scan.json"

    printf '{"theme":"changed-%s%s"}\n' "$config" "$option" > "$HOME/.config/Claude/settings.json"
    if PATH="$work/bin" "$rehome" status $option --json --config "$work/$config" > "$work/status.json"; then
      echo "Status missed the changed setting" >&2
      exit 1
    else
      [[ "$?" -eq 1 ]]
    fi
    grep -q 'Desktop session format is unsupported' "$work/status.json"
    for view in "" --details; do
      if PATH="$work/bin" "$rehome" status $view --config "$work/$config" > "$work/status-text.log"; then
        echo "Text status missed the changed setting" >&2
        exit 1
      else
        [[ "$?" -eq 1 ]]
      fi
      if [[ "$view" == --details ]]; then
        grep -q 'files/unknown-claude-desktop/.config/Claude/settings.json' "$work/status-text.log"
      else
        grep -Eq 'Changed files +1 files' "$work/status-text.log"
        grep -q 'status --interactive' "$work/status-text.log"
        [[ "$(wc -l < "$work/status-text.log")" -le 30 ]]
      fi
    done
    sed -n '/"changed_files": \[/,/]/p' "$work/status.json" > "$work/changed-files.json"
    grep -q 'settings.json' "$work/changed-files.json"
    if grep -Eq 'local_123|Crashpad|VideoDecodeStats|payload|verified|buddy-tokens|cakey' "$work/changed-files.json"; then
      echo "Runtime/session data appeared in status changes" >&2
      exit 1
    fi
  done
done
