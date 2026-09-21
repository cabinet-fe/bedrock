#!/usr/bin/env bash
# BEDROCK_ONE_LINE_INSTALLER
# Bedrock bootstrap installer: downloads the Go `bedctl` binary from
# releases, installs it as `bedctl`, and hands over to it with all
# arguments intact. All install/update/service logic lives in bedctl
# itself; this script only bootstraps it.
#
# The marker comment above is load-bearing: bedctl (shell edition, v1.x)
# verifies downloaded self-update payloads by grepping for it, which is how
# old installs migrate onto the Go bedctl via `bedctl self-update`.
#
# Environment:
#   BEDROCK_RELEASE_BASE   override download base (default GitHub, for tests)
#   BEDROCK_MIRROR         mirror prefix, e.g. https://gh-proxy.com/
#   BEDROCK_OS / BEDROCK_ARCH  override platform detection
set -euo pipefail

REPO="cabinet-fe/bedrock"
RELEASE_BASE="${BEDROCK_RELEASE_BASE:-https://github.com/${REPO}}"
MIRRORS=("https://gh-proxy.com/" "https://ghfast.top/")

info() { printf '%s\n' "==> $*"; }
warn() { printf '%s\n' "警告: $*" >&2; }
die()  { printf '%s\n' "错误: $*" >&2; exit 1; }

# normalize_mirror canonicalizes a mirror prefix with the same rules as
# bedctl: whitespace trimmed, https:// added when the scheme is missing,
# trailing slash appended; empty stays empty.
normalize_mirror() {
  local m="${1-}"
  m="${m#"${m%%[![:space:]]*}"}"
  m="${m%"${m##*[![:space:]]}"}"
  [ -n "$m" ] || return 0
  case "$m" in http://*|https://*) ;; *) m="https://$m" ;; esac
  case "$m" in */) ;; *) m="$m/" ;; esac
  printf '%s' "$m"
}

command -v curl >/dev/null 2>&1 || die "缺少依赖命令: curl（请先安装）"

BEDROCK_MIRROR="$(normalize_mirror "${BEDROCK_MIRROR:-}")"

# --- platform --------------------------------------------------------------
OS="${BEDROCK_OS:-$(uname -s | tr '[:upper:]' '[:lower:]')}"
case "$(uname -m)" in
  x86_64|amd64) HOST_ARCH=amd64 ;;
  aarch64|arm64) HOST_ARCH=arm64 ;;
  *) HOST_ARCH="$(uname -m)" ;;
esac
ARCH="${BEDROCK_ARCH:-$HOST_ARCH}"
[ "$OS" = "linux" ] || die "bedctl 面向 Linux 部署（当前: ${OS}）。macOS/Windows 请参考 README 从源码构建或手动下载发布包"
case $ARCH in amd64|arm64) ;; *) die "不支持的架构: ${ARCH}（发布包仅提供 linux amd64/arm64）" ;; esac
SUFFIX="linux-${ARCH}"
ASSET="bedctl-${SUFFIX}"

# --- install destination -----------------------------------------------------
IS_ROOT=0
[ "$(id -u)" = "0" ] && IS_ROOT=1
if [ "$IS_ROOT" = "1" ]; then
  DEST="/usr/local/bin/bedctl"
  STATE_FILE="/etc/bedrock/bedctl.env"
else
  DEST="${HOME:?HOME 未设置}/.local/bin/bedctl"
  STATE_FILE="${HOME}/.bedrock/bedctl.env"
fi

state_set() { # state_set <KEY> <VALUE> — same file format as bedctl itself
  mkdir -p "$(dirname "$STATE_FILE")" 2>/dev/null || return 0
  { grep -v "^$1=" "$STATE_FILE" 2>/dev/null || true; printf '%s=%s\n' "$1" "$2"; } >"$STATE_FILE.tmp"
  mv -f "$STATE_FILE.tmp" "$STATE_FILE"
}

# --- resolve newest release tag ----------------------------------------------
latest_tag() {
  local t loc base m
  t=$(curl -m 10 -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1) || true
  [ -n "$t" ] && { printf '%s' "$t"; return 0; }
  # Fallback: releases/latest 302 Location ends in /releases/tag/<tag>
  # (plain GitHub and some mirrors; HTML-rendering mirrors are skipped).
  for m in "" "${MIRRORS[@]}"; do
    base="$RELEASE_BASE"
    [ -n "$m" ] && base="${m}${RELEASE_BASE}"
    loc=$(curl -m 8 -fsSI "$base/releases/latest" 2>/dev/null | tr -d '\r' | sed -n 's/^[Ll]ocation: *//p' | head -1) || true
    t=$(printf '%s' "$loc" | sed -n 's#.*/releases/tag/\([^/?#]*\).*#\1#p')
    [ -n "$t" ] && { printf '%s' "$t"; return 0; }
  done
  return 1
}

# --- download with mirror fallback ---------------------------------------------
# fetch <repo-path> <dest>; on success the working mirror is remembered and
# exported so the child bedctl reuses it instead of re-probing.
WORKING_MIRROR=""
fetch() {
  local url=$1 dest=$2 m base try
  local -a candidates=()
  [ -n "${BEDROCK_MIRROR:-}" ] && candidates+=("${BEDROCK_MIRROR}")
  candidates+=("" "${MIRRORS[@]}")
  for m in "${candidates[@]}"; do
    base="$RELEASE_BASE"
    [ -n "$m" ] && base="${m}${RELEASE_BASE}"
    try="${base}${url}"
    info "下载 $try"
    if curl -fsSL --connect-timeout 8 -o "$dest" -- "$try" 2>/dev/null; then
      WORKING_MIRROR="$m"
      return 0
    fi
    rm -f "$dest"
  done
  return 1
}

sha256_sum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

TAG="$(latest_tag)" || die "无法获取最新版本号。请检查网络后重试，或手动从 https://github.com/${REPO}/releases 下载 bedctl-${SUFFIX}"
info "最新版本: ${TAG}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fetch "/releases/download/${TAG}/${ASSET}" "${TMP}/bedctl" \
  || die "下载失败: ${ASSET}（可设置 BEDROCK_MIRROR=https://gh-proxy.com/ 后重试）"

if fetch "/releases/download/${TAG}/bedrock-${SUFFIX}.sha256" "${TMP}/sums"; then
  want=$(awk -v f="${ASSET}" '{n=$2; sub(/^\*/,"",n); if (n==f) print $1}' "${TMP}/sums" | head -1)
  if [ -n "$want" ]; then
    got=$(sha256_sum "${TMP}/bedctl")
    [ "$want" = "$got" ] || die "SHA256 校验失败: ${ASSET}
  期望: $want
  实际: $got"
  else
    warn "校验文件中没有 ${ASSET} 的记录，跳过 SHA256 校验"
  fi
else
  warn "校验文件下载失败，跳过 SHA256 校验"
fi

# --- install & hand over --------------------------------------------------------
mkdir -p "$(dirname "$DEST")" 2>/dev/null || die "无法创建 $(dirname "$DEST")"
chmod +x "${TMP}/bedctl"
mv -f "${TMP}/bedctl" "$DEST"
state_set CLI_PATH "$DEST"
info "已安装 bedctl: ${DEST}"
if [ -n "$WORKING_MIRROR" ]; then
  export BEDROCK_MIRROR="$WORKING_MIRROR"
fi

DEST_DIR="$(dirname "$DEST")"
case ":$PATH:" in
  *":$DEST_DIR:"*) ;;
  *) warn "提示: $DEST_DIR 不在 PATH 中，已直接使用完整路径执行" ;;
esac

exec "$DEST" "$@"
