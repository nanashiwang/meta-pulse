#!/usr/bin/env bash
# Explicit opt-in: consistent backups briefly pause Forum/API/Worker writes.
set -Eeuo pipefail
ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
if [[ $# != 2 || "$1" != --apply ]]; then
  echo '用法：sudo deploy/install-backup-timer.sh --apply /etc/meta-pulse/offsite.env' >&2
  echo '每天 03:30（服务器时区）暂停三项应用、备份、恢复服务，并上传和回读校验。' >&2
  exit 2
fi
[[ "$(uname -s)" == Linux && $EUID == 0 ]] || { echo '需要 Linux root' >&2; exit 1; }
command -v restic >/dev/null
command -v systemctl >/dev/null
[[ -f "$2" && "$2" == /* ]] || { echo '需要绝对路径的异地配置文件' >&2; exit 1; }
# Keep unit quoting unambiguous. Do not interpolate credentials into units.
[[ "$ROOT" =~ ^/[A-Za-z0-9_./-]+$ && "$2" =~ ^/[A-Za-z0-9_./-]+$ ]] || exit 1
[[ "$(stat -c %a "$2")" == 600 ]] || { echo '异地配置权限必须为 600' >&2; exit 1; }
cat > /etc/systemd/system/metar-backup.service <<UNIT
[Unit]
Description=METAR consistent encrypted offsite backup and readback
Requires=docker.service
After=docker.service network-online.target
[Service]
Type=oneshot
WorkingDirectory=$ROOT
UMask=0077
ExecStart=/usr/bin/python3 $ROOT/deploy/backup.py create --quiesced --offsite-config $2
TimeoutStartSec=2h
UNIT
cat > /etc/systemd/system/metar-backup.timer <<'UNIT'
[Unit]
Description=Daily METAR recovery set
[Timer]
OnCalendar=*-*-* 03:30:00
RandomizedDelaySec=5m
Persistent=true
[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload
systemctl enable --now metar-backup.timer
printf '定时器已启用；用 systemctl status metar-backup.service 查看结果。请监控失败与备份年龄。\n'
