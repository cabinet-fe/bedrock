#!/usr/bin/env bash
# API E2E smoke: login → menus → list repos/jobs/runs → projects docs endpoints → AI/PAT surfaces.
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

echo "PASS: api-e2e smoke"
