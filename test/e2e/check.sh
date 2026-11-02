#!/bin/bash
# Runs inside a clean container as root: installs the fixture twice as a sudo user and asserts the result.
set -uo pipefail

failures=0

pass() { echo "  ok   $1"; }
fail() { echo "  FAIL $1"; failures=$((failures + 1)); }
check() { if eval "$2"; then pass "$1"; else fail "$1"; fi; }
as_dev() { su - dev -c "$1"; }

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null && apt-get install -y -qq sudo >/dev/null
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

echo "== run 2"
as_dev "./rehome install --yes --config cfg" > /tmp/run2.log 2>&1
cat /tmp/run2.log
check "run 2 only retries the broken apps" 'grep -q "to install: 3," /tmp/run2.log && grep -q "installed: 0," /tmp/run2.log'

echo "== scan"
as_dev "mkdir -p scanned && ./rehome scan --yes --config scanned" > /tmp/scan.log 2>&1
cat /tmp/scan.log
check "scan finds apt package" 'grep -q "id: htop" $home/scanned/rehome.yml'
check "scan links gh to its repo" 'grep -q "repo: github-cli" $home/scanned/rehome.yml'
check "scan finds script install" 'grep -q "id: oh-my-zsh" $home/scanned/rehome.yml'
check "scan finds uv tool" 'grep -q "uv_tool: ruff" $home/scanned/rehome.yml'
check "scanned config is valid" 'as_dev "./rehome debug config --config scanned" | grep -q " 0 errors"'

echo "== $failures failure(s)"
[ "$failures" -eq 0 ]
