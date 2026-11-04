#!/bin/bash
set -Eeuo pipefail

rehome="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
trap 'echo "WSL E2E failed at line $LINENO" >&2' ERR

export HOME="$work/home" XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state"
export WSL_DISTRO_NAME=RehomeTest XDG_CURRENT_DESKTOP=GNOME
mkdir -p "$HOME" "$work/repo/files"

cat > "$work/repo/rehome.yml" <<'YAML'
version: 1
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
files:
  bash:
    - {src: files/bashrc, dest: {linux: ~/.bashrc}}
  fonts:
    - {src: files/font, dest: {linux: ~/.local/share/fonts/e2e-font}}
  gnome-settings:
    - {src: files/gnome.ini, dconf: true}
YAML

printf 'export REHOME_WSL_TEST=1\n' > "$work/repo/files/bashrc"
printf 'test font\n' > "$work/repo/files/font"
printf '[desktop/interface]\ncolor-scheme=\x27prefer-dark\x27\n' > "$work/repo/files/gnome.ini"

"$rehome" install --config "$work/repo" --offline --dry-run > "$work/plan.log"
grep -Eq 'fonts +install this on Windows' "$work/plan.log"
grep -Eq 'custom-gui +install this on Windows' "$work/plan.log"
grep -Eq 'snap-tool +Snap and Flatpak are unavailable in WSL without systemd' "$work/plan.log"
grep -Eq 'flatpak-tool +Snap and Flatpak are unavailable in WSL without systemd' "$work/plan.log"
grep -Eq 'gnome-settings +GNOME desktop settings are unavailable in WSL' "$work/plan.log"

"$rehome" install --config "$work/repo" --offline --yes > "$work/install.log"
cmp -s "$work/repo/files/bashrc" "$HOME/.bashrc"
[[ ! -e "$HOME/.gui-installed" && ! -e "$HOME/.local/share/fonts/e2e-font" ]]
[[ ! -e "$XDG_CONFIG_HOME/dconf/user" ]]

"$rehome" scan --yes --config "$work/repo" > "$work/scan.log"
if grep -q 'missing_on_scan' "$work/repo/rehome.yml"; then
  echo "WSL scan marked an intentionally skipped app as missing" >&2
  exit 1
fi
