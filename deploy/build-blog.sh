#!/usr/bin/env bash
# 使用一次性 Node 容器构建 VitePress，只发布静态产物。
set -Eeuo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
BLOG_DIR="$REPO_ROOT/sites/blog"

command -v docker >/dev/null 2>&1 || { echo '[meta-pulse] 错误：缺少 docker' >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo '[meta-pulse] 错误：Docker daemon 不可用' >&2; exit 1; }
[[ -f "$BLOG_DIR/package-lock.json" ]] || { echo '[meta-pulse] 错误：缺少博客 package-lock.json' >&2; exit 1; }

rm -rf "$BLOG_DIR/docs/.vitepress/dist"
docker run --rm \
  -e HOST_UID="$(id -u)" \
  -e HOST_GID="$(id -g)" \
  -v "$BLOG_DIR:/app" \
  -v /app/node_modules \
  -w /app \
  node:22-alpine sh -ec '
    apk add --no-cache git >/dev/null
    npm ci --ignore-scripts
    npm run build
    chown -R "$HOST_UID:$HOST_GID" docs/.vitepress
  '

[[ -f "$BLOG_DIR/docs/.vitepress/dist/index.html" ]] || { echo '[meta-pulse] 错误：博客首页未生成' >&2; exit 1; }
echo '[meta-pulse] 博客静态构建完成'
