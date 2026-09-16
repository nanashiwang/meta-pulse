#!/usr/bin/env bash
# Pulse 日志摄入死循环修复 —— 部署步骤
#
# 背景：meta-pulse 的 usage_ingest 因查询退化为 logs 全表扫描，每轮 20s 超时
# 被杀、30s 后原样重试，游标停滞十天、零事件入库，同时把 new-api 的 MySQL
# CPU 持续打满。修复已提交并推送（meta-pulse 13040f8）。
#
# 在 23.94.111.46 上执行。

set -euo pipefail

PULSE_DIR="${PULSE_DIR:-/opt/meta-pulse}"

echo "==> 1/4 停止仍在死循环的旧 worker（立即止血）"
# 它已十天没有产出任何数据，停掉不损失任何东西，但 MySQL CPU 会立刻下降。
docker stop meta-pulse-pulse-worker-1

echo "==> 2/4 拉取修复"
cd "$PULSE_DIR"
git pull --ff-only origin main

echo "==> 3/4 重建镜像并启动"
docker compose build pulse-worker
docker compose up -d pulse-worker

echo "==> 4/4 观察日志（Ctrl-C 退出）"
# 预期看到 "usage ingest batch completed" 且 fetched > 0。
# 若仍出现 "context deadline exceeded"，不要重复重启——新版本会指数退避到
# 10 分钟，先把日志贴出来再排查。
docker logs -f --tail 50 meta-pulse-pulse-worker-1
