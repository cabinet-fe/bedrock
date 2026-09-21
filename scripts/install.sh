#!/usr/bin/env bash
# BEDROCK_ONE_LINE_INSTALLER
# Bedrock one-line installer + bedctl CLI: interactive setup of Bedrock
# Server (main) or Deploy Agent, with mainland-China GitHub mirror fallbacks
# and a download -> graceful stop -> replace -> restart update flow.
#
# The script installs itself as the `bedctl` command (install/update/status/
# start/stop/restart/logs/doctor/self-update) and remembers install dirs and
# the chosen download mirror.
#
# See .agents/docs/ops-handbook.md for the manual install path.
set -euo pipefail

SCRIPT_VERSION="1.1.1"

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
OPT_COMPONENT=""
OPT_LOGS_N=""
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
Bedrock 一键安装 / 更新脚本（安装后自动装为 bedctl 命令行工具）

用法:
  ./install.sh                     交互模式（推荐）
  ./install.sh server   [选项]     安装 Bedrock Server（主体）
  ./install.sh agent    [选项]     安装 Deploy Agent（代理分发工具）
  ./install.sh update   [选项]     更新已安装组件（下载 → 优雅停机 → 更新 → 重启）
  ./install.sh status              查看安装状态

bedctl 管理命令（安装完成即可用，无需再下载脚本）:
  bedctl install server|agent [选项]   安装（同 ./install.sh server|agent）
  bedctl update [server|agent]         更新已安装组件
  bedctl status                        查看安装状态
  bedctl start|stop|restart [server|agent]
                                       服务管理（缺省作用于全部已安装组件）
  bedctl logs [server|agent] [-n N]    最近日志（默认 100 行）
  bedctl doctor [server|agent]         体检: 服务/端口/监听地址/本机健康/防火墙
  bedctl self-update                   bedctl 自我升级（拉取仓库最新脚本）
  bedctl version                       查看 bedctl 版本

  bedctl 会记住安装目录与下载源（root: /etc/bedrock/bedctl.env，非 root: ~/.bedrock/bedctl.env），
  后续命令无需重复传 --dir / --mirror。

选项:
  --mirror <URL>     GitHub 镜像前缀，如 https://gh-proxy.com/
  --no-mirror        强制直连 GitHub
  --dir <DIR>        安装目录（默认 /opt/bedrock[-agent]，非 root 为 ~/bedrock[-agent]；已记住时用记住的目录）
  --version <TAG>    指定版本（默认最新 release，如 v2.0.0）
  --port <N>         Server 监听端口（默认 8080）
  --admin-user <U>   超级管理员用户名（默认 admin）
  --admin-pass <P>   超级管理员密码（默认随机生成并打印）
  --token <T>        Agent 认证 token（默认随机生成并打印，需填回平台「服务器」配置）
  --addr <A>         Agent 监听地址（默认 :9091）
  -n <N>             logs 输出行数（配合 bedctl logs）
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

# --------------------------------------------------------------- state ----
# bedctl 状态：记住各组件安装目录与下载源，供下次命令直接复用。

state_dir() { [ "$IS_ROOT" = "1" ] && printf '/etc/bedrock' || printf '%s/.bedrock' "$HOME"; }
state_file() { printf '%s/bedctl.env' "$(state_dir)"; }

state_set() { # state_set <KEY> <VALUE>
  local key=$1 val=$2 f tmp
  f=$(state_file)
  mkdir -p "$(state_dir)" 2>/dev/null || return 0
  tmp="$f.tmp.$$"
  touch "$f" 2>/dev/null || return 0
  grep -v "^${key}=" "$f" >"$tmp" 2>/dev/null || true
  printf '%s=%s\n' "$key" "$val" >>"$tmp"
  mv -f "$tmp" "$f"
}

state_get() { # state_get <KEY> -> stdout（未设置时为空）
  [ -f "$(state_file)" ] || return 0
  sed -n "s/^$1=//p" "$(state_file)" 2>/dev/null | head -1
  return 0
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
    # 直连与内置镜像都探测失败时，退回上次记录的下载源
    if [ -z "$MIRROR" ] && [ "$direct_ok" = "0" ]; then
      MIRROR=$(state_get MIRROR)
      [ -n "$MIRROR" ] && info "直连与内置镜像探测失败，使用上次记录的下载源: ${MIRROR}"
    fi
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
  if [ -n "$MIRROR" ]; then state_set MIRROR "$MIRROR"; fi
  return 0
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

detect_login_path() {
  # The service env must not depend on whoever launched the installer: capture
  # the PATH an interactive login shell would have, so user-level tools (bun,
  # cargo, mise...) whose rc entries sit below the non-interactive early return
  # (e.g. Ubuntu ~/.bashrc) stay visible to build scripts. stdin is detached so
  # the probe can never touch the caller's terminal; a killed interactive shell
  # would otherwise leave the tty in a broken state.
  local probe p
  for probe in "${SHELL:-/bin/bash}" /bin/bash; do
    command -v "$probe" >/dev/null 2>&1 || continue
    p=$(timeout 5 "$probe" -l -i -c 'echo $PATH' </dev/null 2>/dev/null | tail -n 1 || true)
    if [ -n "$p" ]; then
      printf '%s' "$p"
      return 0
    fi
  done
  printf '%s' "$PATH"
}

svc_install() { # svc_install <component> <dir>
  local comp=$1 dir=$2 name timeout lpath lhome env_lines
  name=$(svc_name "$comp")
  if [ "$SYSTEMD" = "1" ]; then
    if [ "$comp" = "server" ]; then timeout=45; else timeout=10; fi
    lpath=$(detect_login_path)
    lhome=${HOME:-}
    if [ -z "$lhome" ]; then
      lhome=$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f6 || true)
    fi
    env_lines=""
    if [ -n "$lhome" ]; then
      env_lines="Environment=\"HOME=${lhome}\""$'\n'
    fi
    env_lines+="Environment=\"PATH=${lpath}\""
    cat >"/etc/systemd/system/${name}.service" <<EOF
[Unit]
Description=Bedrock ${comp} (installed by install.sh)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${dir}
${env_lines}
ExecStart=$(svc_exec_args "$comp" "$dir")
Restart=on-failure
RestartSec=3
TimeoutStopSec=${timeout}

[Install]
WantedBy=multi-user.target
EOF
    # A wedged systemd must not hang the updater: bound every systemctl call.
    timeout 15 systemctl daemon-reload || warn "systemd daemon-reload 超时或失败，重启服务前请手动执行 systemctl daemon-reload"
    timeout 15 systemctl enable "${name}.service" >/dev/null 2>&1 || warn "设置开机自启失败: ${name}"
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
    # Login PATH for the same reason as svc_install; env passes it to nohup and the child.
    # shellcheck disable=SC2046
    env PATH="$(detect_login_path)" nohup $(svc_exec_args "$comp" "$dir") >>"$logfile" 2>&1 &
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
  if [ -z "$a" ]; then
    a=$(sed -n 's/^[[:space:]]*addr:[[:space:]]*"\{0,1\}\([^"[:space:]]*\).*/\1/p' "$1/bedrock-agent.yaml" 2>/dev/null | head -1) || true
  fi
  p=${a##*:}
  printf '%s' "${p:-9091}"
}

agent_token() { # agent_token <dir>
  sed -n 's/^[[:space:]]*token:[[:space:]]*"\{0,1\}\([^"]*\)"\{0,1\}[[:space:]]*$/\1/p' "$1/bedrock-agent.yaml" 2>/dev/null | head -1
  return 0
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

HEALTH_URL=""
HEALTH_BEARER=""
health_url_for() { # health_url_for <comp> <dir> ; 设置 HEALTH_URL / HEALTH_BEARER
  if [ "$1" = "server" ]; then
    HEALTH_BEARER=""
    HEALTH_URL="http://127.0.0.1:$(server_port "$2")/api/v1/health"
  else
    HEALTH_BEARER=$(agent_token "$2")
    HEALTH_URL="http://127.0.0.1:$(agent_port "$2")/healthz"
  fi
}

# --------------------------------------------- readiness & diagnostics ---

LAST_HTTP_CODE=""

port_busy() { # port_busy <port> ; 0 = 127.0.0.1:<port> 上有进程在监听
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

svc_dead() { # svc_dead <comp> <dir> ; 0 = 进程已退出或处于崩溃重启循环，不会再就绪
  local comp=$1 dir=$2 state sub pidfile pid
  if [ "$SYSTEMD" = "1" ]; then
    local unit
    unit="$(svc_name "$comp").service"
    state=$(systemctl show -p ActiveState --value "$unit" 2>/dev/null)
    sub=$(systemctl show -p SubState --value "$unit" 2>/dev/null)
    if [ "$state" = "failed" ] || [ "$state" = "inactive" ]; then return 0; fi
    # Restart=on-failure 的退避窗口（activating/auto-restart）说明进程在反复崩溃
    if [ "$state" = "activating" ] && [ "$sub" = "auto-restart" ]; then return 0; fi
    return 1
  fi
  pidfile=$(svc_pidfile "$comp" "$dir")
  pid=$(cat "$pidfile" 2>/dev/null || true)
  [ -n "$pid" ] || return 0
  ! kill -0 "$pid" 2>/dev/null
}

wait_service_ready() { # wait_service_ready <comp> <dir> <url> [bearer] [tries] ; ok -> 0
  local comp=$1 dir=$2 url=$3 bearer=${4:-} tries=${5:-60} i=0
  LAST_HTTP_CODE=""
  while [ "$i" -lt "$tries" ]; do
    if [ -n "$bearer" ]; then
      LAST_HTTP_CODE=$(curl -m 2 -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${bearer}" "$url" 2>/dev/null) || LAST_HTTP_CODE="000"
    else
      LAST_HTTP_CODE=$(curl -m 2 -s -o /dev/null -w '%{http_code}' "$url" 2>/dev/null) || LAST_HTTP_CODE="000"
    fi
    [ "$LAST_HTTP_CODE" = "200" ] && return 0
    # 进程已经退出（配置错误、端口冲突等）就没必要等满整个窗口
    if svc_dead "$comp" "$dir"; then return 1; fi
    sleep 1
    i=$((i + 1))
  done
  return 1
}

readiness_hint() { # 用 wait_service_ready 留下的 LAST_HTTP_CODE 解释失败原因
  case "${LAST_HTTP_CODE:-000}" in
    000) printf '端口无响应，服务可能启动即退出或未监听' ;;
    401|403) printf 'HTTP %s，健康检查鉴权不匹配（token/配置变更？）' "${LAST_HTTP_CODE}" ;;
    *) printf 'HTTP %s，端口有响应但不是预期的服务（可能被其他程序占用）' "${LAST_HTTP_CODE}" ;;
  esac
}

print_logs() { # print_logs <comp> <dir> [lines] ; 直接把最近日志打给用户看
  local comp=$1 dir=$2 n=${3:-30} name f
  name=$(svc_name "$comp")
  echo >&2
  if [ "$SYSTEMD" = "1" ]; then
    info "最近 ${n} 行日志（journalctl -u ${name}）:"
    journalctl -u "${name}.service" -n "$n" --no-pager --quiet 2>/dev/null \
      || warn "无法读取 journal，请手动执行: journalctl -u ${name} -n ${n} --no-pager"
  else
    f=$(svc_logfile "$comp" "$dir")
    if [ -f "$f" ]; then
      info "最近 ${n} 行日志（${f}）:"
      tail -n "$n" "$f"
    else
      warn "未找到日志文件: ${f}"
    fi
  fi
}

# ------------------------------------------------------------- install ----

default_dir() { # default_dir <server|agent> : 记住的目录 > 平台默认
  local comp=$1 suffix="" key val
  [ "$comp" = "agent" ] && suffix="-agent"
  case $comp in
    server) key=SERVER_DIR ;;
    agent) key=AGENT_DIR ;;
    *) return 1 ;;
  esac
  val=$(state_get "$key")
  if [ -n "$val" ]; then
    printf '%s' "$val"
    return 0
  fi
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

# --------------------------------------------------------------- bedctl ----

cli_path() { [ "$IS_ROOT" = "1" ] && printf '/usr/local/bin/bedctl' || printf '%s/.local/bin/bedctl' "$HOME"; }

download_cli_script() { # download_cli_script <dest> ; 0 = ok
  local dest=$1 base m
  # 以文件方式运行时直接复制自身；curl | bash 时 $0 不是文件，改从仓库下载
  if [ -f "$0" ] && grep -q "BEDROCK_ONE_LINE_INSTALLER" "$0" 2>/dev/null; then
    cp -f "$0" "$dest"
    return 0
  fi
  for m in "${MIRROR}" "" "${MIRRORS[@]}"; do
    if [ -z "$m" ]; then base=$RELEASE_BASE; else base="${m}${RELEASE_BASE}"; fi
    if curl -fsSL ${CURL_QUIET} --connect-timeout 8 -o "$dest" "${base}/raw/main/scripts/install.sh" 2>/dev/null \
      && grep -q "BEDROCK_ONE_LINE_INSTALLER" "$dest" 2>/dev/null; then
      return 0
    fi
  done
  rm -f "$dest"
  return 1
}

install_cli() { # install_cli ; 把本脚本装成 bedctl 命令并记录路径
  local dest tmp
  dest=$(cli_path)
  mkdir -p "$(dirname "$dest")" 2>/dev/null || { warn "无法创建 $(dirname "$dest")，跳过 bedctl 安装"; return 0; }
  tmp="${dest}.tmp.$$"
  if ! download_cli_script "$tmp"; then
    warn "bedctl 安装脚本获取失败，跳过（可稍后重新运行本安装流程）"
    return 0
  fi
  chmod +x "$tmp"
  mv -f "$tmp" "$dest"
  state_set CLI_PATH "$dest"
  info "已安装命令行工具: ${dest}"
  case ":$PATH:" in
    *":$(dirname "$dest"):"*) ;;
    *) warn "提示: $(dirname "$dest") 不在 PATH 中，请将其加入 PATH 后使用 bedctl" ;;
  esac
  return 0
}

install_server() {
  local dir port tag
  if interactive; then
    dir=$(ask "安装目录" "${OPT_DIR:-$(default_dir server)}")
  else
    dir="${OPT_DIR:-$(default_dir server)}"
  fi

  case $dir in
    *\ *) die "安装目录不能包含空格: $dir" ;;
  esac
  mkdir -p "$dir/data" "$dir/.update" "$dir/backups" \
    || die "无法创建目录 ${dir}（安装到系统路径请用 sudo 运行，或用 --dir 指定可写目录）"

  # 已有配置时端口/管理员以配置文件为准，不问也不覆盖
  local kept_config=0
  [ -f "$dir/config.yaml" ] && kept_config=1
  local user pass generated_pass=0
  if [ "$kept_config" = "1" ]; then
    local ignored=""
    [ -n "$OPT_PORT" ] && ignored="${ignored} --port"
    [ -n "$OPT_ADMIN_USER" ] && ignored="${ignored} --admin-user"
    [ -n "$OPT_ADMIN_PASS" ] && ignored="${ignored} --admin-pass"
    if [ -n "$ignored" ]; then
      warn "已有 config.yaml，以下选项不生效（脚本不覆盖现有配置）:${ignored}"
    fi
  else
    if interactive; then
      port=$(ask "监听端口" "${OPT_PORT:-8080}")
      user=$(ask "超级管理员用户名" "${OPT_ADMIN_USER:-admin}")
    else
      port="${OPT_PORT:-8080}"
      user="${OPT_ADMIN_USER:-admin}"
    fi
    pass="${OPT_ADMIN_PASS:-$(rand_hex 8)}"
    [ -z "$OPT_ADMIN_PASS" ] && generated_pass=1
  fi

  tag=$(resolve_tag)
  info "目标版本: ${tag}"

  if [ -f "$dir/bedrock" ]; then
    info "发现已有安装（$("$dir/bedrock" --version 2>/dev/null || echo unknown)），将保留为 bedrock.bak"
  fi

  download_verified "$asset_server" "$tag" "$dir/.update/bedrock-new"

  if [ "$kept_config" = "1" ]; then
    warn "config.yaml 已存在，保留现有配置（不覆盖）"
  else
    gen_server_config "$dir" "$port" "$user" "$pass"
  fi

  # 生效端口：新安装用请求端口；沿用配置时从配置解析，解析失败才回退默认值并明说
  local eff_port
  if [ "$kept_config" = "1" ]; then
    eff_port=$(yaml_value "$dir/config.yaml" server port || true)
    if [ -n "$eff_port" ]; then
      info "监听端口（来自现有配置）: ${eff_port}"
    else
      eff_port=8080
      warn "未能从现有 config.yaml 解析 server.port，健康检查按默认端口 ${eff_port} 进行"
    fi
    local cfg_host
    cfg_host=$(yaml_value "$dir/config.yaml" server host || true)
    case $cfg_host in
      127.0.0.1|localhost|::1)
        warn "现有配置 server.host=${cfg_host} 仅监听本机回环，外部将无法访问（改为 0.0.0.0 可对外开放）"
        ;;
    esac
  else
    eff_port="$port"
  fi

  svc_stop server "$dir"
  if port_busy "$eff_port"; then
    err "端口 ${eff_port} 仍被占用：可能存在不受服务管理器管理的旧进程"
    err "请用 ss -ltnp 'sport = :${eff_port}' 找到占用进程并处理后，重新运行本脚本"
    return 1
  fi

  info "放置二进制..."
  [ -f "$dir/bedrock" ] && mv -f "$dir/bedrock" "$dir/bedrock.bak"
  mv -f "$dir/.update/bedrock-new" "$dir/bedrock"
  chmod +x "$dir/bedrock"

  svc_install server "$dir"
  svc_start server "$dir"

  local health_url="http://127.0.0.1:${eff_port}/api/v1/health"
  info "等待服务就绪（最长 60s，进程退出会提前报告）..."
  if ! wait_service_ready server "$dir" "$health_url" "" 60; then
    err "健康检查未通过：$(readiness_hint)"
    print_logs server "$dir"
    svc_stop server "$dir" || true
    if [ -f "$dir/bedrock.bak" ]; then
      warn "自动回滚到旧版本..."
      mv -f "$dir/bedrock.bak" "$dir/bedrock"
      svc_start server "$dir"
      if wait_service_ready server "$dir" "$health_url" "" 30; then
        warn "已回滚到旧版本并恢复运行；失败原因见上方日志，修复后重新运行本脚本即可"
      else
        err "回滚后仍无法启动，请手动检查 ${dir}/bedrock 与上方日志"
      fi
    else
      err "首次安装无可回滚的旧版本。常见原因：config.yaml 缺少必填项（jwt.secret / encryption.key）、数据库初始化失败、端口冲突；修复后重新运行本脚本"
    fi
    return 1
  fi

  echo
  info "${C_G}Bedrock Server 安装成功${C_0}  版本: $("$dir/bedrock" --version 2>/dev/null || echo "$tag")"
  printf '  访问地址:     http://<本机IP>:%s\n' "$eff_port"
  printf '  安装目录:     %s\n' "$dir"
  printf '  数据目录:     %s/data\n' "$dir"
  if [ "$kept_config" = "1" ]; then
    printf '  配置文件:     %s/config.yaml（沿用旧配置）\n' "$dir"
  else
    printf '  超级管理员:   %s\n' "$user"
    if [ "$generated_pass" = "1" ]; then
      printf '  管理员密码:   %s（随机生成，仅本次显示，请妥善保存）\n' "$pass"
    fi
  fi
  if [ "$SYSTEMD" = "1" ]; then
    printf '  服务管理:     systemctl {status|restart|stop} bedrock 或 bedctl {start|stop|restart} server\n'
  else
    printf '  服务管理:     bedctl {start|stop|restart} server；日志 bedctl logs server\n'
  fi
  printf '  故障排查:     bedctl doctor server（外部访问不通时先跑这个）\n'
  state_set SERVER_DIR "$dir"
  install_cli
}

install_agent() {
  local dir addr token tag
  if interactive; then
    dir=$(ask "安装目录" "${OPT_DIR:-$(default_dir agent)}")
  else
    dir="${OPT_DIR:-$(default_dir agent)}"
  fi

  case $dir in
    *\ *) die "安装目录不能包含空格: $dir" ;;
  esac
  mkdir -p "$dir/.update" \
    || die "无法创建目录 ${dir}（安装到系统路径请用 sudo 运行，或用 --dir 指定可写目录）"

  local kept_config=0
  [ -f "$dir/bedrock-agent.yaml" ] && kept_config=1
  local generated_token=0
  if [ "$kept_config" = "1" ]; then
    local ignored=""
    [ -n "$OPT_ADDR" ] && ignored="${ignored} --addr"
    [ -n "$OPT_TOKEN" ] && ignored="${ignored} --token"
    if [ -n "$ignored" ]; then
      warn "已有 bedrock-agent.yaml，以下选项不生效（脚本不覆盖现有配置）:${ignored}"
    fi
  else
    if interactive; then
      addr=$(ask "监听地址" "${OPT_ADDR:-:9091}")
    else
      addr="${OPT_ADDR:-:9091}"
    fi
    token="${OPT_TOKEN:-$(rand_hex 16)}"
    [ -z "$OPT_TOKEN" ] && generated_token=1
  fi

  tag=$(resolve_tag)
  info "目标版本: ${tag}"

  if [ -f "$dir/bedrock-agent" ]; then
    info "发现已有安装（$("$dir/bedrock-agent" --version 2>/dev/null || echo unknown)），将保留为 bedrock-agent.bak"
  fi

  download_verified "$asset_agent" "$tag" "$dir/.update/bedrock-agent-new"

  if [ "$kept_config" = "1" ]; then
    warn "bedrock-agent.yaml 已存在，保留现有配置（不覆盖）"
  else
    cat >"$dir/bedrock-agent.yaml" <<EOF
# Generated by bedrock install.sh at $(date '+%Y-%m-%d %H:%M:%S').
addr: "${addr}"
token: "${token}"
# tls_cert: "/path/to/cert.pem"
# tls_key: "/path/to/key.pem"
EOF
    chmod 600 "$dir/bedrock-agent.yaml"
  fi

  local eff_port shown_addr
  if [ "$kept_config" = "1" ]; then
    eff_port=$(agent_port "$dir")
    shown_addr=$(sed -n 's/^[[:space:]]*addr:[[:space:]]*"\{0,1\}\([^"[:space:]]*\)"\{0,1\}.*/\1/p' "$dir/bedrock-agent.yaml" 2>/dev/null | head -1)
    [ -n "$shown_addr" ] || shown_addr=":${eff_port}"
    info "监听地址（来自现有配置）: ${shown_addr}"
    grep -q '^[[:space:]]*addr:' "$dir/bedrock-agent.yaml" 2>/dev/null \
      || warn "未能从现有 bedrock-agent.yaml 解析 addr，健康检查按默认端口 ${eff_port} 进行"
  else
    eff_port=${addr##*:}
    shown_addr="$addr"
  fi

  svc_stop agent "$dir"
  if port_busy "$eff_port"; then
    err "端口 ${eff_port} 仍被占用：可能存在不受服务管理器管理的旧进程"
    err "请用 ss -ltnp 'sport = :${eff_port}' 找到占用进程并处理后，重新运行本脚本"
    return 1
  fi

  info "放置二进制..."
  [ -f "$dir/bedrock-agent" ] && mv -f "$dir/bedrock-agent" "$dir/bedrock-agent.bak"
  mv -f "$dir/.update/bedrock-agent-new" "$dir/bedrock-agent"
  chmod +x "$dir/bedrock-agent"

  svc_install agent "$dir"
  svc_start agent "$dir"

  local health_url="http://127.0.0.1:${eff_port}/healthz"
  info "等待服务就绪（最长 30s，进程退出会提前报告）..."
  if ! wait_service_ready agent "$dir" "$health_url" "$(agent_token "$dir")" 30; then
    err "健康检查未通过：$(readiness_hint)"
    print_logs agent "$dir"
    svc_stop agent "$dir" || true
    if [ -f "$dir/bedrock-agent.bak" ]; then
      warn "自动回滚到旧版本..."
      mv -f "$dir/bedrock-agent.bak" "$dir/bedrock-agent"
      svc_start agent "$dir"
      if wait_service_ready agent "$dir" "$health_url" "$(agent_token "$dir")" 15; then
        warn "已回滚到旧版本并恢复运行；失败原因见上方日志，修复后重新运行本脚本即可"
      else
        err "回滚后仍无法启动，请手动检查 ${dir}/bedrock-agent 与上方日志"
      fi
    else
      err "首次安装无可回滚的旧版本。常见原因：bedrock-agent.yaml 缺少 token、端口冲突；修复后重新运行本脚本"
    fi
    return 1
  fi

  echo
  info "${C_G}Deploy Agent 安装成功${C_0}  版本: $("$dir/bedrock-agent" --version 2>/dev/null || echo "$tag")"
  printf '  安装目录:     %s\n' "$dir"
  printf '  监听地址:     %s\n' "$shown_addr"
  if [ "$kept_config" = "0" ]; then
    printf '  认证 token:   %s\n' "$token"
    if [ "$generated_token" = "1" ]; then
      printf '                （随机生成，仅本次显示；请填入平台「资源管理 → 服务器」对应 Agent 的 token）\n'
    fi
  fi
  if [ "$SYSTEMD" = "1" ]; then
    printf '  服务管理:     systemctl {status|restart|stop} bedrock-agent 或 bedctl {start|stop|restart} agent\n'
  else
    printf '  服务管理:     bedctl {start|stop|restart} agent；日志 bedctl logs agent\n'
  fi
  state_set AGENT_DIR "$dir"
  install_cli
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

  # Refresh the service unit on every update so the service env (login PATH /
  # HOME) follows the installed installer version, not the first-ever install.
  svc_install "$comp" "$dir"
  info "启动..."
  svc_start "$comp" "$dir"
  health_url_for "$comp" "$dir"
  local url="$HEALTH_URL" bearer="$HEALTH_BEARER" tries
  if [ "$comp" = "server" ]; then tries=60; else tries=30; fi
  info "等待服务就绪（最长 ${tries}s，进程退出会提前报告）..."
  if ! wait_service_ready "$comp" "$dir" "$url" "$bearer" "$tries"; then
    err "$(svc_name "$comp"): 新版本健康检查失败（$(readiness_hint)），自动回滚到 ${cur}"
    print_logs "$comp" "$dir"
    svc_stop "$comp" "$dir"
    mv -f "$bin.bak" "$bin"
    svc_start "$comp" "$dir"
    if wait_service_ready "$comp" "$dir" "$url" "$bearer" "$tries"; then
      warn "已回滚到旧版本 ${cur} 并恢复运行；失败原因见上方日志，请反馈至项目仓库"
    else
      err "回滚后启动仍失败，请手动检查: $bin / $bin.bak"
      print_logs "$comp" "$dir"
    fi
    return 1
  fi
  info "$(svc_name "$comp") 更新成功: ${cur} → $("$bin" --version 2>/dev/null || echo "$tag")"
  return 0
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

# -------------------------------------------------------------- manage ----

pick_component() { # pick_component [server|agent] -> stdout: server|agent|both|空
  local comp=${1:-} s a has_s=0 has_a=0
  case $comp in
    server|agent) printf '%s' "$comp"; return 0 ;;
  esac
  s=$(default_dir server)
  a=$(default_dir agent)
  [ -x "$s/bedrock" ] && has_s=1
  [ -x "$a/bedrock-agent" ] && has_a=1
  if [ "$has_s" = "1" ] && [ "$has_a" = "1" ]; then printf 'both'
  elif [ "$has_s" = "1" ]; then printf 'server'
  elif [ "$has_a" = "1" ]; then printf 'agent'
  fi
  return 0
}

cmd_svc() { # cmd_svc <start|stop|restart> [OPT_COMPONENT]
  local action=$1 comp comps dir bin listed
  listed=$(pick_component "${OPT_COMPONENT:-}")
  [ -n "$listed" ] || die "未发现已安装组件（bedctl status 查看，或用参数 server|agent 指定）"
  case $listed in
    both) comps="server agent" ;;
    *) comps="$listed" ;;
  esac
  for comp in $comps; do
    dir=$(default_dir "$comp")
    bin="$dir/$(svc_bin "$comp")"
    if [ ! -x "$bin" ]; then
      warn "$(svc_name "$comp"): 未安装（$bin 不存在），跳过"
      continue
    fi
    case $action in
      stop)
        svc_stop "$comp" "$dir"
        ;;
      start)
        if svc_is_active "$comp" "$dir"; then
          info "$(svc_name "$comp"): 已在运行"
        else
          svc_start "$comp" "$dir"
          health_url_for "$comp" "$dir"
          if wait_service_ready "$comp" "$dir" "$HEALTH_URL" "$HEALTH_BEARER" 15; then
            info "$(svc_name "$comp"): 已启动（健康检查通过）"
          else
            warn "$(svc_name "$comp"): 已执行启动但未就绪，运行 bedctl logs $comp 查看日志"
          fi
        fi
        ;;
      restart)
        svc_stop "$comp" "$dir"
        svc_start "$comp" "$dir"
        health_url_for "$comp" "$dir"
        if wait_service_ready "$comp" "$dir" "$HEALTH_URL" "$HEALTH_BEARER" 15; then
          info "$(svc_name "$comp"): 已重启（健康检查通过）"
        else
          warn "$(svc_name "$comp"): 已执行重启但未就绪，运行 bedctl logs $comp 查看日志"
        fi
        ;;
    esac
  done
}

cmd_logs() { # cmd_logs [-n N] [OPT_COMPONENT]
  local comp listed dir
  listed=$(pick_component "${OPT_COMPONENT:-}")
  [ -n "$listed" ] || die "未发现已安装组件（bedctl logs server|agent）"
  comp=$listed
  [ "$comp" = "both" ] && comp=server
  dir=$(default_dir "$comp")
  print_logs "$comp" "$dir" "${OPT_LOGS_N:-100}"
}

cmd_doctor() { # cmd_doctor [OPT_COMPONENT] ; 0 = 全部正常
  local comps=() comp dir bin issues rc=0 port url host addr
  if [ -n "${OPT_COMPONENT:-}" ]; then
    comps=("$OPT_COMPONENT")
  else
    case $(pick_component "") in
      both) comps=(server agent) ;;
      server) comps=(server) ;;
      agent) comps=(agent) ;;
      *) die "未发现已安装组件（bedctl doctor server|agent）" ;;
    esac
  fi
  for comp in "${comps[@]}"; do
    echo
    info "诊断 Bedrock $(svc_name "$comp")"
    dir=$(default_dir "$comp")
    bin="$dir/$(svc_bin "$comp")"
    issues=0
    if [ ! -x "$bin" ]; then
      err "  未安装: ${bin} 不存在"
      rc=1
      continue
    fi
    printf '  安装目录: %s\n' "$dir"
    printf '  版本:     %s\n' "$("$bin" --version 2>/dev/null || echo unknown)"

    if svc_is_active "$comp" "$dir"; then
      printf '  服务:     运行中\n'
    else
      printf '  %s服务:     未运行（bedctl start %s 启动）%s\n' "$C_Y" "$comp" "$C_0"
      issues=1
    fi

    if [ "$comp" = "server" ]; then
      port=$(yaml_value "$dir/config.yaml" server port || true)
      if [ -n "$port" ]; then
        printf '  配置端口: %s\n' "$port"
      else
        port=8080
        printf '  %s配置端口: 未能解析 config.yaml 的 server.port，按 8080 检查%s\n' "$C_Y" "$C_0"
      fi
      host=$(yaml_value "$dir/config.yaml" server host || true)
      [ -n "$host" ] || host="0.0.0.0"
      case $host in
        127.0.0.1|localhost|::1)
          printf '  %s监听地址: %s —— 仅本机可访问，外部连不上优先改这里（改为 0.0.0.0 后 bedctl restart server）%s\n' "$C_Y" "$host" "$C_0"
          issues=1
          ;;
        *) printf '  监听地址: %s（对所有网卡开放）\n' "$host" ;;
      esac
    else
      port=$(agent_port "$dir")
      addr=$(sed -n 's/^[[:space:]]*addr:[[:space:]]*"\{0,1\}\([^"[:space:]]*\)"\{0,1\}.*/\1/p' "$dir/bedrock-agent.yaml" 2>/dev/null | head -1)
      [ -n "$addr" ] || addr=":${port}"
      case ${addr%%:*} in
        127.0.0.1|localhost|::1)
          printf '  %s监听地址: %s —— 仅本机可访问，外部连不上优先改这里（如 :9091）%s\n' "$C_Y" "$addr" "$C_0"
          issues=1
          ;;
        *) printf '  监听地址: %s\n' "$addr" ;;
      esac
    fi

    if port_busy "$port"; then
      printf '  端口:     %s 有进程监听\n' "$port"
    else
      printf '  %s端口:     %s 无监听 —— 服务大概率没起来，运行 bedctl logs %s 查看原因%s\n' "$C_Y" "$port" "$comp" "$C_0"
      issues=1
    fi

    if svc_is_active "$comp" "$dir"; then
      health_url_for "$comp" "$dir"
      LAST_HTTP_CODE=""
      if wait_service_ready "$comp" "$dir" "$HEALTH_URL" "$HEALTH_BEARER" 1; then
        printf '  本机健康: 通过（%s）\n' "$HEALTH_URL"
      else
        printf '  %s本机健康: 未通过（%s）—— 运行 bedctl logs %s 查看日志%s\n' "$C_Y" "$(readiness_hint)" "$comp" "$C_0"
        issues=1
      fi
    fi

    if [ "$SYSTEMD" = "1" ]; then
      if systemctl is-enabled --quiet "$(svc_name "$comp").service" 2>/dev/null; then
        printf '  开机自启: 已启用\n'
      else
        printf '  %s开机自启: 未启用（systemctl enable %s）%s\n' "$C_Y" "$(svc_name "$comp")" "$C_0"
      fi
    fi

    # 本机正常但外部连不上：按层给出排查清单（能自动查的自动查）
    if [ "$issues" = "0" ]; then
      printf '  %s本机一切正常。外部仍连不上时按序检查：%s\n' "$C_B" "$C_0"
      printf '    1. 云安全组: 入方向放行 TCP %s（源 0.0.0.0/0）\n' "$port"
      if [ "$IS_ROOT" = "1" ]; then
        if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd -q --state 2>/dev/null; then
          if firewall-cmd --list-ports 2>/dev/null | tr ' ' '\n' | grep -qx "${port}/tcp"; then
            printf '    2. firewalld: 已放行 %s/tcp\n' "$port"
          else
            printf '    %s2. firewalld 运行中且未放行 %s/tcp，执行: firewall-cmd --permanent --add-port=%s/tcp && firewall-cmd --reload%s\n' "$C_Y" "$port" "$port" "$C_0"
          fi
        elif command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
          if ufw status 2>/dev/null | grep -qE "(^|[[:space:]])${port}/tcp"; then
            printf '    2. ufw: 已放行 %s/tcp\n' "$port"
          else
            printf '    %s2. ufw 运行中且未放行 %s/tcp，执行: ufw allow %s/tcp%s\n' "$C_Y" "$port" "$port" "$C_0"
          fi
        else
          printf '    2. 宿主机防火墙: 未检测到 firewalld/ufw，如有 iptables/nftables 规则请自查 %s/tcp\n' "$port"
        fi
      else
        printf '    2. 宿主机防火墙: firewalld/ufw 是否放行 %s/tcp（root 运行 bedctl doctor 会自动检查）\n' "$port"
      fi
      printf '    3. 用公网 IP/EIP 访问（不是内网 IP），并确认 EIP 已绑定该实例\n'
      printf '    4. 配了 Nginx 等反代/HTTPS 时，检查反代监听端口与 upstream 转发\n'
      printf '    5. 外部机器上执行 curl -v http://<公网IP>:%s/ —— 超时=安全组/防火墙，连接拒绝=服务未监听\n' "$port"
    fi
    [ "$issues" = "1" ] && rc=1
  done
  return $rc
}

cmd_self_update() { # cmd_self_update ; 从仓库拉最新 install.sh 替换 bedctl 自身
  local dest tmp ok=1 base m newver
  dest=$(state_get CLI_PATH)
  if [ -z "$dest" ] || [ ! -e "$dest" ]; then dest=$(cli_path); fi
  mkdir -p "$(dirname "$dest")" 2>/dev/null || die "无法创建 $(dirname "$dest")"
  tmp="${dest}.tmp.$$"
  for m in "${MIRROR}" "" "${MIRRORS[@]}"; do
    if [ -z "$m" ]; then base=$RELEASE_BASE; else base="${m}${RELEASE_BASE}"; fi
    if curl -fsSL ${CURL_QUIET} --connect-timeout 8 -o "$tmp" "${base}/raw/main/scripts/install.sh" 2>/dev/null \
      && grep -q "BEDROCK_ONE_LINE_INSTALLER" "$tmp" 2>/dev/null; then
      ok=0
      break
    fi
  done
  [ "$ok" = "0" ] || { rm -f "$tmp"; die "bedctl 自更新失败：无法下载 scripts/install.sh（可 --mirror 指定镜像重试）"; }
  bash -n "$tmp" 2>/dev/null || { rm -f "$tmp"; die "下载的脚本语法校验失败，放弃替换"; }
  chmod +x "$tmp"
  mv -f "$tmp" "$dest"
  newver=$(sed -n 's/^SCRIPT_VERSION="\(.*\)"$/\1/p' "$dest" | head -1)
  info "bedctl 已更新到 ${newver:-最新版}（${dest}）"
}

# ----------------------------------------------------------------- args ----

parse_args() {
  local -a pos=()
  while [ $# -gt 0 ]; do
    case $1 in
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
      -n) OPT_LOGS_N=${2:-}; shift ;;
      -h|--help) usage; exit 0 ;;
      *) pos+=("$1") ;;
    esac
    shift
  done
  if [ "${#pos[@]}" -gt 2 ]; then
    die "多余参数: ${pos[2]}（--help 查看用法）"
  fi
  if [ "${#pos[@]}" -ge 1 ]; then
    case ${pos[0]} in
      server|agent|install|update|status|start|stop|restart|logs|doctor|self-update|version) COMMAND=${pos[0]} ;;
      *) die "未知命令: ${pos[0]}（--help 查看用法）" ;;
    esac
  fi
  if [ "${#pos[@]}" = "2" ]; then
    case ${pos[1]} in
      server|agent) OPT_COMPONENT=${pos[1]} ;;
      *) die "未知组件: ${pos[1]}（可选 server|agent）" ;;
    esac
    case $COMMAND in
      server|agent) die "命令 ${COMMAND} 后不需要再指定组件" ;;
    esac
  fi
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
      die "非交互模式（--yes）需要指定子命令: install | server | agent | update | status | start | stop | restart | logs | doctor | self-update | version"
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
    start|stop|restart)
      cmd_svc "$COMMAND"
      ;;
    logs)
      cmd_logs
      ;;
    doctor)
      cmd_doctor
      ;;
    self-update)
      cmd_self_update
      ;;
    version)
      printf 'bedctl %s\n' "$SCRIPT_VERSION"
      ;;
    install)
      detect_platform
      pick_mirror
      case $OPT_COMPONENT in
        server) install_server || exit 1 ;;
        agent) install_agent || exit 1 ;;
        *)
          if interactive; then
            menu
          else
            usage
            die "非交互模式需要: install server|agent"
          fi
          ;;
      esac
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
