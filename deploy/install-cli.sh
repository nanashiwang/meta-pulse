#!/usr/bin/env bash
# Register a stable launcher, not a copy that becomes stale after git update.
set -Eeuo pipefail
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR=/usr/local/bin
if [[ "${1:-}" == --bin-dir && $# == 2 ]]; then
  BIN_DIR="$2"
elif [[ $# != 0 ]]; then
  printf '用法：bash deploy/install-cli.sh [--bin-dir PATH]\n' >&2
  exit 1
fi
mkdir -p "$BIN_DIR"
BIN_DIR="$(CDPATH= cd -- "$BIN_DIR" && pwd)"
TARGET="$BIN_DIR/metar"
if [[ -e "$TARGET" || -L "$TARGET" ]]; then
  if [[ -L "$TARGET" ]] || ! grep -qxF '# Meta Pulse managed launcher' "$TARGET"; then
    printf '拒绝覆盖已有的非 METAR 文件：%s\n' "$TARGET" >&2
    exit 1
  fi
fi
TEMP="$(mktemp "$BIN_DIR/.metar.XXXXXX")"
trap 'rm -f "$TEMP"' EXIT
{
  printf '#!/usr/bin/env bash\n# Meta Pulse managed launcher\n'
  printf 'exec bash %q "$@"\n' "$SCRIPT_DIR/metar.sh"
} >"$TEMP"
chmod 755 "$TEMP"
mv -f "$TEMP" "$TARGET"
printf '已安装：%s\n输入 metar 打开菜单，或使用 metar update。\n仓库路径：%s\n' "$TARGET" "$(dirname "$SCRIPT_DIR")"
printf '若提示命令不存在，请将 %s 加入 PATH。\n' "$BIN_DIR"
