#!/usr/bin/env bash
# 安装每日两次的 Certbot 检查；systemd 自带随机延迟，renew.sh 不再额外休眠。
set -Eeuo pipefail
IFS=$'\n\t'

[[ "$(id -u)" -eq 0 ]] || { echo '[meta-pulse] 错误：请使用 root 安装 systemd timer' >&2; exit 1; }
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)"
[[ "$REPO_ROOT" != *[[:space:]]* ]] || { echo '[meta-pulse] 错误：部署路径不能包含空格' >&2; exit 1; }
[[ -x "$SCRIPT_DIR/renew.sh" ]] || { echo '[meta-pulse] 错误：renew.sh 不可执行' >&2; exit 1; }

cat > /etc/systemd/system/metar-certbot-renew.service <<UNIT
[Unit]
Description=Renew metar.uk Let's Encrypt certificate
After=docker.service network-online.target
Wants=network-online.target
Requires=docker.service

[Service]
Type=oneshot
WorkingDirectory=$REPO_ROOT
ExecStart=/bin/bash $SCRIPT_DIR/renew.sh
UNIT

cat > /etc/systemd/system/metar-certbot-renew.timer <<'UNIT'
[Unit]
Description=Twice-daily metar.uk certificate renewal check

[Timer]
OnCalendar=*-*-* 03,15:17:00
RandomizedDelaySec=45m
Persistent=true

[Install]
WantedBy=timers.target
UNIT

chmod 644 /etc/systemd/system/metar-certbot-renew.service /etc/systemd/system/metar-certbot-renew.timer
systemctl daemon-reload
systemctl enable --now metar-certbot-renew.timer
systemctl list-timers --all metar-certbot-renew.timer --no-pager
