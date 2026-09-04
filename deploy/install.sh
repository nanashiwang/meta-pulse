#!/usr/bin/env bash
# 在已检出的 Meta Pulse 仓库中完成首次生产部署。
set -Eeuo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

NO_BUILD=0
SKIP_FORUM=0
SKIP_WORKER=0

usage() {
  cat <<'USAGE'
用法：deploy/install.sh [选项]

首次运行会创建并保护仓库根目录 .env；请填写 new-api LOG_DB 只读 DSN 后再次运行。

选项：
  --env-file PATH   使用指定生产配置文件（默认：.env）
  --no-build        不构建镜像，仅使用已有镜像
  --skip-forum      不部署/更新 Apache Answer
  --skip-worker     不启动 worker（仅用于 API 临时验收）
  -h, --help        显示帮助
USAGE
}

while (($# > 0)); do
  case "$1" in
    --env-file)
      (($# >= 2)) || die "--env-file 需要路径"
      ENV_FILE="$(absolute_path "$2")"
      shift 2
      ;;
    --env-file=*)
      ENV_FILE="$(absolute_path "${1#*=}")"
      shift
      ;;
    --no-build)
      NO_BUILD=1
      shift
      ;;
    --skip-forum)
      SKIP_FORUM=1
      shift
      ;;
    --skip-worker)
      SKIP_WORKER=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "未知选项：$1（使用 --help 查看用法）"
      ;;
  esac
done

check_host_prerequisites
ensure_env_file
validate_environment
if (( SKIP_WORKER == 0 )); then
  validate_worker_environment
fi
validate_compose

on_error() {
  local rc=$?
  trap - ERR
  show_failure_logs
  printf '[meta-pulse] 首次部署失败（退出码 %s），未执行任何数据卷删除操作。\n' "$rc" >&2
  exit "$rc"
}
trap on_error ERR

log "拉取基础镜像"
compose pull mysql redis forum-mysql

if (( NO_BUILD == 0 )); then
  build_services=(pulse-api)
  (( SKIP_WORKER == 0 )) && build_services+=(pulse-worker)
  (( SKIP_FORUM == 0 )) && build_services+=(forum)
  log "构建服务镜像：${build_services[*]}"
  compose build "${build_services[@]}"
fi

log "启动数据库和 Redis"
compose up -d mysql redis forum-mysql
wait_for_service mysql 180 || die "Pulse MySQL 未就绪"
wait_for_service redis 120 || die "Redis 未就绪"
wait_for_service forum-mysql 180 || die "Forum MySQL 未就绪"

log "执行 Pulse 数据库迁移"
compose run --rm --no-deps --entrypoint meta-pulse-tool pulse-api migrate-up

services_to_start=(pulse-api)
(( SKIP_WORKER == 0 )) && services_to_start+=(pulse-worker)
(( SKIP_FORUM == 0 )) && services_to_start+=(forum)
log "启动服务：${services_to_start[*]}"
compose up -d "${services_to_start[@]}"
wait_for_service pulse-api 180 || die "Pulse API 未就绪"
wait_for_ready 120 || die "Pulse API /readyz 检查失败"
if (( SKIP_WORKER == 0 )); then
  wait_for_service pulse-worker 120 || die "Pulse Worker 未就绪"
fi
if (( SKIP_FORUM == 0 )); then
  wait_for_service forum 120 || die "Forum 未就绪"
fi

trap - ERR
log "首次部署完成"
show_runtime_status
log "配置文件：${ENV_FILE}（权限已设为 600）"
log "后续更新请执行：$SCRIPT_DIR/update.sh"
