#!/bin/bash
set -Eeuo pipefail

rehome="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
trap 'echo "Bitwarden E2E failed at line $LINENO" >&2' ERR

export HOME="$work/home" XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state"
export PATH="$work/bin:$PATH" BW_FIXTURE_DIR="$work"
unset BW_SESSION
mkdir -p "$HOME" "$work/bin" "$work/repo/files/bash"

cat > "$work/bin/bw" <<'SH'
#!/bin/sh
set -eu

test "$BW_SESSION" = "fixture-session"
test "$*" = "get item 12345678-1234-1234-1234-123456789abc --raw --nointeraction"
touch "$BW_FIXTURE_DIR/lookup"

if [ -e "$BW_FIXTURE_DIR/locked" ]; then
  echo "fixture-private-stderr" >&2
  exit 1
fi

printf '%s\n' '{"id":"12345678-1234-1234-1234-123456789abc","fields":[{"name":"api_token","value":"fixture-private-token"}]}'
SH
chmod +x "$work/bin/bw"

cat > "$work/repo/rehome.yml" <<'YAML'
version: 1
apps:
  - id: bash
    files: bash
files:
  bash:
    - src: files/bash/.bashrc
      dest: {linux: ~/.bashrc}
      bitwarden: bw:12345678-1234-1234-1234-123456789abc/api_token
YAML

printf "export FIXTURE_SETTING='<rehome:redacted>'\n" > "$work/template"
cp "$work/template" "$work/repo/files/bash/.bashrc"
target="$HOME/.bashrc"

install_fixture() {
  "$rehome" install --yes --offline --config "$work/repo" "$@"
}

install_fixture > "$work/skipped.log"
grep -q 'use --bitwarden' "$work/skipped.log"
[[ ! -e "$target" && ! -e "$work/lookup" ]]

export BW_SESSION=fixture-session
install_fixture --bitwarden --dry-run > "$work/dry.log"
[[ ! -e "$target" && ! -e "$work/lookup" ]]

install_fixture --bitwarden --json --verbose > "$work/install.json" 2> "$work/install.log"
[[ -e "$work/lookup" ]]
grep -Fxq "export FIXTURE_SETTING='fixture-private-token'" "$target"
[[ "$(stat -c %a "$target")" == 600 ]]

install_fixture --bitwarden --overwrite-files > "$work/repeat.log"
[[ ! -e "$target.rehome.bak" ]]

cp "$target" "$work/restored"
touch "$work/locked"
code=0
install_fixture --bitwarden --overwrite-files > "$work/locked.log" 2>&1 || code=$?
[[ "$code" == 1 ]]
grep -q 'could not read the Bitwarden item' "$work/locked.log"
cmp -s "$work/restored" "$target"
[[ ! -e "$target.rehome.bak" ]]

rm "$work/locked"
echo "local settings" > "$work/edited"
cp "$work/edited" "$target"
install_fixture --bitwarden > "$work/keep.log"
cmp -s "$work/edited" "$target"
[[ ! -e "$target.rehome.bak" ]]

install_fixture --bitwarden --overwrite-files > "$work/overwrite.log"
cmp -s "$work/restored" "$target"
cmp -s "$work/edited" "$target.rehome.bak"
[[ "$(stat -c %a "$target.rehome.bak")" == 600 ]]

unset BW_SESSION
"$rehome" scan --yes --json --config "$work/repo" > "$work/scan.json" 2> "$work/scan.log"
cmp -s "$work/template" "$work/repo/files/bash/.bashrc"
grep -q 'bitwarden: bw:' "$work/repo/rehome.yml"

if grep -rqE 'fixture-private-token|fixture-private-stderr|fixture-session' \
  "$work/repo" "$work/"*.log "$work/"*.json; then
  echo "Bitwarden fixture data leaked into the config or reports" >&2
  exit 1
fi
