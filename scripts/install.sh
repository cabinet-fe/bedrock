#!/usr/bin/env bash
# Bedrock one-line installer: interactive setup of Bedrock Server (main) or
# Deploy Agent, with mainland-China GitHub mirror fallbacks and a
# download -> graceful stop -> replace -> restart update flow.
#
# See .agents/docs/ops-handbook.md for the manual install path.
set -euo pipefail

REPO="cabinet-fe/bedrock"
RELEASE_BASE="${BEDROCK_RELEASE_BASE:-https://github.com/${REPO}}"
# Public gh proxies (prefix mode: ${mirror}https://github.com/...); edit as they come and go.
MIRRORS=("https://gh-proxy.com/" "https://ghfast.top/")

COMMAND=""
OPT_DIR=""
OPT_VERSION=""
OPT_PORT=""
OPT_ADMIN_USER=""
OPT_ADMIN_PASS=""
OPT_TOKEN=""
OPT_ADDR=""
OPT_MIRROR="${BEDROCK_MIRROR:-}"
OPT_NO_MIRROR=0
OPT_YES=0
MIRROR=""
IS_ROOT=0
[ "$(id -u)" = "0" ] && IS_ROOT=1

# ---------------------------------------------------------------- output ---

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_G=$'\033[32m'; C_Y=$'\033[33m'; C_R=$'\033[31m'; C_B=$'\033[1m'; C_0=$'\033[0m'
else
  C_G=""; C_Y=""; C_R=""; C_B=""; C_0=""
fi
info()  { printf '%s\n' "${C_G}==>${C_0} $*"; }
warn()  { printf '%s\n' "${C_Y}警告:${C_0} $*" >&2; }
err()   { printf '%s\n' "${C_R}错误:${C_0} $*" >&2; }
die()   { err "$*"; exit 1; }

usage() {
  cat <<'EOF'
Bedrock 一键安装 / 更新脚本

用法:
  ./install.sh                     交互模式（推荐）
  ./install.sh server   [选项]     安装 Bedrock Server（主体）
  ./install.sh agent    [选项]     安装 Deploy Agent（代理分发工具）
  ./install.sh update   [选项]     更新已安装组件（下载 → 优雅停机 → 更新 → 重启）
  ./install.sh status              查看安装状态

选项:
  --mirror <URL>     GitHub 镜像前缀，如 https://gh-proxy.com/
  --no-mirror        强制直连 GitHub
  --dir <DIR>        安装目录（默认 /opt/bedrock[-agent]，非 root 为 ~/bedrock[-agent]）
  --version <TAG>    指定版本（默认最新 release，如 v2.0.0）
  --port <N>         Server 监听端口（默认 8080）
  --admin-user <U>   超级管理员用户名（默认 admin）
  --admin-pass <P>   超级管理员密码（默认随机生成并打印）
  --token <T>        Agent 认证 token（默认随机生成并打印，需填回平台「服务器」配置）
  --addr <A>         Agent 监听地址（默认 :9091）
  --yes              非交互模式，未提供的参数取默认值

环境变量:
  BEDROCK_MIRROR         等效 --mirror
  BEDROCK_RELEASE_BASE   覆盖下载源（默认 GitHub Releases，测试用）
  BEDROCK_OS / BEDROCK_ARCH  覆盖平台探测（默认 linux + uname -m）
EOF
}

# ------------------------------------------------------------ interaction ---

# Ask a question on /dev/tty so `curl ... | bash` keeps stdin for the script
# itself. Falls back to the default when no tty is available or input is empty.
ask() { # ask <prompt> <default> -> stdout
  local prompt=$1 def=$2 in=""
  printf '%s' "${C_B}${prompt}${C_0} [${def}]: " >&2
  if [ -e /dev/tty ]; then
    in=$( (read -r t </dev/tty && printf '%s' "$t") 2>/dev/null ) || true
  fi
  [ -z "$in" ] && in=$def
  printf '%s\n' "$in"
}

ask_choice() { # ask_choice <prompt> <valid-regex> <default> -> stdout
  local prompt=$1 valid=$2 def=$3 in=""
  while :; do
    in=$(ask "$prompt" "$def")
    if printf '%s' "$in" | grep -Eq "$valid"; then
      printf '%s\n' "$in"
      return 0
    fi
    warn "无效输入: $in"
  done
}

confirm() { # confirm <prompt> <default-y|n>
  local prompt=$1 def=$2
  local yn
  yn=$(ask_choice "$prompt [y/n]" '^[yYnN]$' "$def")
  [ "$yn" = "y" ] || [ "$yn" = "Y" ]
}

interactive() { [ "$OPT_YES" = "1" ] && return 1 || return 0; }

# ------------------------------------------------------------------ deps ---

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "缺少依赖命令: $1（请先安装）"
}

sha256_sum() { # sha256_sum <file> -> stdout
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "缺少 sha256sum / shasum，无法校验下载产物"
  fi
}

rand_hex() { # rand_hex <bytes> -> stdout
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$1"
  else
    od -An -tx1 -N "$1" /dev/urandom | tr -d ' \n'
  fi
}

# --------------------------------------------------------------- mirrors ---

normalize_mirror() { # ensure trailing slash, empty means direct
  local m=$1
  [ -z "$m" ] && { printf ''; return 0; }
  case $m in
    http://*|https://*) ;;
    *) m="https://$m" ;;
  esac
  case $m in
    */) ;;
    *) m="$m/" ;;
  esac
  printf '%s' "$m"
}

mirror_probe() { # mirror_probe <full-url> ; ok -> 0
  curl -m 5 -fsSI -o /dev/null -- "$1" 2>/dev/null
}

pick_mirror() {
  MIRROR=$(normalize_mirror "$OPT_MIRROR")
  if [ "$OPT_NO_MIRROR" = "1" ]; then MIRROR=""; fi
  if [ -n "$MIRROR" ]; then
    info "使用指定下载源: ${MIRROR}"
    return 0
  fi
  if [ "$OPT_NO_MIRROR" = "1" ]; then
    info "使用下载源: 直连 GitHub"
    return 0
  fi

  local direct_ok=0 m first_ok=""
  if mirror_probe "https://github.com/${REPO}/releases"; then direct_ok=1; fi
  if [ "$direct_ok" = "1" ]; then
    first_ok=""
  else
    for m in "${MIRRORS[@]}"; do
      if mirror_probe "${m}https://github.com/${REPO}/releases"; then first_ok=$m; break; fi
    done
    if [ -z "$first_ok" ]; then
      warn "GitHub 与内置镜像均不可达，将按直连继续；失败时可重试并指定 --mirror <URL>"
    fi
  fi

  if ! interactive; then
    MIRROR=$first_ok
  else
    local def_label sel custom
    if [ -n "$first_ok" ] || [ "$direct_ok" = "1" ]; then
      def_label=$([ -n "$first_ok" ] && printf '%s' "$first_ok" || printf '直连 GitHub')
    else
      def_label="直连 GitHub"
    fi
    echo
    info "下载源选择（GitHub 直连不可达时建议使用镜像）"
    printf '  [1] 直连 GitHub\n'
    printf '  [2] %s\n' "${MIRRORS[0]}"
    printf '  [3] %s\n' "${MIRRORS[1]}"
    printf '  [4] 自定义镜像 URL\n'
    printf '  当前推荐: %s\n' "$def_label"
    sel=$(ask_choice "请选择 [1-4]" '^[1-4]$' "$([ -n "$first_ok" ] && echo 2 || echo 1)")
    case $sel in
      1) MIRROR="" ;;
      2) MIRROR="${MIRRORS[0]}" ;;
      3) MIRROR="${MIRRORS[1]}" ;;
      4)
        custom=$(ask "镜像前缀 URL（如 https://gh-proxy.com）" "")
        [ -n "$custom" ] || die "未输入镜像 URL"
        MIRROR=$(normalize_mirror "$custom")
        ;;
    esac
  fi
  info "使用下载源: $([ -n "$MIRROR" ] && printf '%s' "$MIRROR" || printf '直连 GitHub')"
}

# Fetch a repo-relative path (e.g. /releases/download/vX/asset) to dest,
# rotating mirror candidates on failure. Only github.com bases get mirrored.
# Candidates may repeat (harmless retry); the first success wins and sticks.
fetch() { # fetch <repo-relative-path> <dest-file>
  local path=$1 dest=$2 base try m ok=1
  local -a candidates=()

  case $RELEASE_BASE in
    https://github.com/*)
      candidates=("${MIRROR}" "" "${MIRRORS[@]}")
      ;;
    *)  # custom base (local test server): no mirror rotation
      candidates=("")
      ;;
  esac
  for m in "${candidates[@]}"; do
    if [ -z "$m" ]; then base=$RELEASE_BASE; else base="${m}${RELEASE_BASE}"; fi
    try="${base}${path}"
    info "下载 $try"
    # shellcheck disable=SC2086
    if curl -fL ${CURL_QUIET} --connect-timeout 8 -o "$dest" -- "$try" 2>&1; then
      if [ -n "$m" ]; then MIRROR=$m; fi
      ok=0
      break
    fi
    rm -f "$dest"
  done
  [ "$ok" = "0" ] || die "下载失败: ${path}（可尝试 --mirror 指定镜像后重试）"
}

latest_tag() { # latest_tag -> stdout (e.g. v2.0.0); dies on failure
  local t="" loc base p
  # 1) GitHub API (direct; most gh proxies do not proxy api.github.com)
  t=$(curl -m 10 -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1) || true
  if [ -n "$t" ]; then printf '%s' "$t"; return 0; fi
  # 2) some mirrors 302 the releases/latest page with a Location header
  #    ending in /releases/tag/<tag> (ghfast.top style; plain github too).
  #    Mirrors that render HTML instead (gh-proxy.com style) are skipped.
  local -a prefixes=("${MIRROR}" "${MIRRORS[@]}" "")
  for p in "${prefixes[@]}"; do
    case $RELEASE_BASE in
      https://github.com/*) base="${p}${RELEASE_BASE}" ;;
      *) base="$RELEASE_BASE"; [ -n "$p" ] && continue ;;
    esac
    loc=$(curl -m 8 -fsSI "$base/releases/latest" 2>/dev/null \
          | tr -d '\r' | sed -n 's/^[Ll]ocation: *//p' | head -1) || true
    t=$(printf '%s' "$loc" | sed -n 's#.*/releases/tag/\([^/?#]*\).*#\1#p')
    [ -n "$t" ] && { printf '%s' "$t"; return 0; }
  done
  die "无法获取最新版本号（GitHub API 与 release 页均失败）。请用 --version <TAG> 指定版本后重试"
}

resolve_tag() { # resolve_tag -> stdout
  if [ -n "$OPT_VERSION" ]; then
    printf '%s' "$OPT_VERSION"
  else
    latest_tag
  fi
}

# -------------------------------------------------------------- platform ---

detect_platform() {
  OS="${BEDROCK_OS:-$(uname -s | tr '[:upper:]' '[:lower:]')}"
  case "$(uname -m)" in
    x86_64|amd64) HOST_ARCH=amd64 ;;
    aarch64|arm64) HOST_ARCH=arm64 ;;
    *) HOST_ARCH="$(uname -m)" ;;
  esac
  ARCH="${BEDROCK_ARCH:-$HOST_ARCH}"
  if [ "$OS" != "linux" ]; then
    die "本脚本面向 Linux 部署（当前: ${OS}）。macOS/Windows 请参考 README 从源码构建或手动下载发布包"
  fi
  case $ARCH in
    amd64|arm64) ;;
    *) die "不支持的架构: ${ARCH}（发布包仅提供 linux amd64/arm64）" ;;
  esac
  SUFFIX="linux-${ARCH}"
  asset_server="bedrock-${SUFFIX}"
  asset_agent="bedrock-agent-${SUFFIX}"
}

# ------------------------------------------------------------- download ----

download_verified() { # download_verified <asset> <tag> <dest>
  local asset=$1 tag=$2 dest=$3
  fetch "/releases/download/${tag}/${asset}" "$dest"
  # checksums are per-platform files covering both server and agent assets
  fetch "/releases/download/${tag}/bedrock-${SUFFIX}.sha256" "${dest}.sha256"
  local want got
  want=$(awk -v f="$asset" '{n=$2; sub(/^\*/,"",n); if (n==f) print $1}' "${dest}.sha256" | head -1)
  [ -n "$want" ] || die "校验文件中没有 ${asset} 的记录，请确认发布产物"
  got=$(sha256_sum "$dest")
  [ "$want" = "$got" ] || die "SHA256 校验失败: ${asset}
  期望: $want
  实际: $got"
  rm -f "${dest}.sha256"
}

# ------------------------------------------------------------ services ----

SYSTEMD=0
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ] && [ "$IS_ROOT" = "1" ]; then
  SYSTEMD=1
fi

svc_name() { [ "$1" = "server" ] && printf 'bedrock' || printf 'bedrock-agent'; }
svc_bin()  { [ "$1" = "server" ] && printf 'bedrock' || printf 'bedrock-agent'; }
svc_pidfile() { printf '%s/.%s.pid' "$2" "$(svc_name "$1")"; }
svc_logfile() { printf '%s/%s.log' "$2" "$(svc_name "$1")"; }

svc_exec_args() { # svc_exec_args <component> <dir>
  if [ "$1" = "server" ]; then
    printf '%s --config %s' "${2}/bedrock" "${2}/config.yaml"
  else
    printf '%s --config %s' "${2}/bedrock-agent" "${2}/bedrock-agent.yaml"
  fi
}

svc_install() { # svc_install <component> <dir>
  local comp=$1 dir=$2 name timeout
  name=$(svc_name "$comp")
  if [ "$SYSTEMD" = "1" ]; then
    if [ "$comp" = "server" ]; then timeout=45; else timeout=10; fi
    cat >"/etc/systemd/system/${name}.service" <<EOF
[Unit]
Description=Bedrock ${comp} (installed by install.sh)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${dir}
ExecStart=$(svc_exec_args "$comp" "$dir")
Restart=on-failure
RestartSec=3
TimeoutStopSec=${timeout}

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable "${name}.service" >/dev/null 2>&1 || warn "设置开机自启失败: ${name}"
  fi
}

svc_is_active() { # svc_is_active <component> <dir>
  local comp=$1 dir=$2 name pidfile
  name=$(svc_name "$comp")
  if [ "$SYSTEMD" = "1" ]; then
    systemctl is-active --quiet "${name}.service"
  else
    pidfile=$(svc_pidfile "$comp" "$dir")
    [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile" 2>/dev/null)" 2>/dev/null
  fi
}

svc_stop() { # svc_stop <component> <dir> ; graceful SIGTERM, hard kill after 60s
  local comp=$1 dir=$2 name pidfile pid i
  name=$(svc_name "$comp")
  if ! svc_is_active "$comp" "$dir"; then
    info "${name}: 未在运行"
    return 0
  fi
  info "停止 ${name}（优雅停机，最长等待 60s）..."
  if [ "$SYSTEMD" = "1" ]; then
    # systemd forwards SIGTERM and waits TimeoutStopSec itself
    systemctl stop "${name}.service" || true
    i=0
    while systemctl is-active --quiet "${name}.service" 2>/dev/null && [ $i -lt 15 ]; do
      sleep 1; i=$((i + 1))
    done
    systemctl kill --signal=SIGKILL "${name}.service" >/dev/null 2>&1 || true
  else
    pidfile=$(svc_pidfile "$comp" "$dir")
    pid=$(cat "$pidfile" 2>/dev/null || true)
    [ -n "$pid" ] || { rm -f "$pidfile"; return 0; }
    kill -TERM "$pid" 2>/dev/null || true
    i=0
    while kill -0 "$pid" 2>/dev/null && [ $i -lt 60 ]; do
      sleep 1; i=$((i + 1))
    done
    if kill -0 "$pid" 2>/dev/null; then
      warn "${name} 优雅退出超时，发送 SIGKILL"
      kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$pidfile"
  fi
  svc_is_active "$comp" "$dir" && warn "${name} 停止后仍有残留进程，请检查" || info "${name}: 已停止"
}

svc_start() { # svc_start <component> <dir>
  local comp=$1 dir=$2 name
  name=$(svc_name "$comp")
  if [ "$SYSTEMD" = "1" ]; then
    systemctl restart "${name}.service"
  else
    if svc_is_active "$comp" "$dir"; then svc_stop "$comp" "$dir"; fi
    local logfile pidfile
    logfile=$(svc_logfile "$comp" "$dir")
    pidfile=$(svc_pidfile "$comp" "$dir")
    # shellcheck disable=SC2046
    nohup $(svc_exec_args "$comp" "$dir") >>"$logfile" 2>&1 &
    echo $! >"$pidfile"
  fi
}

# ----------------------------------------------------------- health -------

yaml_value() { # yaml_value <file> <section> <key> -> stdout (first match)
  awk -v sec="^${2}:" -v key="$3" '
    $0 ~ sec { insec=1; next }
    insec && /^[^[:space:]]/ { insec=0 }
    insec && $1 == key":" { gsub(/["'"'"']/, "", $2); print $2; exit }
  ' "$1" 2>/dev/null
}

server_port() { # server_port <dir>
  local p
  p=$(yaml_value "$1/config.yaml" server port) || true
  printf '%s' "${p:-8080}"
}

agent_port() { # agent_port <dir> ; addr ":9091" or "0.0.0.0:9091" -> 9091
  local a p
  a=$(yaml_value "$1/bedrock-agent.yaml" "" addr) || true
  # addr is top-level in agent config: fall back to a flat grep
  [ -n "$a" ] || a=$(sed -n 's/^[[:space:]]*addr:[[:space:]]*"\{0,1\}\([^"[:space:]]*\).*/\1/p' "$1/bedrock-agent.yaml" 2>/dev/null | head -1)
  p=${a##*:}
  printf '%s' "${p:-9091}"
}

agent_token() { # agent_token <dir>
  sed -n 's/^[[:space:]]*token:[[:space:]]*"\{0,1\}\([^"]*\)"\{0,1\}[[:space:]]*$/\1/p' "$1/bedrock-agent.yaml" 2>/dev/null | head -1
}

wait_health() { # wait_health <url> [bearer] [tries] ; ok -> 0
  local url=$1 bearer=${2:-} tries=${3:-30} i code
  i=1
  while [ $i -le "$tries" ]; do
    if [ -n "$bearer" ]; then
      code=$(curl -m 3 -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${bearer}" "$url" 2>/dev/null) || code="000"
    else
      code=$(curl -m 3 -s -o /dev/null -w '%{http_code}' "$url" 2>/dev/null) || code="000"
    fi
    [ "$code" = "200" ] && return 0
    sleep 1; i=$((i + 1))
  done
  return 1
}

component_health() { # component_health <component> <dir>
  local comp=$1 dir=$2
  if [ "$comp" = "server" ]; then
    wait_health "http://127.0.0.1:$(server_port "$dir")/api/v1/health" "" 1
  else
    wait_health "http://127.0.0.1:$(agent_port "$dir")/healthz" "$(agent_token "$dir")" 1
  fi
}

# ------------------------------------------------------------- install ----

default_dir() { # default_dir <server|agent>
  local suffix=""
  [ "$1" = "agent" ] && suffix="-agent"
  if [ "$IS_ROOT" = "1" ]; then
    printf '/opt/bedrock%s' "$suffix"
  else
    printf '%s/bedrock%s' "$HOME" "$suffix"
  fi
}

gen_server_config() { # gen_server_config <dir> <port> <admin-user> <admin-pass>
  local dir=$1 port=$2 user=$3 pass=$4 key jwt
  key=$(rand_hex 32)
  jwt=$(rand_hex 32)
  cat >"$dir/config.yaml" <<EOF
# Generated by bedrock install.sh at $(date '+%Y-%m-%d %H:%M:%S'). Edit as needed.
# 生产加固建议: HTTPS 反代、修改默认端口、按需关闭 auth.allow_register。

server:
  port: ${port}
  host: "0.0.0.0"

# driver: sqlite | postgres | mysql（改 driver 不搬迁数据，2.0 仅支持全新安装）
database:
  driver: sqlite
  path: "./data/bedrock.sqlite"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 1h

jwt:
  secret: "${jwt}"
  access_ttl: "2h"
  refresh_ttl: "168h"

build:
  max_concurrent: 3
  workspace_dir: "./data/workspaces"
  artifact_dir: "./data/artifacts"
  log_dir: "./data/logs"
  cache_dir: "./data/caches"

storage:
  root: "./data/storage"
  attachment_max_bytes: 20971520
  doc_import_max_bytes: 104857600

encryption:
  key: "${key}"

admin:
  username: "${user}"
  password: "${pass}"
  display_name: "管理员"

auth:
  allow_register: true

# DSH / opencode harness 默认关闭，装好对应 CLI 后再打开。
dsh:
  enabled: false
  bin: "dsh"
  home: "./data/dsh"
  workspace_root: "./data/dsh-workspaces"
  port: 17800
  startup_timeout: "60s"
  health_interval: "10s"
  auto_restart: true
  approval_mode: "manual"
  pending_ttl: "10m"
  session_idle_ttl: "72h"
  log_dir: ""
  max_sessions: 64

harness:
  enabled: false
  backend: "opencode"
  bin: "opencode"
  port: 4096
  approval_mode: "manual"
EOF
  chmod 600 "$dir/config.yaml"
}

install_server() {
  local dir port user pass tag
  if interactive; then
    dir=$(ask "安装目录" "${OPT_DIR:-$(default_dir server)}")
  else
    dir="${OPT_DIR:-$(default_dir server)}"
  fi
  port="${OPT_PORT:-$(interactive && ask "监听端口" 8080 || echo 8080)}"
  user="${OPT_ADMIN_USER:-$(interactive && ask "超级管理员用户名" admin || echo admin)}"
  pass="${OPT_ADMIN_PASS:-$(rand_hex 8)}"
  local generated_pass=0
  [ -z "$OPT_ADMIN_PASS" ] && generated_pass=1

  case $dir in
    *\ *) die "安装目录不能包含空格: $dir" ;;
  esac
  mkdir -p "$dir/data" "$dir/.update" "$dir/backups" \
    || die "无法创建目录 ${dir}（安装到系统路径请用 sudo 运行，或用 --dir 指定可写目录）"

  tag=$(resolve_tag)
  info "目标版本: ${tag}"

  if [ -f "$dir/bedrock" ]; then
    info "发现已有安装（$("$dir/bedrock" --version 2>/dev/null || echo unknown)），将保留为 bedrock.bak"
  fi

  download_verified "$asset_server" "$tag" "$dir/.update/bedrock-new"

  local new_config=0
  [ -f "$dir/config.yaml" ] || new_config=1
  if [ "$new_config" = "1" ]; then
    gen_server_config "$dir" "$port" "$user" "$pass"
  else
    warn "config.yaml 已存在，保留现有配置（不覆盖）"
  fi

  info "放置二进制..."
  [ -f "$dir/bedrock" ] && mv -f "$dir/bedrock" "$dir/bedrock.bak"
  mv -f "$dir/.update/bedrock-new" "$dir/bedrock"
  chmod +x "$dir/bedrock"

  svc_install server "$dir"
  svc_start server "$dir"

  info "等待服务就绪（最长 60s）..."
  local health_url="http://127.0.0.1:$(server_port "$dir")/api/v1/health"
  if ! wait_health "$health_url" "" 60; then
    err "健康检查失败，查看日志: $(svc_logfile server "$dir") 或 journalctl -u bedrock -n 50"
    return 1
  fi

  echo
  info "${C_G}Bedrock Server 安装成功${C_0}  版本: $("$dir/bedrock" --version 2>/dev/null || echo "$tag")"
  printf '  访问地址:     http://<本机IP>:%s\n' "$(server_port "$dir")"
  printf '  安装目录:     %s\n' "$dir"
  printf '  数据目录:     %s/data\n' "$dir"
  if [ "$new_config" = "1" ]; then
    printf '  超级管理员:   %s\n' "$user"
    if [ "$generated_pass" = "1" ]; then
      printf '  管理员密码:   %s（随机生成，仅本次显示，请妥善保存）\n' "$pass"
    fi
  else
    printf '  配置文件:     %s/config.yaml（沿用旧配置）\n' "$dir"
  fi
  if [ "$SYSTEMD" = "1" ]; then
    printf '  服务管理:     systemctl {status|restart|stop} bedrock\n'
  else
    printf '  服务管理:     重新运行本脚本 update / status；日志 %s\n' "$(svc_logfile server "$dir")"
  fi
}

install_agent() {
  local dir addr token tag
  if interactive; then
    dir=$(ask "安装目录" "${OPT_DIR:-$(default_dir agent)}")
  else
    dir="${OPT_DIR:-$(default_dir agent)}"
  fi
  addr="${OPT_ADDR:-$(interactive && ask "监听地址" ":9091" || echo ":9091")}"
  token="${OPT_TOKEN:-$(rand_hex 16)}"
  local generated_token=0
  [ -z "$OPT_TOKEN" ] && generated_token=1

  case $dir in
    *\ *) die "安装目录不能包含空格: $dir" ;;
  esac
  mkdir -p "$dir/.update" \
    || die "无法创建目录 ${dir}（安装到系统路径请用 sudo 运行，或用 --dir 指定可写目录）"

  tag=$(resolve_tag)
  info "目标版本: ${tag}"

  if [ -f "$dir/bedrock-agent" ]; then
    info "发现已有安装（$("$dir/bedrock-agent" --version 2>/dev/null || echo unknown)），将保留为 bedrock-agent.bak"
  fi

  download_verified "$asset_agent" "$tag" "$dir/.update/bedrock-agent-new"

  local new_config=0
  [ -f "$dir/bedrock-agent.yaml" ] || new_config=1
  if [ "$new_config" = "1" ]; then
    cat >"$dir/bedrock-agent.yaml" <<EOF
# Generated by bedrock install.sh at $(date '+%Y-%m-%d %H:%M:%S').
addr: "${addr}"
token: "${token}"
# tls_cert: "/path/to/cert.pem"
# tls_key: "/path/to/key.pem"
EOF
    chmod 600 "$dir/bedrock-agent.yaml"
  else
    warn "bedrock-agent.yaml 已存在，保留现有配置（不覆盖）"
  fi

  info "放置二进制..."
  [ -f "$dir/bedrock-agent" ] && mv -f "$dir/bedrock-agent" "$dir/bedrock-agent.bak"
  mv -f "$dir/.update/bedrock-agent-new" "$dir/bedrock-agent"
  chmod +x "$dir/bedrock-agent"

  svc_install agent "$dir"
  svc_start agent "$dir"

  info "等待服务就绪（最长 30s）..."
  if ! wait_health "http://127.0.0.1:$(agent_port "$dir")/healthz" "$(agent_token "$dir")" 30; then
    err "健康检查失败，查看日志: $(svc_logfile agent "$dir") 或 journalctl -u bedrock-agent -n 50"
    return 1
  fi

  echo
  info "${C_G}Deploy Agent 安装成功${C_0}  版本: $("$dir/bedrock-agent" --version 2>/dev/null || echo "$tag")"
  printf '  安装目录:     %s\n' "$dir"
  printf '  监听地址:     %s\n' "$addr"
  if [ "$new_config" = "1" ]; then
    printf '  认证 token:   %s\n' "$token"
    if [ "$generated_token" = "1" ]; then
      printf '                （随机生成，仅本次显示；请填入平台「资源管理 → 服务器」对应 Agent 的 token）\n'
    fi
  fi
  if [ "$SYSTEMD" = "1" ]; then
    printf '  服务管理:     systemctl {status|restart|stop} bedrock-agent\n'
  else
    printf '  服务管理:     重新运行本脚本 update / status；日志 %s\n' "$(svc_logfile agent "$dir")"
  fi
}

# -------------------------------------------------------------- update ----

backup_sqlite() { # backup_sqlite <dir> ; snapshot the sqlite file while stopped
  local dir=$1 db backup
  db=$(yaml_value "$dir/config.yaml" database path) || true
  [ -n "$db" ] || return 0
  # config paths are relative to the working dir (= install dir)
  case $db in /*) ;; *) db="$dir/$db" ;; esac
  [ -f "$db" ] || return 0
  mkdir -p "$dir/backups"
  backup="$dir/backups/bedrock-$(date '+%Y%m%d-%H%M%S').sqlite"
  cp -f "$db" "$backup"
  info "已备份 SQLite: $backup"
  ls -1t "$dir"/backups/bedrock-*.sqlite 2>/dev/null | tail -n +4 | xargs rm -f 2>/dev/null || true
}

update_component() { # update_component <server|agent> <dir> ; 0 = ok / skip
  local comp=$1 dir=$2 bin tag cur
  if [ "$comp" = "server" ]; then bin="$dir/bedrock"; else bin="$dir/bedrock-agent"; fi
  [ -x "$bin" ] || { info "$(svc_name "$comp"): 未安装（跳过）"; return 0; }

  cur=$("$bin" --version 2>/dev/null || echo unknown)
  tag=$(resolve_tag)
  info "$(svc_name "$comp"): 当前 ${cur} → 目标 ${tag}"
  if [ "$cur" = "$tag" ]; then
    info "$(svc_name "$comp"): 已是最新版本"
    return 0
  fi

  local asset
  asset=$([ "$comp" = "server" ] && printf '%s' "$asset_server" || printf '%s' "$asset_agent")
  info "下载新版本..."
  download_verified "$asset" "$tag" "$dir/.update/${asset}.new"

  info "优雅停机..."
  svc_stop "$comp" "$dir"
  if [ "$comp" = "server" ]; then backup_sqlite "$dir"; fi

  info "替换二进制..."
  mv -f "$bin" "$bin.bak"
  mv -f "$dir/.update/${asset}.new" "$bin"
  chmod +x "$bin"

  info "启动..."
  svc_start "$comp" "$dir"
  info "等待服务就绪（最长 60s）..."
  if ! component_health_wait "$comp" "$dir"; then
    err "$(svc_name "$comp"): 新版本健康检查失败，自动回滚到 ${cur}"
    svc_stop "$comp" "$dir"
    mv -f "$bin.bak" "$bin"
    svc_start "$comp" "$dir"
    if component_health_wait "$comp" "$dir"; then
      warn "已回滚到旧版本 ${cur} 并恢复运行；请将失败现象（$(svc_logfile "$comp" "$dir")）反馈至项目仓库"
    else
      err "回滚后启动仍失败，请手动检查: $bin / $bin.bak 与日志 $(svc_logfile "$comp" "$dir")"
    fi
    return 1
  fi
  info "$(svc_name "$comp") 更新成功: ${cur} → $("$bin" --version 2>/dev/null || echo "$tag")"
  return 0
}

component_health_wait() {
  if [ "$1" = "server" ]; then
    wait_health "http://127.0.0.1:$(server_port "$2")/api/v1/health" "" 60
  else
    wait_health "http://127.0.0.1:$(agent_port "$2")/healthz" "$(agent_token "$2")" 30
  fi
}

cmd_update() {
  local rc=0
  if [ -n "$OPT_DIR" ]; then
    local comp
    if [ -x "$OPT_DIR/bedrock" ]; then comp="server"
    elif [ -x "$OPT_DIR/bedrock-agent" ]; then comp="agent"
    else die "目录 $OPT_DIR 下未发现 bedrock / bedrock-agent 二进制"; fi
    update_component "$comp" "$OPT_DIR" || rc=1
  else
    local d
    d="$(default_dir server)"
    [ -x "$d/bedrock" ] && { update_component server "$d" || rc=1; }
    d="$(default_dir agent)"
    [ -x "$d/bedrock-agent" ] && { update_component agent "$d" || rc=1; }
    if [ "$rc" = "0" ] && [ ! -x "$(default_dir server)/bedrock" ] && [ ! -x "$(default_dir agent)/bedrock-agent" ]; then
      warn "默认目录下未发现已安装组件；自定义目录请用 --dir <DIR> 指定"
    fi
  fi
  return $rc
}

# -------------------------------------------------------------- status ----

cmd_status() {
  local comp dir ver active health
  if [ -n "$OPT_DIR" ]; then
    if [ -x "$OPT_DIR/bedrock" ]; then comp="server"; dir="$OPT_DIR"
    elif [ -x "$OPT_DIR/bedrock-agent" ]; then comp="agent"; dir="$OPT_DIR"
    else die "目录 $OPT_DIR 下未发现 bedrock / bedrock-agent 二进制"; fi
  fi
  for comp in ${comp:-server agent}; do
    dir="${OPT_DIR:-$(default_dir "$comp")}"
    printf '%s\n' "${C_B}=== Bedrock $(svc_name "$comp") ===${C_0}"
    local bin="$dir/$(svc_bin "$comp")"
    if [ ! -x "$bin" ]; then
      printf '  状态: 未安装\n'; continue
    fi
    ver=$("$bin" --version 2>/dev/null || echo unknown)
    if svc_is_active "$comp" "$dir"; then active="运行中"; else active="未运行"; fi
    if [ "$active" = "运行中" ]; then
      if component_health "$comp" "$dir"; then health="健康"; else health="异常（健康检查未通过）"; fi
    else
      health="-"
    fi
    printf '  目录:   %s\n  版本:   %s\n  服务:   %s\n  健康:   %s\n' "$dir" "$ver" "$active" "$health"
    [ "$SYSTEMD" = "0" ] && [ -f "$(svc_logfile "$comp" "$dir")" ] && printf '  日志:   %s\n' "$(svc_logfile "$comp" "$dir")"
  done
}

# ----------------------------------------------------------------- args ----

parse_args() {
  local pos=()
  while [ $# -gt 0 ]; do
    case $1 in
      server|agent|update|status) COMMAND=$1 ;;
      --mirror) OPT_MIRROR=${2:-}; shift ;;
      --mirror=*) OPT_MIRROR="${1#*=}" ;;
      --no-mirror) OPT_NO_MIRROR=1 ;;
      --dir) OPT_DIR=${2:-}; shift ;;
      --dir=*) OPT_DIR="${1#*=}" ;;
      --version) OPT_VERSION=${2:-}; shift ;;
      --version=*) OPT_VERSION="${1#*=}" ;;
      --port) OPT_PORT=${2:-}; shift ;;
      --port=*) OPT_PORT="${1#*=}" ;;
      --admin-user) OPT_ADMIN_USER=${2:-}; shift ;;
      --admin-user=*) OPT_ADMIN_USER="${1#*=}" ;;
      --admin-pass) OPT_ADMIN_PASS=${2:-}; shift ;;
      --admin-pass=*) OPT_ADMIN_PASS="${1#*=}" ;;
      --token) OPT_TOKEN=${2:-}; shift ;;
      --token=*) OPT_TOKEN="${1#*=}" ;;
      --addr) OPT_ADDR=${2:-}; shift ;;
      --addr=*) OPT_ADDR="${1#*=}" ;;
      --yes|-y) OPT_YES=1 ;;
      -h|--help) usage; exit 0 ;;
      *) die "未知参数: $1（--help 查看用法）" ;;
    esac
    shift
  done
  OPT_MIRROR=$(normalize_mirror "$OPT_MIRROR")
}

menu() {
  echo "${C_B}=== Bedrock 一键安装 ===${C_0}"
  printf '  [1] 安装 Bedrock Server（主体）\n'
  printf '  [2] 安装 Deploy Agent（代理分发工具）\n'
  printf '  [3] 更新已安装组件\n'
  printf '  [4] 查看状态\n'
  printf '  [q] 退出\n'
  local sel
  sel=$(ask_choice "请选择" '^[1-4qQ]$' 1)
  case $sel in
    1) install_server ;;
    2) install_agent ;;
    3) cmd_update || true ;;
    4) cmd_status ;;
    q|Q) exit 0 ;;
  esac
}

main() {
  parse_args "$@"
  require_cmd curl
  require_cmd awk

  if [ -z "$COMMAND" ]; then
    if ! interactive; then
      usage
      die "非交互模式（--yes）需要指定子命令: server | agent | update | status"
    fi
    detect_platform
    pick_mirror
    menu
    return 0
  fi

  case $COMMAND in
    status)
      detect_platform
      cmd_status
      ;;
    server)
      detect_platform
      pick_mirror
      install_server || exit 1
      ;;
    agent)
      detect_platform
      pick_mirror
      install_agent || exit 1
      ;;
    update)
      detect_platform
      pick_mirror
      cmd_update || exit 1
      ;;
  esac
}

# curl shows a progress bar on ttys, silences itself when piped
if [ -t 2 ]; then CURL_QUIET=""; else CURL_QUIET="-sS"; fi

main "$@"
