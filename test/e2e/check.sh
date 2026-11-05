#!/bin/bash
# Runs inside a clean container as root: installs the fixture twice as a sudo user and asserts the result.
set -uo pipefail

failures=0

pass() { echo "  ok   $1"; }
fail() { echo "  FAIL $1"; failures=$((failures + 1)); }
check() { if eval "$2"; then pass "$1"; else fail "$1"; fi; }
as_dev() { su - dev -c "$1"; }

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null && apt-get install -y -qq sudo openssh-client dconf-cli dconf-service dbus sqlite3 >/dev/null
useradd -m -s /bin/bash dev
echo "dev ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/dev
cp -r /e2e/fixture /home/dev/cfg && cp /e2e/rehome /home/dev/rehome && chown -R dev /home/dev/cfg /home/dev/rehome
home=/home/dev

echo "== run 1"
as_dev "./rehome install --yes --config cfg" > /tmp/run1.log 2>&1
code1=$?
cat /tmp/run1.log

echo "== assertions"
check "run 1 exits 1 (ghost fails on purpose)" '[ "$code1" -eq 1 ]'
check "apt packages installed" 'command -v htop >/dev/null && command -v zsh >/dev/null'
check "third-party repo with keyring: gh" 'command -v gh >/dev/null'
check "unreachable repo disabled" '[ -f /etc/apt/sources.list.d/broken.list.disabled ]'
check "failed app reported" 'grep -q "✗ ghost" /tmp/run1.log'
check "dependent of failed app skipped" 'grep -q "needs-ghost: requires ghost" /tmp/run1.log && [ ! -e $home/needs-ghost ]'
check "dependent of disabled app installed" '[ -e $home/needs-rustup ]'
check "failed postinst reported and package removed" 'grep -q "✗ postinst-broken.*removed to keep apt working" /tmp/run1.log && ! dpkg -s rehome-e2e-broken >/dev/null 2>&1'
check "dpkg left healthy" '[ -z "$(dpkg -l | awk "NR>5 && \$1 != \"ii\"")" ]'
check "oh-my-zsh installed before its settings" '[ -f $home/.oh-my-zsh/oh-my-zsh.sh ]'
check "theme placed into oh-my-zsh custom" 'cmp -s /e2e/fixture/files/oh-my-zsh/themes/e2e.zsh-theme $home/.oh-my-zsh/custom/themes/e2e.zsh-theme'
check "plugin cloned" '[ -d $home/.oh-my-zsh/custom/plugins/zsh-autosuggestions/.git ]'
check ".zshrc restored" 'grep -q "ZSH_THEME=\"e2e\"" $home/.zshrc'
check "placeholder reported for manual secret" 'grep -q "fill in secrets by hand" /tmp/run1.log'
check "uv tool installed in the same run as uv" '[ -x $home/.local/bin/ruff ]'
check "node installed via nvm lts alias" 'ls $home/.nvm/versions/node/*/bin/node >/dev/null 2>&1'

cowsay_version() { grep -ho '"version": *"[^"]*"' $home/.nvm/versions/node/*/lib/node_modules/cowsay/package.json | grep -o '[0-9][0-9.]*'; }
check "locked version installed (cowsay 1.5.0 from rehome.lock)" '[ "$(cowsay_version)" = "1.5.0" ]'

echo "== run 2"
as_dev "./rehome install --yes --config cfg" > /tmp/run2.log 2>&1
cat /tmp/run2.log
check "run 2 only retries the broken apps" 'grep -q "to install: 3," /tmp/run2.log && grep -q "installed: 0," /tmp/run2.log'

check "run 2 keeps the locked version" '[ "$(cowsay_version)" = "1.5.0" ]'

echo "== upgrade"
sed -i "s/version: 1.5.0/version: 1.6.0/" $home/cfg/rehome.lock
as_dev "./rehome install --yes --config cfg" > /tmp/run3.log 2>&1
check "older than lock is only reported" 'grep -q "cowsay: 1.5.0 → 1.6.0" /tmp/run3.log && [ "$(cowsay_version)" = "1.5.0" ]'
as_dev "./rehome install --yes --upgrade --config cfg" > /tmp/run4.log 2>&1
check "--upgrade raises it to the lock version" '[ "$(cowsay_version)" = "1.6.0" ]'
check "an older lock never downgrades" 'sed -i "s/version: 1.6.0/version: 1.5.0/" $home/cfg/rehome.lock && as_dev "./rehome install --yes --versions locked --config cfg" >/dev/null 2>&1; [ "$(cowsay_version)" = "1.6.0" ]'

echo "== agent interface"
as_dev "./rehome config add-install jq --apt jq --group tools --config cfg" > /tmp/agent.log 2>&1
check "config add-install edits the config" 'grep -q "id: jq" $home/cfg/rehome.yml'
as_dev "./rehome status --json --config cfg" > /tmp/status.json 2>/dev/null
check "status --json lists the new app as missing" 'grep -q "\"clean\": false" /tmp/status.json && grep -A5 "\"missing\"" /tmp/status.json | grep -q "\"jq\""'
check "status --json includes installed and recorded versions" 'grep -A18 '\''"cowsay": {'\'' /tmp/status.json | grep -q '\''"Version": "1.6.0"'\'' && grep -A18 '\''"cowsay": {'\'' /tmp/status.json | grep -q '\''"Version": "1.5.0"'\'''
as_dev "./rehome status --details --config cfg" > /tmp/status-details.log 2>/dev/null
check "status --details compares versions in named columns" 'grep -q "ON MACHINE" /tmp/status-details.log && grep -q "IN CONFIG" /tmp/status-details.log && grep -q "cowsay" /tmp/status-details.log && grep -q "1.6.0" /tmp/status-details.log && grep -q "1.5.0" /tmp/status-details.log'
as_dev "./rehome install --dry-run --json --config cfg" > /tmp/plan.json 2>/dev/null
check "install --dry-run --json returns the command for a person" 'grep -q "\"run_as\"" /tmp/plan.json'

echo "== scan"
as_dev "mkdir -p .config/mc .config/mystery && printf '[Midnight-Commander]\nskin=dark\n' > .config/mc/ini && echo '{}' > .config/mystery/settings.json"
as_dev "mkdir -p scanned && ./rehome scan --yes --config scanned" > /tmp/scan.log 2>&1
cat /tmp/scan.log
check "scan finds apt package" 'grep -q "id: htop" $home/scanned/rehome.yml'
check "scan links gh to its repo" 'grep -q "repo: github-cli" $home/scanned/rehome.yml'
check "scan finds script install" 'grep -q "id: oh-my-zsh" $home/scanned/rehome.yml'
check "scan finds uv tool" 'grep -q "uv_tool: ruff" $home/scanned/rehome.yml'
check "scan records versions in rehome.lock" 'grep -A3 "cowsay:" $home/scanned/rehome.lock | grep -q "version: 1.6.0"'
check "settings of an app outside the catalog found automatically" 'grep -q "origin: auto" $home/scanned/rehome.yml && [ -f $home/scanned/files/mc/.config/mc/ini ]'
check "unclaimed config dir reported with an action" 'grep -q "~/.config/mystery: no app claims it" /tmp/scan.log'
check "scanned config is valid" 'as_dev "./rehome debug config --config scanned" | grep -q " 0 errors"'

echo "== Bitwarden"
if as_dev "bash /e2e/bitwarden.sh /home/dev/rehome"; then
  pass "Bitwarden restore, failure handling and rescan"
else
  fail "Bitwarden restore, failure handling and rescan"
fi

echo "== GNOME dconf"
if as_dev "bash /e2e/dconf.sh /home/dev/rehome"; then
  pass "GNOME settings scan, restore and conflict handling"
else
  fail "GNOME settings scan, restore and conflict handling"
fi

echo "== WSL"
if as_dev "bash /e2e/wsl.sh /home/dev/rehome"; then
  pass "WSL plan, install and scan policy"
else
  fail "WSL plan, install and scan policy"
fi

echo "== harness data"
if as_dev "bash /e2e/harness.sh /home/dev/rehome"; then
  pass "harness opt-in capture, restore and local edits"
else
  fail "harness opt-in capture, restore and local edits"
fi

echo "== sessions"
if as_dev "bash /e2e/sessions.sh /home/dev/rehome"; then
  pass "session opt-in capture, redaction, relocation and repeat import"
else
  fail "session opt-in capture, redaction, relocation and repeat import"
fi

echo "== automatic capture"
if as_dev "bash /e2e/autocapture.sh /home/dev/rehome"; then
  pass "fresh and legacy auto rules exclude runtime/Desktop sessions in scan and status"
else
  fail "fresh and legacy auto rules exclude runtime/Desktop sessions in scan and status"
fi

echo "== $failures failure(s)"
[ "$failures" -eq 0 ]
