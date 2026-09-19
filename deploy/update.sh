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
RELEASE_TAG=""
ACCEPT_INGEST=0

usage() {
  cat <<'USAGE'
用法：deploy/update.sh [选项]

默认将当前分支以 fast-forward 方式更新，然后构建、迁移、重启服务。
.env 只读校验，不会初始化或轮换凭据；缺失配置时停止。脚本使用 Git 锁避免并发更新。

选项：
  --accept-ingest   更新后执行 180 秒只读摄入验收（需 python3）
  --env-file PATH   使用指定生产配置文件（默认：.env）
  --ref BRANCH      更新指定远程分支（默认：当前分支）
  --release vX.Y.Z  部署已发布版本，核对清单并完整重建所有服务
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
    --accept-ingest)
      ACCEPT_INGEST=1
      shift
      ;;
    --release)
      (($# >= 2)) || die "--release 需要版本号"
      [[ -n "$2" ]] || die "--release 需要版本号"
      RELEASE_TAG="$2"
      shift 2
      ;;
    --release=*)
      RELEASE_TAG="${1#*=}"
      [[ -n "$RELEASE_TAG" ]] || die "--release 需要版本号"
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

if [[ -n "$RELEASE_TAG" ]]; then
  [[ "$RELEASE_TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || die "正式版本必须为 vX.Y.Z"
  [[ -z "$REF" ]] || die "--release 不能与 --ref 同用"
  (( NO_BUILD == 0 && SKIP_FORUM == 0 && SKIP_WORKER == 0 )) || die "正式版本部署必须完整构建所有服务"
  require_command python3
fi

if (( ACCEPT_INGEST == 1 )); then
  require_command python3
  (( SKIP_WORKER == 0 )) || die "--accept-ingest 不能与 --skip-worker 同用"
fi

check_host_prerequisites
# Lock before reading or backing up deployment configuration. Updating must
# never initialize credentials or import shell overrides like install does.
require_command flock
GIT_DIR="$(git -C "$REPO_ROOT" rev-parse --absolute-git-dir)"
LOCK_FILE="$GIT_DIR/meta-pulse-update.lock"
exec 9>"$LOCK_FILE"
flock -n 9 || die "已有另一个 Meta Pulse 更新正在执行"

require_existing_env_file
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
backup_runtime_keys_if_present pulse-api "$BACKUP_DIR/runtime-keys/api"
backup_runtime_keys_if_present pulse-worker "$BACKUP_DIR/runtime-keys/worker"

if [[ -n "$RELEASE_TAG" ]]; then
  log "核实正式版本：$RELEASE_TAG"
  git -C "$REPO_ROOT" fetch --no-tags origin "refs/tags/$RELEASE_TAG:refs/tags/$RELEASE_TAG"
  TARGET_COMMIT="$(git -C "$REPO_ROOT" rev-parse "refs/tags/$RELEASE_TAG^{commit}")"
  python3 "$SCRIPT_DIR/verify-release.py" "$RELEASE_TAG" "$TARGET_COMMIT"
  [[ "v$(git -C "$REPO_ROOT" show "$TARGET_COMMIT:VERSION")" == "$RELEASE_TAG" ]] || die "标签与 VERSION 不一致"
  # Refuse a no-op merge when the checkout is ahead of the requested version.
  # Production Git history is never reset by the updater.
  git -C "$REPO_ROOT" merge-base --is-ancestor HEAD "$TARGET_COMMIT" || die "当前代码超前或分叉，不能快进到指定版本"
  git -C "$REPO_ROOT" merge --ff-only "$TARGET_COMMIT"
  [[ "$(git -C "$REPO_ROOT" rev-parse HEAD)" == "$TARGET_COMMIT" ]] || die "部署提交与发布版本不一致"
else
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
fi

validate_compose
GATEWAY_ENABLED=0
BLOG_CHANGED=0
COMMUNITY_CHANGED=0
GATEWAY_CONFIG_CHANGED=0
if compose config --services | grep -Fx gateway >/dev/null; then
  GATEWAY_ENABLED=1
fi
if ! git -C "$REPO_ROOT" diff --quiet "$OLD_COMMIT" HEAD -- sites/blog; then
  BLOG_CHANGED=1
fi
if ! git -C "$REPO_ROOT" diff --quiet "$OLD_COMMIT" HEAD -- metar-frontend/production metar-frontend/src/styles.css deploy/build-community.sh; then
  COMMUNITY_CHANGED=1
fi
# 网关直接挂载 Nginx 文件。Git 快进可能替换源文件 inode，已有容器会继续
# 使用旧的 bind mount；此时仅 reload 不足，必须重建网关容器。
if ! git -C "$REPO_ROOT" diff --quiet "$OLD_COMMIT" HEAD -- deploy/nginx/meta-pulse.conf; then
  GATEWAY_CONFIG_CHANGED=1
fi
if [[ -n "$RELEASE_TAG" ]]; then
  # Reinstalling a tag also refreshes host-local static builds.
  BLOG_CHANGED=1
  COMMUNITY_CHANGED=1
fi
if (( GATEWAY_ENABLED == 1 && (BLOG_CHANGED == 1 || COMMUNITY_CHANGED == 1) )); then
  if (( NO_BUILD == 0 )); then
    if (( BLOG_CHANGED == 1 )); then
      log "构建更新后的社区博客静态文件"
      "$SCRIPT_DIR/build-blog.sh"
    fi
    # VitePress rebuild clears its dist directory, therefore the METAR shell
    # must be rebuilt after every blog rebuild even when its own source did not change.
    log "构建更新后的 METAR 正式前端"
    "$SCRIPT_DIR/build-community.sh"
  else
    warn "检测到社区静态前端变更，但 --no-build 已跳过构建"
  fi
fi

if (( NO_BUILD == 0 )); then
  build_services=(pulse-api)
  (( SKIP_WORKER == 0 )) && build_services+=(pulse-worker)
  (( SKIP_FORUM == 0 )) && build_services+=(forum)
  log "构建服务镜像：${build_services[*]}"
  compose build "${build_services[@]}"
fi

# Drain every old API replica before the new idempotency implementation can
# accept writes. New-api's model/billing/login services are not stopped.
log "排空旧 Pulse API 写请求（不影响 new-api 主链路）"
compose stop -t 15 pulse-api

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
if (( GATEWAY_ENABLED == 1 )); then
  [[ -s "$REPO_ROOT/sites/blog/docs/.vitepress/dist/index.html" ]] || die "社区网关已启用，但博客静态产物不存在"
  [[ -s "$REPO_ROOT/sites/blog/docs/.vitepress/dist/metar/index.html" ]] || die "社区网关已启用，但 METAR 正式前端产物不存在"
  log "校验并更新社区 HTTPS 网关"
  compose run --rm --no-deps --entrypoint nginx gateway -t
  if (( GATEWAY_CONFIG_CHANGED == 1 || BLOG_CHANGED == 1 )); then
    # build-blog replaces the dist directory inode. Reloading nginx leaves its
    # bind mount attached to the removed directory and serves permanent 404s.
    log "检测到网关配置或博客目录变更，重建网关以刷新文件挂载"
    compose up -d --force-recreate --no-deps gateway
  else
    compose up -d gateway
  fi
  wait_for_service gateway 120
  compose exec -T gateway test -s /var/www/blog/index.html
  compose exec -T gateway test -s /var/www/blog/metar/index.html
  compose exec -T --user nginx gateway test -r /var/www/blog/metar/index.html
  compose exec -T --user nginx gateway test -r /var/www/blog/metar/assets/app.js
  compose exec -T gateway nginx -t
  compose exec -T gateway nginx -s reload
fi

if [[ -n "$RELEASE_TAG" ]]; then
  for service in pulse-api pulse-worker forum; do
    container="$(compose ps -q "$service")"
    [[ -n "$container" ]] || die "$service 容器不存在"
    actual_revision="$(docker inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$container")"
    actual_version="$(docker inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$container")"
    [[ "$actual_revision" == "$TARGET_COMMIT" && "$actual_version" == "${RELEASE_TAG#v}" ]] || die "$service 实际镜像与发布版本不一致"
  done
  printf '%s\n%s\n' "$RELEASE_TAG" "$TARGET_COMMIT" >"$BACKUP_DIR/deployed-release.txt"
  chmod 600 "$BACKUP_DIR/deployed-release.txt"
  cp "$BACKUP_DIR/deployed-release.txt" "$REPO_ROOT/.data/deployed-release.txt"
  chmod 600 "$REPO_ROOT/.data/deployed-release.txt"
  log "运行版本已核实：$RELEASE_TAG ($TARGET_COMMIT)"
fi

trap - ERR
log "更新完成：$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
show_runtime_status
log "备份目录：$BACKUP_DIR"

if (( ACCEPT_INGEST == 1 )); then
  META_PULSE_ENV_FILE="$ENV_FILE" python3 "$SCRIPT_DIR/accept-ingest.py"
fi
