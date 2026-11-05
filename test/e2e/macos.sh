#!/bin/bash
# End-to-end test on a macOS CI runner: a config written on Ubuntu installs through auto-mapping.
set -uo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
failures=0

pass() { echo "  ok   $1"; }
fail() { echo "  FAIL $1"; failures=$((failures + 1)); }
check() { if eval "$2"; then pass "$1"; else fail "$1"; fi; }

(cd "$root" && go build -o "$work/rehome" ./cmd/rehome) || exit 1
mkdir -p "$work/cfg" "$work/scanned"
export XDG_STATE_HOME="$work/state"
cat > "$work/cfg/rehome.yml" <<'YAML'
version: 1
apps:
  - id: htop # same name in Homebrew
    install: { debian: { apt: htop } }
  - id: fd # fd-find on Debian, fd in Homebrew
    install: { debian: { apt: fd-find } }
  - id: tree
    install: { debian: { apt: tree } }
  - id: rectangle # macOS only
    install: { macos: { cask: rectangle } }
  - id: terminator # the catalog says it does not exist on macOS
    install: { debian: { apt: terminator } }
YAML
rehome() { "$work/rehome" "$@"; }

echo "== plan"
rehome install --dry-run --json --config "$work/cfg" > "$work/plan.json" 2>"$work/plan.err"
cat "$work/plan.json"
check "debian-only apps are auto-mapped" 'grep -c "\"auto_mapped\"" "$work/plan.json" | grep -q "^3$"'
check "fd-find maps to the fd formula" 'grep -A3 "\"id\": \"fd\"" "$work/plan.json" | grep -q "\"package\": \"fd\""'
check "terminator is reported as not available" 'grep -A6 "\"id\": \"terminator\"" "$work/plan.json" | grep -q "add install.macos"'

echo "== run 1"
rehome install --yes --config "$work/cfg" > "$work/run1.log" 2>&1
cat "$work/run1.log"
check "formulae installed" 'brew list --versions htop fd tree >/dev/null'
check "cask installed" 'brew list --cask --versions rectangle >/dev/null'

echo "== run 2"
rehome install --yes --config "$work/cfg" > "$work/run2.log" 2>&1
cat "$work/run2.log"
check "rerun installs nothing" 'grep -q "to install: 0," "$work/run2.log"'

echo "== scan"
rehome scan --yes --config "$work/scanned" > "$work/scan.log" 2>&1
check "scan records macOS installers" 'grep -q "brew: htop" "$work/scanned/rehome.yml" && grep -q "cask: rectangle" "$work/scanned/rehome.yml"'
rehome debug config --config "$work/scanned" > "$work/debug.log" 2>&1
check "scanned config is valid" 'grep -q " 0 errors" "$work/debug.log" || { cat "$work/debug.log"; false; }'

echo "== harness data"
check "harness opt-in capture, restore and local edits" 'bash "$root/test/e2e/harness.sh" "$work/rehome"'

echo "== sessions"
check "session opt-in capture, redaction, relocation and repeat import" 'bash "$root/test/e2e/sessions.sh" "$work/rehome"'

echo "== $failures failure(s)"
[ "$failures" -eq 0 ]
