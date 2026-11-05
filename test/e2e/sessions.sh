#!/bin/bash
set -Eeuo pipefail

rehome="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
trap 'echo "Session E2E failed at line $LINENO" >&2' ERR

export HOME="$work/source" XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state"
export CURSOR_CONFIG_DIR="$HOME/.cursor"
unset CODEX_HOME CLAUDE_CONFIG_DIR XDG_CURRENT_DESKTOP DBUS_SESSION_BUS_ADDRESS WSL_DISTRO_NAME WSL_INTEROP
id=11111111-1111-4111-8111-111111111111
workspace=4f2a9deaa47bd4c1d6e8f804c161a77a
cursor="$HOME/.cursor/chats/$workspace/$id"
mkdir -p "$HOME/.claude/projects/source-project" "$HOME/.codex/sessions/2026/11/05" "$cursor" "$work/bin" "$work/scanned"
if command -v dpkg-query >/dev/null; then
  ln -s "$(command -v dpkg-query)" "$work/bin/dpkg-query"
fi

cat > "$HOME/.claude/projects/source-project/$id.jsonl" <<JSON
{"type":"user","cwd":"/rehome-session-project","sessionId":"$id","message":{"role":"user","content":"Remember fixture 83147 sk-ant-abcdefghijklmnopqrstuvwxyz"}}
JSON
cat > "$HOME/.codex/sessions/2026/11/05/rollout-2026-11-05T00-00-00-$id.jsonl" <<JSON
{"timestamp":"2026-11-05T00:00:00Z","type":"session_meta","payload":{"id":"$id","cwd":"/rehome-session-project","source":"cli","model_provider":"openai"}}
{"timestamp":"2026-11-05T00:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"Remember fixture 83147 sk-ant-abcdefghijklmnopqrstuvwxyz"}}
JSON

sqlite3 "$cursor/store.db" <<SQL
CREATE TABLE meta(key TEXT PRIMARY KEY,value TEXT);
CREATE TABLE blobs(id TEXT PRIMARY KEY,data BLOB);
INSERT INTO meta VALUES('0',hex('{"agentId":"$id","name":"Fixture","createdAt":1793836800000,"mode":"default","latestRootBlobId":"b01d320922d0c8fa0385cdbdada3ffa572cd47f5375cdc9789043c0a1ef4c131"}'));
INSERT INTO blobs VALUES('3ca5b456ee3c0d382e0f5ebd52338465d7928dff8684b5d93f1c2a9d52a730fb',X'0a3852656d656d626572206669787475726520383331343720736b2d616e742d6162636465666768696a6b6c6d6e6f707172737475767778797a122432323232323232322d323232322d343232322d383232322d323232323232323232323232');
INSERT INTO blobs VALUES('fcc09967a478910261c3a92b873cac38102ddfa0b06f51d9cb5d38baff50b599',X'0a220a203ca5b456ee3c0d382e0f5ebd52338465d7928dff8684b5d93f1c2a9d52a730fb');
INSERT INTO blobs VALUES('b01d320922d0c8fa0385cdbdada3ffa572cd47f5375cdc9789043c0a1ef4c131',X'4220fcc09967a478910261c3a92b873cac38102ddfa0b06f51d9cb5d38baff50b599');
SQL
printf '{"cwd":"/rehome-session-project"}\n' > "$cursor/meta.json"
mkdir -p "$HOME/.cursor/acp-sessions/$id"
cp "$cursor/store.db" "$cursor/meta.json" "$HOME/.cursor/acp-sessions/$id/"

PATH="$work/bin" "$rehome" scan --yes --json --config "$work/scanned" > "$work/default.json"
[[ ! -e "$work/scanned/files/claude-code-sessions" && ! -e "$work/scanned/files/cursor-sessions" ]]
PATH="$work/bin" "$rehome" scan --sessions --yes --json --config "$work/scanned" > "$work/scan.json"
for kind in claude-code codex cursor; do
  [[ -d "$work/scanned/files/$kind-sessions" ]]
done
if grep -rq 'sk-ant-abcdefghijklmnopqrstuvwxyz' "$work/scanned"; then
  echo "Session capture kept a secret" >&2
  exit 1
fi

export HOME="$work/target" XDG_CONFIG_HOME="$work/target-config" XDG_STATE_HOME="$work/target-state"
export CURSOR_CONFIG_DIR="$HOME/.cursor"
mkdir -p "$HOME"
PATH="$work/bin" "$rehome" sync --yes --offline --config "$work/scanned" > "$work/default-restore.log"
[[ ! -e "$HOME/.claude/projects" && ! -e "$HOME/.cursor/chats" ]]
if PATH="$work/bin" "$rehome" status --sessions --session-path-map "/rehome-session-project=$work/target-project" --json --config "$work/scanned" > "$work/status.json"; then
  echo "Status did not report missing sessions" >&2
  exit 1
else
  [[ "$?" == 1 ]]
fi
grep -q 'session missing:' "$work/status.json"
PATH="$work/bin" "$rehome" sync --sessions --session-path-map "/rehome-session-project=$work/target-project" --yes --offline --config "$work/scanned" > "$work/restore.log"
claude_session="$(find "$HOME/.claude/projects" -name "$id.jsonl")"
codex_session="$(find "$HOME/.codex/sessions" -name "*$id.jsonl")"
cursor_db="$(find "$HOME/.cursor/chats" -name store.db)"
[[ -f "$HOME/.cursor/acp-sessions/$id/store.db" ]]
grep -q "$work/target-project" "$claude_session"
grep -q "$work/target-project" "$codex_session"
[[ "$(sqlite3 "$cursor_db" 'SELECT count(*) FROM blobs')" == 3 ]]
[[ "$(sqlite3 "$cursor_db" "SELECT count(*) FROM blobs WHERE instr(CAST(data AS TEXT),'sk-ant-')>0")" == 0 ]]
printf 'local history\n' >> "$claude_session"
PATH="$work/bin" "$rehome" sync --sessions --session-path-map "/rehome-session-project=$work/target-project" --yes --offline --config "$work/scanned" > "$work/repeat.log"
grep -q 'local history' "$claude_session"
