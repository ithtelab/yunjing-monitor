#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
cd "$ROOT"

random_hex() {
  bytes="$1"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes"
  else
    dd if=/dev/urandom bs="$bytes" count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'
  fi
}

if ! command -v docker >/dev/null 2>&1; then
  echo "错误：尚未安装 Docker。请先在宝塔应用商店安装 Docker 管理器。" >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "错误：缺少 docker compose 插件，请在宝塔 Docker 管理器中安装 Compose。" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "错误：暂不支持的服务器架构 $(uname -m)，目前支持 amd64 和 arm64。" >&2; exit 1 ;;
esac

if [ -f "bin/vps-server-linux-$ARCH" ]; then
  SERVER_BINARY="bin/vps-server-linux-$ARCH"
elif [ -f "release/vps-server-linux-$ARCH" ]; then
  SERVER_BINARY="release/vps-server-linux-$ARCH"
else
  echo "错误：找不到 vps-server-linux-$ARCH，请使用完整部署包。" >&2
  exit 1
fi

HOST_PORT="${HOST_PORT:-1314}"
HOST_BIND="${HOST_BIND:-127.0.0.1}"
PUBLIC_URL="${PUBLIC_URL:-http://127.0.0.1:$HOST_PORT}"
ADMIN_USER="${ADMIN_USER:-admin}"
GENERATED_PASSWORD=""

# Compose 会从项目目录读取 .env 做 ${VAR} 替换；不再依赖 env_file: docker.env
# （宝塔面板从 /tmp 创建栈时 env_file 相对路径会失败）
if [ ! -f .env ]; then
  AUTH_SECRET="${AUTH_SECRET:-$(random_hex 32)}"
  BACKUP_ENCRYPTION_KEY="${BACKUP_ENCRYPTION_KEY:-$(random_hex 32)}"
  if [ -n "${ADMIN_PASS:-}" ]; then
    PASSWORD="$ADMIN_PASS"
  else
    PASSWORD="$(random_hex 8)"
    GENERATED_PASSWORD="$PASSWORD"
  fi
  umask 077
  {
    echo "SERVER_BINARY=$SERVER_BINARY"
    echo "HOST_BIND=$HOST_BIND"
    echo "HOST_PORT=$HOST_PORT"
    echo "ADDR=0.0.0.0:1314"
    echo "AUTH_SECRET=$AUTH_SECRET"
    echo "BACKUP_ENCRYPTION_KEY=$BACKUP_ENCRYPTION_KEY"
    echo "BACKUP_DIR=/app/data/backups"
    echo "BACKUP_INTERVAL=24h"
    echo "ADMIN_USER=$ADMIN_USER"
    echo "ADMIN_PASS=$PASSWORD"
    echo "DATA_PATH=/app/data/server.json"
    echo "PUBLIC_URL=$PUBLIC_URL"
    echo "OFFLINE_WAIT=60s"
    echo "MAX_NODES=2000"
  } > .env
  chmod 600 .env
else
  # 已有 .env：只补 SERVER_BINARY / 端口，避免覆盖用户密钥
  if ! grep -q '^SERVER_BINARY=' .env 2>/dev/null; then
    echo "SERVER_BINARY=$SERVER_BINARY" >> .env
  fi
  if ! grep -q '^HOST_PORT=' .env 2>/dev/null; then
    echo "HOST_PORT=$HOST_PORT" >> .env
  fi
  if ! grep -q '^HOST_BIND=' .env 2>/dev/null; then
    echo "HOST_BIND=$HOST_BIND" >> .env
  fi
  if ! grep -q '^BACKUP_ENCRYPTION_KEY=' .env 2>/dev/null; then
    echo "BACKUP_ENCRYPTION_KEY=$(random_hex 32)" >> .env
  fi
  if ! grep -q '^BACKUP_DIR=' .env 2>/dev/null; then
    echo "BACKUP_DIR=/app/data/backups" >> .env
  fi
fi

# 兼容旧版：若只有 docker.env，复制为 .env
if [ ! -f .env ] && [ -f docker.env ]; then
  cp docker.env .env
  echo "SERVER_BINARY=$SERVER_BINARY" >> .env
  echo "HOST_PORT=$HOST_PORT" >> .env
fi

# 容器内进程以 UID/GID 10001 运行（见 Dockerfile USER monitor）。
# 挂载的 ./data 必须允许 10001 写，否则会报：
#   open /app/data/server.json: permission denied
mkdir -p data
fix_data_perms() {
  # 1) 本机 root 直接改
  if chown -R 10001:10001 data 2>/dev/null && chmod -R u+rwX,g+rwX,o-rwx data 2>/dev/null; then
    return 0
  fi
  # 2) 无 root 时用临时容器以 root 改宿主目录属主
  if docker run --rm -v "$ROOT/data:/data" alpine:3.22 \
      sh -c 'chown -R 10001:10001 /data && chmod -R u+rwX,g+rwX,o-rwx /data' 2>/dev/null; then
    return 0
  fi
  echo "错误：无法将 data 目录安全地交给容器 UID/GID 10001，已停止安装。" >&2
  echo "请修复宿主机挂载目录的 chown/chmod 支持后重试；不会使用 chmod 777。" >&2
  return 1
}
fix_data_perms
# 预创建空文件，避免部分文件系统对「创建新文件」与「写已有文件」权限表现不一致
docker run --rm -v "$ROOT/data:/data" alpine:3.22 sh -eu -c '
  [ -e /data/server.json ] || : > /data/server.json
  [ -e /data/content.json ] || printf "%s\n" "{\"announcement\":\"\",\"changelog\":\"\"}" > /data/content.json
  chown 10001:10001 /data/server.json /data/content.json
  chmod u+rw,g+rw,o-rwx /data/server.json /data/content.json
'
# 再次确保（预创建后）
fix_data_perms

docker compose up -d --build
echo
docker compose ps
echo
echo "云镜监控已启动"
echo "本机监听：http://$HOST_BIND:$HOST_PORT"
echo "外部地址：$PUBLIC_URL"
echo "后台路径：/admin"
echo "后台用户：$ADMIN_USER"
if [ -n "$GENERATED_PASSWORD" ]; then
  echo "后台密码：$GENERATED_PASSWORD"
  echo "请立即保存该密码（只显示一次）。"
elif [ -f .env ]; then
  echo "后台密码见项目目录 .env 中的 ADMIN_PASS"
fi
echo "查看日志：cd $ROOT && docker compose logs -f"
echo
echo "若日志出现 permission denied，请在项目目录执行："
echo "  mkdir -p data && docker run --rm -v \"\$PWD/data:/data\" alpine:3.22 chown -R 10001:10001 /data"
echo "  docker compose restart"
