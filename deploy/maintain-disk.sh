#!/usr/bin/env bash
# Independent maintenance: no fetch, application configuration or deployment.
set -Eeuo pipefail
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
GIT_DIR="$(git -C "$REPO_ROOT" rev-parse --absolute-git-dir)"
exec 9>"$GIT_DIR/meta-pulse-update.lock"
if ! flock -n 9; then
  echo '[meta-pulse] 磁盘维护跳过：更新锁忙，下次定时重试'
  exit 0
fi
echo '[meta-pulse] 独立磁盘维护开始'
python3 "$SCRIPT_DIR/backup-retention.py" prune "$REPO_ROOT" --keep "${META_PULSE_BACKUP_KEEP:-1}"
python3 "$SCRIPT_DIR/build-cache.py" "$REPO_ROOT"
echo '[meta-pulse] 独立磁盘维护成功'
