#!/usr/bin/env bash
# API E2E smoke: login → menus → list repos/jobs/runs → projects docs endpoints → AI/PAT surfaces
# → harness full chain (REST + WS against a scripted fake `opencode serve`) → RBAC/audit →
# harness.enabled=false 503 gates (server restart without the backend).
# Does not require a full build pipeline (optional when BEDROCK_SMOKE_FULL=1 and fixtures exist).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

ensure_dirs
DATA_DIR="$SMOKE_TMP/e2e-data"
CFG="$SMOKE_TMP/e2e-config.yaml"
LOG="$SMOKE_TMP/e2e-server.log"
PORT="${SMOKE_PORT:-18081}"
BASE="http://127.0.0.1:${PORT}"

rm -rf "$DATA_DIR"
mkdir -p "$DATA_DIR"
write_smoke_config "$CFG" "$DATA_DIR" sqlite "$PORT"

# Harness backend for the full-chain phase: the scripted fake serve the
# process tests use, supervised by the server exactly like a real opencode.
FAKESERVE="$SMOKE_TMP/fakeserve"
(cd "$ROOT" && CGO_ENABLED=0 go build -o "$FAKESERVE" ./internal/harness/testdata/fakeserve)
HARNESS_PORT="${SMOKE_HARNESS_PORT:-14096}"
cat >>"$CFG" <<EOF
harness:
  enabled: true
  backend: opencode
  bin: "$FAKESERVE"
  port: ${HARNESS_PORT}
  approval_mode: manual
EOF

BIN="$(build_server_bin "$SMOKE_TMP/bedrock-e2e")"
"$BIN" --config "$CFG" >"$LOG" 2>&1 &
PID=$!
cleanup() { kill "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true; }
trap cleanup EXIT

wait_http "$BASE/api/v1/health" 80
TOKEN="$(api_login "$BASE")"
AUTH=(-H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json")

echo "==> menus / RBAC"
ME="$(curl -fsS "$BASE/api/v1/auth/me" "${AUTH[@]}")"
json_get "$ME" "len(o['data']['menus'])" >/dev/null

echo "==> resource + CI/CD list surfaces"
for path in \
  /resource/repositories \
  /resource/servers \
  /resource/credentials \
  /build-jobs \
  /build-runs
do
  CODE="$(curl -sS -o /tmp/smoke-body.json -w '%{http_code}' "$BASE/api/v1$path" -H "Authorization: Bearer $TOKEN")"
  [[ "$CODE" == "200" ]] || { echo "GET $path → $CODE $(cat /tmp/smoke-body.json)" >&2; exit 1; }
done

echo "==> create local repo + job (build path)"
REPO="$(curl -fsS -X POST "$BASE/api/v1/resource/repositories" "${AUTH[@]}" \
  -d '{"name":"smoke-repo","repo_url":"https://example.com/smoke.git"}')"
REPO_ID="$(json_get "$REPO" "o['data']['id']")"

JOB="$(curl -fsS -X POST "$BASE/api/v1/build-jobs" "${AUTH[@]}" \
  -d "{\"repository_id\":$REPO_ID,\"name\":\"smoke-job\",\"branch\":\"main\",\"build_script\":\"echo smoke\",\"work_dir\":\"\",\"trigger_manual\":true}")"
JOB_ID="$(json_get "$JOB" "o['data']['id']")"
echo "repo=$REPO_ID job=$JOB_ID"

# Trigger may fail at clone; we still accept 200/202 and inspect run list.
RUN_CODE="$(curl -sS -o /tmp/smoke-run.json -w '%{http_code}' -X POST \
  "$BASE/api/v1/build-jobs/$JOB_ID/runs" "${AUTH[@]}" -d '{}')"
echo "trigger run HTTP $RUN_CODE: $(cat /tmp/smoke-run.json)"
RUNS="$(curl -fsS "$BASE/api/v1/build-runs?page=1&page_size=5" -H "Authorization: Bearer $TOKEN")"
json_get "$RUNS" "o['data'].get('total',0)" >/dev/null

echo "==> projects / docs CRUD surface"
PROJ="$(curl -fsS -X POST "$BASE/api/v1/projects" "${AUTH[@]}" \
  -d '{"name":"Smoke Project","slug":"smoke-project","description":"e2e"}')"
PROJ_ID="$(json_get "$PROJ" "o['data']['id']")"
NODE="$(curl -fsS -X POST "$BASE/api/v1/projects/$PROJ_ID/docs" "${AUTH[@]}" \
  -d '{"name":"readme.md","kind":"doc","content":"# Smoke\n"}')"
NODE_ID="$(json_get "$NODE" "o['data']['id']")"
NODE_GET="$(curl -fsS "$BASE/api/v1/projects/$PROJ_ID/docs/$NODE_ID" -H "Authorization: Bearer $TOKEN")"
echo "doc get ok: $(json_get "$NODE_GET" "o['data']['name'] == 'readme.md'")"
curl -fsS -X PUT "$BASE/api/v1/projects/$PROJ_ID/docs/$NODE_ID" "${AUTH[@]}" \
  -d '{"name":"readme.md","kind":"doc","content":"# Smoke v2\n"}' >/dev/null
DOCS_TREE="$(curl -fsS "$BASE/api/v1/projects/$PROJ_ID/docs" -H "Authorization: Bearer $TOKEN")"
echo "doc tree ok: $(json_get "$DOCS_TREE" "len(o['data']) > 0")"

echo "==> AI / PAT surfaces"
for path in /resource/clis /ai/agents /ai/runs /skills /resource/tokens; do
  CODE="$(curl -sS -o /tmp/smoke-ai.json -w '%{http_code}' "$BASE/api/v1$path" -H "Authorization: Bearer $TOKEN")"
  [[ "$CODE" == "200" ]] || { echo "GET $path → $CODE $(cat /tmp/smoke-ai.json)" >&2; exit 1; }
done

PAT="$(curl -fsS -X POST "$BASE/api/v1/resource/tokens" "${AUTH[@]}" \
  -d '{"name":"smoke-pat","scopes":["skills:read","agents:run"]}')"
PAT_TOKEN="$(json_get "$PAT" "o['data'].get('token') or ''")"
if [[ -n "$PAT_TOKEN" && "$PAT_TOKEN" != "None" ]]; then
  SKILLS_CODE="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/v1/skills" \
    -H "Authorization: Bearer $PAT_TOKEN")"
  echo "PAT skills:read → HTTP $SKILLS_CODE"
  # Optional: agents:run surface (may 404/400 without agent id — list is enough for smoke)
fi

echo "==> notifications REST + WS"
NOTIF_LIST="$(curl -fsS "$BASE/api/v1/notifications?page=1&page_size=20" -H "Authorization: Bearer $TOKEN")"
json_get "$NOTIF_LIST" "o['data'].get('total',0)" >/dev/null
# Missing token must not upgrade
WS_NOAUTH="$(curl -sS -o /tmp/smoke-ws-noauth.txt -w '%{http_code}' "$BASE/ws/notifications")"
[[ "$WS_NOAUTH" == "401" ]] || { echo "expected WS 401 without token, got $WS_NOAUTH" >&2; exit 1; }
# Real WebSocket upgrade with JWT (Bun has a built-in WebSocket client)
WS_HOST="${BASE#http://}"
WS_HOST="${WS_HOST#https://}"
WS_OK="$(
  bun -e "
const ws = new WebSocket('ws://${WS_HOST}/ws/notifications?token=' + encodeURIComponent(process.argv[1]));
const t = setTimeout(() => { console.log('timeout'); ws.close(); process.exit(1); }, 3000);
ws.onopen = () => { clearTimeout(t); console.log('ok'); ws.close(); process.exit(0); };
ws.onerror = () => { clearTimeout(t); console.log('error'); process.exit(1); };
" "$TOKEN" 2>/dev/null || true
)"
[[ "$WS_OK" == "ok" ]] || { echo "notification WS upgrade failed: '$WS_OK'" >&2; exit 1; }
echo "notification WS upgrade ok"

# Wait for triggered run terminal → persisted inbox (build may fail at clone; still notifies)
echo "==> wait for build-run notification"
NOTIF_OK=0
for _ in $(seq 1 40); do
  NOTIF_LIST="$(curl -fsS "$BASE/api/v1/notifications?page=1&page_size=20" -H "Authorization: Bearer $TOKEN")"
  COUNT="$(json_get "$NOTIF_LIST" "len([x for x in (o.get('data') or {}).get('items') or [] if str(x.get('type','')).startswith('build_run_')])")"
  if [[ "$COUNT" != "0" ]]; then
    NOTIF_OK=1
    echo "notification items (build_run_*): $COUNT"
    break
  fi
  sleep 0.5
done
[[ "$NOTIF_OK" == "1" ]] || { echo "no build_run_* notification after trigger; last=$NOTIF_LIST" >&2; exit 1; }
MARK_ALL="$(curl -sS -o /tmp/smoke-notif-read.json -w '%{http_code}' -X PUT \
  "$BASE/api/v1/notifications/read-all" -H "Authorization: Bearer $TOKEN")"
[[ "$MARK_ALL" == "200" ]] || { echo "mark-all-read failed $MARK_ALL $(cat /tmp/smoke-notif-read.json)" >&2; exit 1; }

# --- harness full chain (fake opencode serve) ---

echo "==> harness catalogs (fake serve backend)"
MODELS="$(curl -fsS "$BASE/api/v1/harness/models" "${AUTH[@]}")"
json_get "$MODELS" "any(m.get('id')=='smoke-model' and m.get('provider')=='bedrock-p-smoke' for m in o['data'])" >/dev/null
AGENTS="$(curl -fsS "$BASE/api/v1/harness/agents" "${AUTH[@]}")"
json_get "$AGENTS" "any(a.get('name')=='build' and a.get('native') for a in o['data'])" >/dev/null

echo "==> harness session create + guards"
DIR_CODE="$(curl -sS -o /tmp/smoke-harness-dir.json -w '%{http_code}' -X POST \
  "$BASE/api/v1/harness/sessions" "${AUTH[@]}" -d '{"directory":"/tmp"}')"
[[ "$DIR_CODE" == "400" ]] || { echo "create with directory → $DIR_CODE $(cat /tmp/smoke-harness-dir.json)" >&2; exit 1; }

SESS="$(curl -fsS -X POST "$BASE/api/v1/harness/sessions" "${AUTH[@]}" \
  -d '{"agent":"build","model":{"provider":"bedrock-p-smoke","id":"smoke-model"}}')"
SID="$(json_get "$SESS" "o['data']['id']")"
json_get "$SESS" "o['data']['directory'].startswith('/') and o['data']['model']['id']=='smoke-model'" >/dev/null
echo "harness session: $SID"

SLIST="$(curl -fsS "$BASE/api/v1/harness/sessions" "${AUTH[@]}")"
json_get "$SLIST" "any(s['id']=='$SID' for s in o['data']['items'])" >/dev/null
NF_CODE="$(curl -sS -o /tmp/smoke-harness-nf.json -w '%{http_code}' "$BASE/api/v1/harness/sessions/ses_nope" "${AUTH[@]}")"
[[ "$NF_CODE" == "404" ]] || { echo "unknown session → $NF_CODE $(cat /tmp/smoke-harness-nf.json)" >&2; exit 1; }
json_get "$(cat /tmp/smoke-harness-nf.json)" "o['message']=='harness-session-not-found'" >/dev/null
PEND_CODE="$(curl -sS -o /tmp/smoke-harness-pend.json -w '%{http_code}' -X POST \
  "$BASE/api/v1/harness/sessions/$SID/permissions/req_unknown" "${AUTH[@]}" -d '{"reply":"once"}')"
[[ "$PEND_CODE" == "409" ]] || { echo "reply unknown pending → $PEND_CODE $(cat /tmp/smoke-harness-pend.json)" >&2; exit 1; }
json_get "$(cat /tmp/smoke-harness-pend.json)" "o['message']=='harness-pending-not-found'" >/dev/null
DELIV_CODE="$(curl -sS -o /tmp/smoke-harness-deliv.json -w '%{http_code}' -X POST \
  "$BASE/api/v1/harness/sessions/$SID/messages" "${AUTH[@]}" -d '{"text":"x","delivery":"later"}')"
[[ "$DELIV_CODE" == "400" ]] || { echo "invalid delivery → $DELIV_CODE $(cat /tmp/smoke-harness-deliv.json)" >&2; exit 1; }

echo "==> harness WS: prompt → live frames → approval reply → idle"
WS_DIR="$SMOKE_TMP/harness-ws"
rm -rf "$WS_DIR"; mkdir -p "$WS_DIR"
cat >"$WS_DIR/live.mjs" <<'EOF'
const [wsUrl, token, apiBase, sid, framesOut, ackOut] = process.argv.slice(2);
const fs = await import("node:fs");
const ws = new WebSocket(wsUrl);
const frames = [];
const dump = () => fs.writeFileSync(framesOut, frames.map((f) => JSON.stringify(f)).join("\n") + "\n");
const fail = (msg) => { console.error(msg); dump(); process.exit(1); };
const timer = setTimeout(() => fail("timeout waiting for session frames"), 60000);
const post = (path, body) => fetch(apiBase + path, {
  method: "POST",
  headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
  body: JSON.stringify(body),
});
let replied = false;
ws.onerror = () => fail("ws error");
ws.onopen = async () => {
  const resp = await post(`/api/v1/harness/sessions/${sid}/messages`, { text: "smoke hello" });
  const body = await resp.json().catch(() => ({}));
  fs.writeFileSync(ackOut, JSON.stringify({ status: resp.status, body }));
  if (resp.status !== 202) fail("send message → " + resp.status);
};
ws.onmessage = (ev) => {
  const f = JSON.parse(ev.data);
  frames.push(f);
  if (f.kind === "permission" && !replied) {
    replied = true;
    post(`/api/v1/harness/sessions/${sid}/permissions/${f.permission.requestId}`, { reply: "once" })
      .catch((e) => fail("permission reply failed: " + e.message));
  }
  if (f.kind === "status" && f.status && f.status.name === "idle" && replied) {
    clearTimeout(timer);
    setTimeout(() => { dump(); process.exit(0); }, 500);
  }
};
EOF
bun "$WS_DIR/live.mjs" "ws://127.0.0.1:${PORT}/ws/harness/sessions/${SID}/events?token=${TOKEN}" \
  "$TOKEN" "$BASE" "$SID" "$WS_DIR/live.jsonl" "$WS_DIR/ack.json" || {
    echo "harness live WS failed" >&2; exit 1; }
json_get "$(cat "$WS_DIR/ack.json")" "o['status']==202 and o['body']['data']['admitted_seq']>0 and bool(o['body']['data']['id'])" >/dev/null

echo "==> harness WS: replay (after=0) of durable frames"
cat >"$WS_DIR/replay.mjs" <<'EOF'
const [wsUrl, framesOut, quietMsArg] = process.argv.slice(2);
const fs = await import("node:fs");
const quietMs = Number(quietMsArg || 2000);
const ws = new WebSocket(wsUrl);
const frames = [];
const done = () => {
  fs.writeFileSync(framesOut, frames.map((f) => JSON.stringify(f)).join("\n") + "\n");
  process.exit(frames.length ? 0 : 1);
};
const timer = setTimeout(() => { console.error("replay timeout without frames"); process.exit(1); }, 20000);
let quiet;
ws.onerror = () => { console.error("ws error"); process.exit(1); };
ws.onmessage = (ev) => {
  frames.push(JSON.parse(ev.data));
  if (quiet) clearTimeout(quiet);
  quiet = setTimeout(() => { clearTimeout(timer); done(); }, quietMs);
};
EOF
bun "$WS_DIR/replay.mjs" "ws://127.0.0.1:${PORT}/ws/harness/sessions/${SID}/events?token=${TOKEN}&after=0" \
  "$WS_DIR/replay.jsonl" 2000 || { echo "harness replay WS failed" >&2; exit 1; }

for MODE in live replay; do
  python3 - "$SID" "$MODE" "$WS_DIR/$MODE.jsonl" <<'PY' || exit 1
import json, sys
sid, mode, path = sys.argv[1:4]
frames = [json.loads(l) for l in open(path) if l.strip()]
kinds = lambda k: [f for f in frames if f.get("kind") == k]
text = lambda f: (f.get("messageText") or {}).get("text", "")
err = []
if not frames:
    err.append("no frames")
if any(f.get("sessionId") != sid for f in frames):
    err.append("foreign sessionId")
if mode == "live":
    if not any("smoke-delta-" in (f.get("messageDelta") or {}).get("delta", "") for f in kinds("message_delta")):
        err.append("missing message_delta")
    if not kinds("permission") or not kinds("permission")[0]["permission"].get("requestId"):
        err.append("missing permission ask")
    if not any((f.get("toolCall") or {}).get("tool") == "exec" for f in kinds("tool_call")):
        err.append("missing tool_call")
    if not any("smoke-done-" in text(f) for f in kinds("message_text")):
        err.append("missing final message_text")
    if not any((f.get("status") or {}).get("name") == "idle" for f in kinds("status")):
        err.append("missing idle")
    if any(f.get("seq", 0) != 0 for f in kinds("message_delta") + kinds("permission")):
        err.append("transient frame carries seq")
else:
    if kinds("message_delta") or kinds("permission"):
        err.append("transient frame replayed")
    if not any("smoke-done-" in text(f) for f in kinds("message_text")):
        err.append("missing replay message_text")
    if not any((f.get("status") or {}).get("name") == "idle" for f in kinds("status")):
        err.append("missing replay idle")
seqs = [f["seq"] for f in frames if f.get("seq", 0) > 0]
if len(seqs) != len(set(seqs)):
    err.append("duplicate durable seq")
if seqs != sorted(seqs):
    err.append("durable seqs out of order")
if err:
    print("FAIL: harness %s frames: %s" % (mode, err), file=sys.stderr)
    sys.exit(1)
print("ok: harness %s frames: %d" % (mode, len(frames)))
PY
done

echo "==> harness history / export / interrupt / audit"
MSGS="$(curl -fsS "$BASE/api/v1/harness/sessions/$SID/messages" "${AUTH[@]}")"
json_get "$MSGS" \
  "o['data']['items'][0]['role']=='user' and o['data']['items'][0]['content'][0]['text']=='smoke hello' and o['data']['items'][-1]['role']=='assistant' and o['data']['items'][-1]['content'][0]['text'].startswith('smoke-done-')" >/dev/null
EXPORT_LINES="$(curl -fsS "$BASE/api/v1/harness/sessions/$SID/export" "${AUTH[@]}" | wc -l | tr -d ' ')"
[[ "$EXPORT_LINES" == "2" ]] || { echo "export lines=$EXPORT_LINES want 2" >&2; exit 1; }
INTR_CODE="$(curl -sS -o /tmp/smoke-harness-intr.json -w '%{http_code}' -X POST \
  "$BASE/api/v1/harness/sessions/$SID/interrupt" "${AUTH[@]}" -d '{}')"
[[ "$INTR_CODE" == "200" ]] || { echo "interrupt → $INTR_CODE $(cat /tmp/smoke-harness-intr.json)" >&2; exit 1; }
AUDIT="$(curl -fsS "$BASE/api/v1/operation-logs?action=harness_permission_reply&page=1&page_size=20" "${AUTH[@]}")"
json_get "$AUDIT" "any(i.get('resource_id')=='$SID' for i in o['data']['items'])" >/dev/null
echo "harness permission reply audited"

echo "==> harness RBAC: plain user has all non-super-admin-only features"
curl -fsS -X POST "$BASE/api/v1/users" "${AUTH[@]}" \
  -d '{"username":"smoke-plain","password":"smoke-pass-123","display_name":"Plain User"}' >/dev/null
PLAIN_LOGIN="$(curl -fsS -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"smoke-plain","password":"smoke-pass-123"}')"
PLAIN_TOKEN="$(json_get "$PLAIN_LOGIN" "o['data']['access_token']")"
RB_SEND="$(curl -sS -o /tmp/smoke-harness-plain.json -w '%{http_code}' -X POST \
  "$BASE/api/v1/harness/sessions" -H "Authorization: Bearer $PLAIN_TOKEN" -H 'Content-Type: application/json' -d '{}')"
[[ "$RB_SEND" == "201" ]] || { echo "plain create → $RB_SEND $(cat /tmp/smoke-harness-plain.json)" >&2; exit 1; }
RB_VIEW="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/v1/harness/models" -H "Authorization: Bearer $PLAIN_TOKEN")"
[[ "$RB_VIEW" == "200" ]] || { echo "plain models → $RB_VIEW" >&2; exit 1; }
RB_SUPER="$(curl -sS -o /tmp/smoke-harness-super.json -w '%{http_code}' \
  "$BASE/api/v1/dashboard/system-info" -H "Authorization: Bearer $PLAIN_TOKEN")"
[[ "$RB_SUPER" == "403" ]] || { echo "plain super-admin-only dashboard → $RB_SUPER $(cat /tmp/smoke-harness-super.json)" >&2; exit 1; }

# --- harness.enabled=false: gates answer 503, no CLI fallback ---

echo "==> harness disabled: 503 gates (server restart without backend)"
kill "$PID" 2>/dev/null || true
wait "$PID" 2>/dev/null || true
CFG2="$SMOKE_TMP/e2e-config-off.yaml"
LOG2="$SMOKE_TMP/e2e-server-off.log"
PORT2=$((PORT + 1))
BASE2="http://127.0.0.1:${PORT2}"
write_smoke_config "$CFG2" "$DATA_DIR" sqlite "$PORT2"
"$BIN" --config "$CFG2" >"$LOG2" 2>&1 &
PID=$!
wait_http "$BASE2/api/v1/health" 80
TOKEN2="$(api_login "$BASE2")"
OFF_AUTH=(-H "Authorization: Bearer $TOKEN2" -H "Content-Type: application/json")
for CHECK in \
  "GET|/api/v1/harness/models|" \
  "GET|/api/v1/harness/sessions|" \
  "POST|/api/v1/harness/sessions|{}" \
  "POST|/api/v1/harness/sessions/$SID/messages|{\"text\":\"x\"}" \
  "GET|/api/v1/ai/models|"
do
  IFS='|' read -r METHOD PATHSPEC BODY <<<"$CHECK"
  ARGS=(-sS -o /tmp/smoke-harness-off.json -w '%{http_code}' -X "$METHOD" "$BASE2$PATHSPEC" "${OFF_AUTH[@]}")
  [[ -n "$BODY" ]] && ARGS+=(-d "$BODY")
  CODE="$(curl "${ARGS[@]}")"
  [[ "$CODE" == "503" ]] || { echo "$METHOD $PATHSPEC → $CODE $(cat /tmp/smoke-harness-off.json)" >&2; exit 1; }
done
json_get "$(cat /tmp/smoke-harness-off.json)" "o['message']=='harness-unavailable'" >/dev/null
WS_OFF="$(curl -sS -o /tmp/smoke-harness-ws-off.json -w '%{http_code}' \
  "$BASE2/ws/harness/sessions/$SID/events?token=$TOKEN2")"
[[ "$WS_OFF" == "503" ]] || { echo "WS with harness off → $WS_OFF $(cat /tmp/smoke-harness-ws-off.json)" >&2; exit 1; }
AI_STILL="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE2/api/v1/ai/agents" "${OFF_AUTH[@]}")"
[[ "$AI_STILL" == "200" ]] || { echo "ai/agents with harness off → $AI_STILL (CRUD must stay up)" >&2; exit 1; }

echo "PASS: api-e2e smoke"
