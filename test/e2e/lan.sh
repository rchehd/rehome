#!/bin/bash
# Homes on one host sync over the LAN: pair with a code, take a signed node, refuse an unpaired one.
set -Eeuo pipefail
# Checks end in || false: bash 3.2 on macOS ignores set -e for a failing [[ ]].

rehome="$1"
work="$(mktemp -d)"
pids=()
trap 'kill "${pids[@]}" 2>/dev/null || true; rm -rf "$work"' EXIT
trap 'echo "LAN E2E failed at line $LINENO" >&2; tail -n 25 "$work"/*.log >&2 2>/dev/null' ERR

unset XDG_CURRENT_DESKTOP DBUS_SESSION_BUS_ADDRESS WSL_DISTRO_NAME WSL_INTEROP

# home NAME PORT: a home with its own settings, node id, LAN port and config folder.
home() {
  mkdir -p "$work/$1/home" "$work/$1/config/rehome" "$work/$1/data"
  printf 'node_id: %s\nnode_name: %s\nclone_path: %s\nlan:\n  port: %s\n' "$(printf "$1%.0s" {1..32})" "$1" "$work/$1/clone" "$2" > "$work/$1/config/rehome/settings.yml"
  mkdir -p "$work/$1/clone"
  printf 'version: 2\napps: []\n' > "$work/$1/clone/rehome.yml"
}

as() {
  local name="$1"
  shift
  HOME="$work/$name/home" XDG_CONFIG_HOME="$work/$name/config" XDG_DATA_HOME="$work/$name/data" XDG_STATE_HOME="$work/$name/state" "$rehome" "$@"
}

home a 17474
home b 17475
home c 17476

# a shares with pairing open; the code is read from its log.
as a serve --pair --for 2m > "$work/a-serve.log" 2>&1 &
pids+=($!)

for _ in $(seq 50); do
  grep -q 'sharing as a' "$work/a-serve.log" && break
  sleep 0.2
done

code="$(grep -A1 'Pairing code' "$work/a-serve.log" | grep -oE '[0-9]{6}' | head -1)"
[[ -n "$code" ]] || false
as b link lan 127.0.0.1:17474 --code "$code" > "$work/b-link.log" 2>&1
grep -q 'paired with a' "$work/b-link.log"
grep -q "id: $(printf 'b%.0s' {1..32})" "$work/a/config/rehome/settings.yml"

# c never paired, and pairing closed with b's pairing: c is refused and a does not trust it.
if as c link lan 127.0.0.1:17474 --code 000000 > "$work/c-link.log" 2>&1; then
  echo "an unpaired machine paired after the window closed" >&2
  exit 1
fi

if grep -q 'id: c' "$work/a/config/rehome/settings.yml"; then
  echo "a trusts c" >&2
  exit 1
fi

as a scan --yes --config "$work/a/clone" > "$work/a-scan.log" 2>&1
[[ -f "$work/a/clone/nodes/a/manifest.sig" ]] || false

as b sync --yes --no-apply --no-push > "$work/b-sync.log" 2>&1
[[ -f "$work/b/clone/nodes/a/manifest.sig" ]] || false
first="$(grep '^revision:' "$work/b/clone/nodes/a/node.yml")"
kill "${pids[@]}" 2>/dev/null || true
wait 2>/dev/null || true
sed -i 's/^lan:$/lan:\n  peers: [127.0.0.1:17475]/' "$work/a/config/rehome/settings.yml"

# a renames itself, which saves its node with a higher revision; then both sync at once and each takes the other.
as a nodes rename a a2 --config "$work/a/clone" > "$work/a-rename.log" 2>&1
as b scan --yes --config "$work/b/clone" > "$work/b-scan.log" 2>&1
as a sync --yes --no-apply --no-push > "$work/a-sync.log" 2>&1 &
apid=$!
as b sync --yes --no-apply --no-push > "$work/b-sync2.log" 2>&1 &
bpid=$!
wait "$apid"
wait "$bpid"

second="$(grep '^revision:' "$work/b/clone/nodes/a2/node.yml")"
[[ "$first" != "$second" ]] || false
[[ ! -e "$work/b/clone/nodes/a" ]] || false
[[ -f "$work/a/clone/nodes/b/manifest.sig" ]] || false

