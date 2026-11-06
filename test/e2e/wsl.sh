#!/bin/bash
set -Eeuo pipefail

rehome="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
trap 'echo "WSL E2E failed at line $LINENO" >&2; tail -n 25 "$work"/*.log >&2 2>/dev/null' ERR

export HOME="$work/home" XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state"
export WSL_DISTRO_NAME=RehomeTest XDG_CURRENT_DESKTOP=GNOME
node="$work/repo/nodes/e2e"
mkdir -p "$HOME" "$node/files" "$XDG_CONFIG_HOME/rehome"
printf 'node_id: e2e\nnode_name: e2e\n' > "$XDG_CONFIG_HOME/rehome/settings.yml"

cat > "$work/repo/rehome.yml" <<'YAML'
version: 2
apps:
  - id: bash
    files: bash
  - id: fonts
    files: fonts
  - id: custom-gui
    group: desktop
    install: {any: {script: 'touch ~/.gui-installed', bin: ~/.gui-installed}}
  - id: snap-tool
    install: {debian: {snap: snap-tool}}
  - id: flatpak-tool
    install: {debian: {flatpak: org.example.Tool}}
  - id: gnome-settings
    files: gnome-settings
YAML

cat > "$node/node.yml" <<'YAML'
id: e2e
name: e2e
platform: debian
file_key: linux
files:
  bash:
    - {src: files/bashrc, dest: {linux: ~/.bashrc}}
  fonts:
    - {src: files/font, dest: {linux: ~/.local/share/fonts/e2e-font}}
  gnome-settings:
    - {src: files/gnome.ini, dconf: true}
YAML

printf 'export REHOME_WSL_TEST=1\n' > "$node/files/bashrc"
printf 'test font\n' > "$node/files/font"
printf '[desktop/interface]\ncolor-scheme=\x27prefer-dark\x27\n' > "$node/files/gnome.ini"

"$rehome" install --all --config "$work/repo" --offline --dry-run > "$work/plan.log"
grep -Eq 'fonts +install this on Windows' "$work/plan.log"
grep -Eq 'custom-gui +install this on Windows' "$work/plan.log"
grep -Eq 'snap-tool +Snap and Flatpak are unavailable in WSL without systemd' "$work/plan.log"
grep -Eq 'flatpak-tool +Snap and Flatpak are unavailable in WSL without systemd' "$work/plan.log"
grep -Eq 'gnome-settings +GNOME desktop settings are unavailable in WSL' "$work/plan.log"

"$rehome" install --all --config "$work/repo" --offline --yes > "$work/install.log"
cmp -s "$node/files/bashrc" "$HOME/.bashrc"
[[ ! -e "$HOME/.gui-installed" && ! -e "$HOME/.local/share/fonts/e2e-font" ]]
[[ ! -e "$XDG_CONFIG_HOME/dconf/user" ]]

"$rehome" scan --yes --config "$work/repo" > "$work/scan.log"
# Recipes are shared: what WSL skips stays for the other machines.
for id in fonts custom-gui snap-tool flatpak-tool gnome-settings; do
  grep -q "id: $id" "$work/repo/rehome.yml"
done
