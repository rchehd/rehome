#!/bin/bash
set -Eeuo pipefail

rehome="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
trap 'echo "GNOME dconf E2E failed at line $LINENO" >&2' ERR

export HOME="$work/home" XDG_CONFIG_HOME="$work/source" XDG_STATE_HOME="$work/state"
export XDG_CURRENT_DESKTOP=GNOME
unset DBUS_SESSION_BUS_ADDRESS DCONF_PROFILE
mkdir -p "$HOME" "$XDG_CONFIG_HOME/rehome" "$work/scanned" "$work/restore/nodes/source/files/gnome-settings"
printf 'node_id: source\nnode_name: source\n' > "$XDG_CONFIG_HOME/rehome/settings.yml"

dbus-run-session -- bash -eu <<'SH'
dconf write /org/gnome/desktop/interface/color-scheme "'prefer-dark'"
dconf write /org/gnome/shell/enabled-extensions "['test@example.test']"
dconf write /org/gnome/shell/extensions/example/api-token "'fixture-gnome-secret'"
dconf write /org/gnome/online-accounts/example "'fixture-outside-snapshot'"
SH

"$rehome" scan --yes --json --config "$work/scanned" > "$work/scan.json"
snapshot=nodes/source/files/gnome-settings/dconf.ini
grep -q 'id: gnome-settings' "$work/scanned/rehome.yml"
grep -q 'dconf: true' "$work/scanned/nodes/source/node.yml"
grep -Fxq "color-scheme='prefer-dark'" "$work/scanned/$snapshot"
grep -q '<rehome:redacted>' "$work/scanned/$snapshot"

if grep -rqE 'fixture-gnome-secret|fixture-outside-snapshot' "$work/scanned" "$work/scan.json"; then
  echo "Sensitive or out-of-scope dconf data was captured" >&2
  exit 1
fi

cp "$work/scanned/$snapshot" "$work/restore/$snapshot"
cat > "$work/restore/rehome.yml" <<'YAML'
version: 2
apps:
  - id: gnome-settings
    files: gnome-settings
    skip: [macos]
YAML
cat > "$work/restore/nodes/source/node.yml" <<'YAML'
id: source
name: source
platform: debian
file_key: linux
files:
  gnome-settings:
    - src: files/gnome-settings/dconf.ini
      dconf: true
YAML

export XDG_CONFIG_HOME="$work/target"
mkdir -p "$XDG_CONFIG_HOME"
dbus-run-session -- bash -eu <<'SH'
dconf write /org/gnome/desktop/interface/color-scheme "'default'"
dconf write /org/gnome/desktop/interface/cursor-size 41
SH

install_fixture() {
  "$rehome" install --all --yes --offline --config "$work/restore" "$@"
}

mkdir "$work/fresh"
XDG_CONFIG_HOME="$work/fresh" install_fixture > "$work/fresh.log"
[[ "$(XDG_CONFIG_HOME="$work/fresh" dconf read /org/gnome/desktop/interface/color-scheme)" == "'prefer-dark'" ]]

install_fixture > "$work/kept.log"
grep -q 'settings kept as they are' "$work/kept.log"
[[ "$(dconf read /org/gnome/desktop/interface/color-scheme)" == "'default'" ]]
[[ -z "$(dconf read /org/gnome/shell/enabled-extensions)" ]]

install_fixture --overwrite-files --dry-run > "$work/dry.log"
[[ "$(dconf read /org/gnome/desktop/interface/color-scheme)" == "'default'" ]]

install_fixture --overwrite-files > "$work/restore.log"
[[ "$(dconf read /org/gnome/desktop/interface/color-scheme)" == "'prefer-dark'" ]]
[[ "$(dconf read /org/gnome/desktop/interface/cursor-size)" == 41 ]]
[[ "$(dconf read /org/gnome/shell/enabled-extensions)" == "['test@example.test']" ]]
[[ -z "$(dconf read /org/gnome/shell/extensions/example/api-token)" ]]
[[ -z "$(dconf read /org/gnome/online-accounts/example)" ]]

backups=("$XDG_STATE_HOME"/rehome/gnome-dconf-*.ini)
[[ "${#backups[@]}" == 1 && "$(stat -c %a "${backups[0]}")" == 600 ]]
grep -Fxq "color-scheme='default'" "${backups[0]}"

install_fixture --overwrite-files > "$work/repeat.log"
backups=("$XDG_STATE_HOME"/rehome/gnome-dconf-*.ini)
[[ "${#backups[@]}" == 1 ]]
