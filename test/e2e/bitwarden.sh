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
ssh-keygen -q -t ed25519 -N '' -f "$work/vault-key"
ssh-keygen -q -t ed25519 -N '' -f "$work/local-key"

cat > "$work/bin/bw" <<'SH'
#!/bin/sh
set -eu

test "$BW_SESSION" = "fixture-session"
test "$1 $2 $4 $5" = "get item --raw --nointeraction"
touch "$BW_FIXTURE_DIR/lookup"

if [ -e "$BW_FIXTURE_DIR/locked" ]; then
  echo "fixture-private-stderr" >&2
  exit 1
fi

case "$3" in
  12345678-1234-1234-1234-123456789abc)
    printf '%s\n' '{"id":"12345678-1234-1234-1234-123456789abc","fields":[{"name":"api_token","value":"fixture-private-token"}]}'
    ;;
  12345678-1234-1234-1234-123456789def)
    key="$(awk '{printf "%s\\n", $0}' "$BW_FIXTURE_DIR/vault-key")"
    if [ -e "$BW_FIXTURE_DIR/malformed" ]; then
      key="fixture-malformed-key"
    fi
    printf '{"id":"%s","type":5,"sshKey":{"privateKey":"%s"}}\n' "$3" "$key"
    ;;
  *) exit 1 ;;
esac
SH
chmod +x "$work/bin/bw"

cat > "$work/repo/rehome.yml" <<'YAML'
version: 1
apps:
  - id: bash
    files: bash
  - id: ssh
    files: ssh
files:
  bash:
    - src: files/bash/.bashrc
      dest: {linux: ~/.bashrc}
      bitwarden: bw:12345678-1234-1234-1234-123456789abc/api_token
  ssh:
    - dest: {linux: ~/.ssh/id_work}
      bitwarden: bw:12345678-1234-1234-1234-123456789def/sshKey/privateKey
YAML

printf "export FIXTURE_SETTING='<rehome:redacted>'\n" > "$work/template"
cp "$work/template" "$work/repo/files/bash/.bashrc"
target="$HOME/.bashrc"
key_target="$HOME/.ssh/id_work"

install_fixture() {
  "$rehome" install --yes --offline --config "$work/repo" "$@"
}

install_fixture > "$work/skipped.log"
grep -q 'use --bitwarden' "$work/skipped.log"
[[ ! -e "$target" && ! -e "$key_target" && ! -e "$work/lookup" ]]

export BW_SESSION=fixture-session
install_fixture --bitwarden --dry-run > "$work/dry.log"
[[ ! -e "$target" && ! -e "$key_target" && ! -e "$work/lookup" ]]

install_fixture --bitwarden --json --verbose > "$work/install.json" 2> "$work/install.log"
[[ -e "$work/lookup" ]]
grep -Fxq "export FIXTURE_SETTING='fixture-private-token'" "$target"
[[ "$(stat -c %a "$target")" == 600 ]]
cmp -s "$work/vault-key" "$key_target"
[[ "$(stat -c %a "$key_target")" == 600 && "$(stat -c %a "$HOME/.ssh")" == 700 ]]

install_fixture --bitwarden --overwrite-files > "$work/repeat.log"
[[ ! -e "$target.rehome.bak" && ! -e "$key_target.rehome.bak" ]]

cp "$target" "$work/restored"
touch "$work/locked"
code=0
install_fixture --bitwarden --overwrite-files > "$work/locked.log" 2>&1 || code=$?
[[ "$code" == 1 ]]
grep -q 'could not read the Bitwarden item' "$work/locked.log"
cmp -s "$work/restored" "$target"
cmp -s "$work/vault-key" "$key_target"
[[ ! -e "$target.rehome.bak" && ! -e "$key_target.rehome.bak" ]]

rm "$work/locked"
touch "$work/malformed"
code=0
install_fixture --bitwarden --overwrite-files > "$work/malformed.log" 2>&1 || code=$?
[[ "$code" == 1 ]]
grep -q 'does not contain a supported private key' "$work/malformed.log"
cmp -s "$work/vault-key" "$key_target"
[[ ! -e "$key_target.rehome.bak" ]]
rm "$work/malformed"

echo "local settings" > "$work/edited"
cp "$work/edited" "$target"
cp "$work/local-key" "$key_target"
install_fixture --bitwarden > "$work/keep.log"
cmp -s "$work/edited" "$target"
cmp -s "$work/local-key" "$key_target"
[[ ! -e "$target.rehome.bak" && ! -e "$key_target.rehome.bak" ]]

install_fixture --bitwarden --overwrite-files > "$work/overwrite.log"
cmp -s "$work/restored" "$target"
cmp -s "$work/edited" "$target.rehome.bak"
[[ "$(stat -c %a "$target.rehome.bak")" == 600 ]]
cmp -s "$work/vault-key" "$key_target"
cmp -s "$work/local-key" "$key_target.rehome.bak"
[[ "$(stat -c %a "$key_target.rehome.bak")" == 600 ]]

mv "$HOME/.ssh" "$work/saved-ssh"
mkdir "$work/outside"
ln -s "$work/outside" "$HOME/.ssh"
code=0
install_fixture --bitwarden --overwrite-files > "$work/symlink.log" 2>&1 || code=$?
[[ "$code" == 1 && ! -e "$work/outside/id_work" ]]
grep -q 'require a real ~/.ssh directory' "$work/symlink.log"
rm "$HOME/.ssh"
mv "$work/saved-ssh" "$HOME/.ssh"

printf 'Host work\n  HostName example.test\n' > "$HOME/.ssh/config"
printf '%s\n' '# fixture known hosts' > "$HOME/.ssh/known_hosts"

unset BW_SESSION
"$rehome" scan --yes --json --config "$work/repo" > "$work/scan.json" 2> "$work/scan.log"
cmp -s "$work/template" "$work/repo/files/bash/.bashrc"
grep -q 'bitwarden: bw:' "$work/repo/rehome.yml"
grep -q '/sshKey/privateKey' "$work/repo/rehome.yml"
cmp -s "$HOME/.ssh/config" "$work/repo/files/ssh/config"
cmp -s "$HOME/.ssh/known_hosts" "$work/repo/files/ssh/known_hosts"
[[ ! -e "$work/repo/files/ssh/id_work" && ! -e "$work/repo/files/ssh/id_work.rehome.bak" ]]

if grep -rqE 'fixture-private-token|fixture-private-stderr|fixture-session|fixture-malformed-key|BEGIN OPENSSH PRIVATE KEY' \
  "$work/repo" "$work/"*.log "$work/"*.json; then
  echo "Bitwarden fixture data leaked into the config or reports" >&2
  exit 1
fi
