#!/usr/bin/env bash
# 拉取代码、构建镜像、迁移并安全重建 Meta Pulse 服务。
set -Eeuo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

NO_BUILD=0
SKIP_FORUM=0
SKIP_WORKER=0
REF=""

usage() {
  cat <<'USAGE'
用法：deploy/update.sh [选项]

默认将当前分支以 fast-forward 方式更新，然后构建、迁移、重启服务。
.env 始终保留，不会被 Git 覆盖；脚本使用 Git 锁避免并发更新。

选项：
  --env-file PATH   使用指定生产配置文件（默认：.env）
  --ref BRANCH      更新指定远程分支（默认：当前分支）
  --no-build        不构建镜像，仅使用已有镜像
  --skip-forum      不构建/重启 Apache Answer
  --skip-worker     不构建/重启 worker
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
    --ref)
      (($# >= 2)) || die "--ref 需要分支名"
      REF="$2"
      shift 2
      ;;
    --ref=*)
      REF="${1#*=}"
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

CURRENT_BRANCH="$(git -C "$REPO_ROOT" symbolic-ref --quiet --short HEAD || true)"
[[ -n "$CURRENT_BRANCH" ]] || die "当前仓库处于 detached HEAD，请先 checkout 一个本地分支后再更新"
[[ -n "$REF" ]] || REF="$CURRENT_BRANCH"
[[ "$REF" != refs/* && "$REF" != -* ]] || die "--ref 只接受分支名"

# 只拒绝 tracked 修改；服务器上的 .env 和其他 ignored/untracked 文件不会阻断更新。
git -C "$REPO_ROOT" diff --quiet || die "存在未提交的 tracked 工作区修改，请先处理"
git -C "$REPO_ROOT" diff --cached --quiet || die "存在已暂存的 tracked 修改，请先处理"

OLD_COMMIT="$(git -C "$REPO_ROOT" rev-parse HEAD)"
OLD_BRANCH="$CURRENT_BRANCH"

# Git 的 lock 路径位于 .git 内，不会污染工作区；flock 是 Linux 部署的标准工具。
require_command flock
GIT_DIR="$(git -C "$REPO_ROOT" rev-parse --absolute-git-dir)"
LOCK_FILE="$GIT_DIR/meta-pulse-update.lock"
exec 9>"$LOCK_FILE"
flock -n 9 || die "已有另一个 Meta Pulse 更新正在执行"

BACKUP_DIR="$REPO_ROOT/.data/deploy-backups/$(date -u +%Y%m%dT%H%M%SZ)-$OLD_COMMIT-$$"
mkdir -p "$BACKUP_DIR"
chmod 700 "$REPO_ROOT/.data" "$REPO_ROOT/.data/deploy-backups" "$BACKUP_DIR"
cp "$ENV_FILE" "$BACKUP_DIR/.env"
chmod 600 "$BACKUP_DIR/.env"
compose config >"$BACKUP_DIR/compose.before.yml"
chmod 600 "$BACKUP_DIR/compose.before.yml"

on_error() {
  local rc=$?
  trap - ERR
  show_failure_logs
  cat >&2 <<EOF_ERROR
[meta-pulse] 更新失败（退出码 $rc）。数据库迁移不会自动回滚，也未删除任何数据卷。
[meta-pulse] 当前代码可能已更新到：$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)
[meta-pulse] 原 commit：$OLD_COMMIT
[meta-pulse] 配置备份：$BACKUP_DIR/.env
[meta-pulse] 如需回退代码，请先确认新迁移与旧版本兼容，再执行：
  git -C "$REPO_ROOT" checkout "$OLD_BRANCH"
  git -C "$REPO_ROOT" reset --hard "$OLD_COMMIT"
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" build pulse-api pulse-worker forum
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d pulse-api pulse-worker forum
EOF_ERROR
  exit "$rc"
}
trap on_error ERR

backup_database_if_running mysql "$BACKUP_DIR/pulse.sql"
backup_database_if_running forum-mysql "$BACKUP_DIR/forum.sql"

log "拉取远程分支：origin/$REF"
git -C "$REPO_ROOT" fetch --prune origin "$REF"
if git -C "$REPO_ROOT" show-ref --verify --quiet "refs/remotes/origin/$REF"; then
  if [[ "$CURRENT_BRANCH" != "$REF" ]]; then
    if git -C "$REPO_ROOT" show-ref --verify --quiet "refs/heads/$REF"; then
      git -C "$REPO_ROOT" checkout "$REF"
    else
      git -C "$REPO_ROOT" checkout --track -b "$REF" "origin/$REF"
    fi
  fi
  git -C "$REPO_ROOT" merge --ff-only "origin/$REF"
else
  printf '[meta-pulse] 找不到远程分支 origin/%s\n' "$REF" >&2
  false
fi

validate_compose
if (( NO_BUILD == 0 )); then
  build_services=(pulse-api)
  (( SKIP_WORKER == 0 )) && build_services+=(pulse-worker)
  (( SKIP_FORUM == 0 )) && build_services+=(forum)
  log "构建服务镜像：${build_services[*]}"
  compose build "${build_services[@]}"
fi

log "执行数据库迁移（只前进，不执行 down）"
compose run --rm --no-deps --entrypoint meta-pulse-tool pulse-api migrate-up

services_to_start=(pulse-api)
(( SKIP_WORKER == 0 )) && services_to_start+=(pulse-worker)
(( SKIP_FORUM == 0 )) && services_to_start+=(forum)
log "重建服务：${services_to_start[*]}"
compose up -d "${services_to_start[@]}"
wait_for_service pulse-api 180
wait_for_ready 120
if (( SKIP_WORKER == 0 )); then
  wait_for_service pulse-worker 120
fi
if (( SKIP_FORUM == 0 )); then
  wait_for_service forum 120
fi

trap - ERR
log "更新完成：$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
show_runtime_status
log "备份目录：$BACKUP_DIR"
