#!/usr/bin/env bash
set -Eeuo pipefail
[[ "$(id -u)" -eq 0 ]] || { echo '请使用 root 安装磁盘维护定时器' >&2; exit 1; }
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Keep systemd unit expansion unambiguous.
[[ "$SCRIPT_DIR" =~ ^/[a-zA-Z0-9_./-]+$ ]] || { echo '部署路径含不支持的字符' >&2; exit 1; }
cat > /etc/systemd/system/metar-disk-maintenance.service <<UNIT
[Unit]
Description=Meta Pulse disk maintenance
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
ExecStart=/bin/bash $SCRIPT_DIR/maintain-disk.sh
TimeoutStartSec=30min
Nice=10
IOSchedulingClass=idle
UNIT
cat > /etc/systemd/system/metar-disk-maintenance.timer <<'UNIT'
[Unit]
Description=Meta Pulse disk maintenance every 15 minutes

[Timer]
OnCalendar=*-*-* *:02/15:00
Persistent=true

[Install]
WantedBy=timers.target
UNIT
chmod 644 /etc/systemd/system/metar-disk-maintenance.{service,timer}
systemctl daemon-reload
systemctl enable --now metar-disk-maintenance.timer
systemctl list-timers --all metar-disk-maintenance.timer --no-pager
