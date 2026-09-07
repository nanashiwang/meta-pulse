#!/usr/bin/env bash
# 构建 METAR 正式前端，并写入现有博客静态卷下的隔离目录。
set -Eeuo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
command -v python3 >/dev/null 2>&1 || { echo '[meta-pulse] 错误：缺少 python3，无法构建 METAR 正式前端' >&2; exit 1; }
BLOG_DIST="$ROOT/sites/blog/docs/.vitepress/dist"
OUTPUT="$BLOG_DIST/metar"
mkdir -p "$BLOG_DIST"
python3 "$ROOT/metar-frontend/production/build.py" --output "$OUTPUT"
[[ -s "$OUTPUT/index.html" && -s "$OUTPUT/assets/app.js" && -s "$OUTPUT/assets/favicon.svg" && -s "$OUTPUT/runtime-config.js" ]] || {
  echo '[meta-pulse] 错误：METAR 正式前端构建产物不完整' >&2
  exit 1
}
printf '[meta-pulse] METAR 正式前端已构建：%s\n' "$OUTPUT"
