#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
  cat <<'USAGE'
用法：
  ./test/start.sh --build
  ./test/start.sh

--build     安装前端依赖、构建前端和 Go 二进制，然后启动。
--help      显示帮助。

不带 --build 时直接运行已有二进制，不需要 Go、Node.js 或 Docker。
环境变量：
  DATABASE_URL     默认：已配置的本地 household_bills 测试库，可通过环境变量覆盖。
  ADMIN_PASSWORD   首次启动必填，设置 admin 密码（至少10字符）；后续提供可更新密码。
  BINARY_PATH      默认：项目目录/backend/household-bills。
  FRONTEND_DIST    默认：项目目录/frontend/dist（外置前端资源）。
  LISTEN_ADDR      默认：0.0.0.0:9890，允许局域网连接。
  COOKIE_SECURE    默认：false，适用于可信局域网 HTTP 测试。
  ALLOWED_ORIGINS  默认不设置，沿用后端同源校验。

仅在可信局域网测试；公网部署应使用 HTTPS 和 COOKIE_SECURE=true。
脚本不会启动/创建数据库，也不会读取 .env 文件。
USAGE
}

BUILD=false
for arg in "$@"; do
  case "$arg" in
    --build) BUILD=true ;;
    --help|-h) usage; exit 0 ;;
    *) printf '未知参数：%s\n' "$arg" >&2; usage >&2; exit 2 ;;
  esac
done

DATABASE_URL="${DATABASE_URL:-postgres://household_bills:w9mGUzNtuY7cW84pfdBRnXdY5ChX8o_XzMxbz0mES7k@127.0.0.1:5432/household_bills?sslmode=disable}"

BINARY_PATH="${BINARY_PATH:-$ROOT_DIR/backend/household-bills}"
FRONTEND_DIST="${FRONTEND_DIST:-$ROOT_DIR/frontend/dist}"
# Resolve caller-supplied relative paths before entering build directories.
[[ "$BINARY_PATH" = /* ]] || BINARY_PATH="$PWD/$BINARY_PATH"
[[ "$FRONTEND_DIST" = /* ]] || FRONTEND_DIST="$PWD/$FRONTEND_DIST"

if [[ "$BUILD" = true ]]; then
  for tool in npm go; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      printf '构建需要 %s。可安装构建工具，或使用已有二进制并省略 --build。\n' "$tool" >&2
      exit 1
    fi
  done
  printf '构建前端资源…\n'
  (cd "$ROOT_DIR/frontend" && npm ci && npm run build)
  printf '构建 Go 二进制…\n'
  mkdir -p -- "$(dirname -- "$BINARY_PATH")"
  (cd "$ROOT_DIR/backend" && CGO_ENABLED=0 go build -trimpath -o "$BINARY_PATH" ./cmd/server)
fi

if [[ ! -f "$BINARY_PATH" || ! -x "$BINARY_PATH" ]]; then
  printf '找不到可执行二进制：%s\n请先执行 ./test/start.sh --build，或设置 BINARY_PATH。\n' "$BINARY_PATH" >&2
  exit 1
fi
if [[ ! -f "$FRONTEND_DIST/index.html" ]]; then
  printf '找不到前端资源：%s/index.html\n请使用 --build 构建默认资源，或设置 FRONTEND_DIST 指向已有构建目录。\n' "$FRONTEND_DIST" >&2
  exit 1
fi

export DATABASE_URL FRONTEND_DIST
export LISTEN_ADDR="${LISTEN_ADDR:-0.0.0.0:9890}"
export COOKIE_SECURE="${COOKIE_SECURE:-false}"
printf '启动二进制：%s\n前端资源：%s\n监听地址：%s\n' "$BINARY_PATH" "$FRONTEND_DIST" "$LISTEN_ADDR"
printf '局域网访问：使用本机局域网 IP 和上述端口；必要时放行防火墙。\n'
printf '此脚本用于可信局域网测试；按 Ctrl+C 停止。\n'
if [[ "$COOKIE_SECURE" = true ]]; then
  printf 'COOKIE_SECURE=true：浏览器需通过 HTTPS 反向代理访问才能保持登录。\n'
fi
exec "$BINARY_PATH"
