#!/usr/bin/env bash
# run-build.sh — 触发 Bedrock 平台构建任务，可选等待完成并拉取日志。
# 本脚本自包含（仅依赖 bash + curl），可直接复制到下游项目使用。
#
# 用法:
#   ./run-build.sh [JOB_ID] [选项]
#
# 配置（环境变量；已存在的变量优先级最高，其次 .env.local / .env）:
#   BEDROCK_HOST          平台地址，如 https://bedrock.example.com
#   BEDROCK_PAT           访问令牌（PAT，scope 需含 builds:run）
#   BEDROCK_BUILD_JOB_ID  默认构建任务 ID（位置参数或 --job-id 可覆盖）
#
# 选项:
#   --job-id <id>   构建任务 ID（与位置参数等价）
#   --branch <b>    指定构建分支
#   --wait          轮询等待构建结束（默认仅触发即返回）
#   --log           拉取构建日志并打印（配合 --wait 时在结束后拉取）
#   --interval <s>  轮询间隔秒数（默认 5）
#   --timeout <s>   最长等待秒数（默认 1800）
#   --env-file <f>  额外加载的 env 文件
#   -h, --help      显示帮助
#
# 退出码:
#   0  构建成功；或未 --wait 时触发成功
#   1  构建失败 / cancelled / interrupted
#   2  用法或配置错误
#   3  网络或 API 错误
#
# 对接速览（Bedrock Open API）:
#   POST /api/v1/build-jobs/{id}/runs   入队构建运行（202 异步）
#     body: { "branch": "...", "trigger_type": "manual" }（branch 可省略）
#     鉴权: PAT scope builds:run；JWT 需 cicd_build_jobs:execute
#   GET  /api/v1/build-runs/{id}        查询运行状态
#     status 枚举: queued / running / success / failed / cancelled / interrupted
#   GET  /api/v1/build-runs/{id}/log    构建日志文本
#   响应包络: { code, message, data }；data 即 BuildRun 对象
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  sed -n '2,31p' "$SCRIPT_DIR/run-build.sh" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

# 从 env 文件加载变量，不覆盖已存在的环境变量
load_env_file() {
  local f="$1" k v
  [[ -f "$f" ]] || return 0
  while IFS='=' read -r k v; do
    [[ "$k" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    if [[ -z "${!k:-}" ]]; then
      v="${v%\"}"; v="${v#\"}"
      export "$k=$v"
    fi
  done < <(sed -e 's/^[[:space:]]*//' -e '/^#/d' -e '/^$/d' "$f")
}

# 从 JSON 提取 "key":value（数字或字符串），取第一次出现
json_field() {
  local json="$1" key="$2"
  grep -o "\"$key\":[^,}]*" <<<"$json" | head -1 | sed "s/\"$key\"://; s/^[[:space:]]*//; s/^\"//; s/\"$//"
}

BRANCH=""
WAIT=0
FETCH_LOG=0
INTERVAL=5
TIMEOUT=1800
JOB_ID=""
ENV_FILES=("$SCRIPT_DIR/../.env" "$SCRIPT_DIR/.env" ".env.local" ".env")

while [[ $# -gt 0 ]]; do
  case "$1" in
    --job-id)   JOB_ID="$2"; shift 2 ;;
    --branch)   BRANCH="$2"; shift 2 ;;
    --wait)     WAIT=1; shift ;;
    --log)      FETCH_LOG=1; shift ;;
    --interval) INTERVAL="$2"; shift 2 ;;
    --timeout)  TIMEOUT="$2"; shift 2 ;;
    --env-file) ENV_FILES+=("$2"); shift 2 ;;
    -h|--help)  usage 0 ;;
    -*)
      echo "未知选项: $1" >&2
      usage 2
      ;;
    *)
      [[ -z "$JOB_ID" ]] || { echo "构建任务 ID 重复指定" >&2; exit 2; }
      JOB_ID="$1"
      shift
      ;;
  esac
done

for f in "${ENV_FILES[@]}"; do load_env_file "$f"; done

HOST="${BEDROCK_HOST:-}"
PAT="${BEDROCK_PAT:-}"
[[ -z "$JOB_ID" ]] && JOB_ID="${BEDROCK_BUILD_JOB_ID:-}"

[[ -n "$HOST" ]] || { echo "缺少 BEDROCK_HOST（平台地址）" >&2; exit 2; }
[[ -n "$PAT" ]] || { echo "缺少 BEDROCK_PAT（访问令牌，scope 需含 builds:run）" >&2; exit 2; }
[[ -n "$JOB_ID" ]] || { echo "缺少构建任务 ID（位置参数、--job-id 或 BEDROCK_BUILD_JOB_ID）" >&2; exit 2; }

api_request() { # api_request <method> <path> [body] — 输出 body，非 2xx 时退出 3
  local method="$1" path="$2" body="${3:-}"
  local out code
  out="$(curl -sS -w $'\n%{http_code}' -X "$method" "$HOST/api/v1$path" \
    -H "Authorization: Bearer $PAT" -H 'Content-Type: application/json' ${body:+-d "$body"})"
  code="${out##*$'\n'}"
  out="${out%$'\n'*}"
  if [[ "$code" -lt 200 || "$code" -ge 300 ]]; then
    echo "API 错误 ($code): $(json_field "$out" message)" >&2
    exit 3
  fi
  printf '%s' "$out"
}

echo "==> 触发构建: job=$JOB_ID${BRANCH:+ branch=$BRANCH} @ $HOST"
BODY="{}"
[[ -n "$BRANCH" ]] && BODY="{\"branch\":\"$BRANCH\"}"
RESP="$(api_request POST "/build-jobs/$JOB_ID/runs" "$BODY")"
RUN_ID="$(json_field "$RESP" id)"
[[ -n "$RUN_ID" ]] || { echo "触发响应缺少 run id: $RESP" >&2; exit 3; }
echo "==> 构建已入队: run=$RUN_ID (job=$JOB_ID)"
[[ "$WAIT" -eq 1 ]] || exit 0

deadline=$(( $(date +%s) + TIMEOUT ))
while :; do
  RESP="$(api_request GET "/build-runs/$RUN_ID")"
  STATUS="$(json_field "$RESP" status)"
  case "$STATUS" in
    success)
      echo "==> 构建成功: run=$RUN_ID"
      break
      ;;
    failed|cancelled|interrupted)
      echo "==> 构建结束: run=$RUN_ID status=${STATUS} $(json_field "$RESP" error_message)"
      break
      ;;
    *)
      echo "==> 构建中: status=${STATUS} $(date +%H:%M:%S)"
      ;;
  esac
  if [[ "$(date +%s)" -ge "$deadline" ]]; then
    echo "==> 等待超时（${TIMEOUT}s），status=${STATUS}，请到平台查看详情" >&2
    exit 3
  fi
  sleep "$INTERVAL"
done

if [[ "$FETCH_LOG" -eq 1 ]]; then
  echo "---- 构建日志 ----"
  api_request GET "/build-runs/$RUN_ID/log"
  echo
fi

[[ "$STATUS" == "success" ]]