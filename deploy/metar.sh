#!/usr/bin/env bash
# METAR management entry point; credentials and Compose overrides stay in lib.sh.
set -Eeuo pipefail
IFS=$'\n\t'
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

usage() {
  cat <<'HELP'
用法：metar [--env-file PATH] [命令]
不带命令打开数字菜单。

  update [升级参数]   备份、拉取、构建、迁移和检查（复用 update.sh）
  start              启动整套 METAR 服务，使用现有镜像
  restart            重启整套 METAR 服务并检查健康状态
  stop               停止整套 METAR 服务，保留容器和数据
  status             查看容器状态
  logs [服务] [-f]    最近 100 行日志；-f 持续跟踪
  uninstall [--yes]   移除容器和网络，保留数据库卷、配置、镜像和源码
  help               查看帮助

stop/restart/uninstall 会影响社区、Pulse 及其数据库，不操作独立的 new-api。
例：metar update --ref main --skip-forum
HELP
}

while [[ "${1:-}" == --env-file* ]]; do
  case "$1" in
    --env-file) [[ $# -ge 2 && -n "$2" ]] || die '--env-file 需要路径'; ENV_FILE="$(absolute_path "$2")"; shift 2 ;;
    --env-file=*) [[ -n "${1#*=}" ]] || die '--env-file 需要路径'; ENV_FILE="$(absolute_path "${1#*=}")"; shift ;;
    *) die "未知选项：$1" ;;
  esac
done
ENV_FILE="$(absolute_path "$ENV_FILE")"
COMPOSE_OVERRIDE_FILE="$(absolute_path "$COMPOSE_OVERRIDE_FILE")"
export META_PULSE_ENV_FILE="$ENV_FILE" META_PULSE_COMPOSE_OVERRIDE_FILE="$COMPOSE_OVERRIDE_FILE"

lock_management() {
  require_command flock
  local git_dir
  git_dir="$(git -C "$REPO_ROOT" rev-parse --absolute-git-dir)"
  exec 9>"$git_dir/meta-pulse-update.lock"
  flock -n 9 || die '已有升级或管理操作正在执行'
}

healthy() {
  local service
  while IFS= read -r service; do
    [[ -n "$service" ]] || continue
    wait_for_service "$service" 180 || die "$service 未就绪"
  done <<<"$services"
  wait_for_ready 120 || die 'Pulse API /readyz 检查失败'
  log '服务健康检查通过'
  show_runtime_status
}

run_command() (
  local command="${1:-help}"
  shift || true
  case "$command" in
    help|-h|--help) usage; return ;;
    update) exec "$SCRIPT_DIR/update.sh" "$@" ;;
    start|restart|stop|status) [[ $# == 0 ]] || die "$command 不接受额外参数" ;;
    uninstall) [[ $# == 0 || ( $# == 1 && "$1" == --yes ) ]] || die '用法：metar uninstall [--yes]' ;;
    logs) ;;
    *) die "未知命令：$command（使用 metar help）" ;;
  esac
  check_host_prerequisites
  require_existing_env_file
  local services
  services="$(compose config --services)"
  case "$command" in
    start|restart|stop|uninstall) lock_management ;;
  esac
  case "$command" in
    start)
      compose up -d --no-build
      healthy
      ;;
    restart)
      compose restart
      healthy
      ;;
    stop)
      compose stop
      log '已停止 METAR 服务，容器和数据保留'
      ;;
    status) show_runtime_status ;;
    logs)
      local service='' follow=0 arg
      for arg in "$@"; do
        case "$arg" in
          -f|--follow) follow=1 ;;
          -*) die "不支持的日志选项：$arg" ;;
          *) [[ -z "$service" ]] || die '日志只接受一个服务名'; service="$arg" ;;
        esac
      done
      if [[ -n "$service" ]] && ! grep -Fxq -- "$service" <<<"$services"; then
        die "未知服务：$service"
      fi
      local -a args=(logs --tail=100)
      (( follow == 0 )) || args+=(--follow)
      [[ -z "$service" ]] || args+=("$service")
      compose "${args[@]}"
      ;;
    uninstall)
      if [[ "${1:-}" != --yes ]]; then
        [[ -t 0 ]] || die '非交互卸载需显式指定 --yes；数据卷仍会保留'
        local answer
        printf '将停止并移除 METAR 容器与网络，保留全部数据和配置。输入 UNINSTALL 确认：'
        read -r answer || return 1
        [[ "$answer" == UNINSTALL ]] || { log '已取消'; return; }
      fi
      compose down
      log '已移除容器和网络；数据卷、配置、镜像、源码及 metar 命令保留，可用 metar start 恢复'
      ;;
  esac
)

if [[ $# -gt 0 ]]; then
  run_command "$@"
  exit $?
fi
[[ -t 0 ]] || { usage; exit 0; }
while true; do
  cat <<'MENU'

METAR 管理菜单
  1. 升级
  2. 重启全部服务
  3. 停止全部服务
  4. 启动全部服务
  5. 查看状态
  6. 查看最近日志
  7. 卸载服务（保留数据）
  0. 退出
MENU
  printf '请选择 [0-7]：'
  read -r choice || exit 0
  case "$choice" in
    1) command=update ;;
    2) command=restart ;;
    3) command=stop ;;
    4) command=start ;;
    5) command=status ;;
    6) command=logs ;;
    7) command=uninstall ;;
    0) exit 0 ;;
    *) printf '请输入 0 到 7。\n'; continue ;;
  esac
  # Do not put run_command in an if/|| condition: that disables Bash errexit
  # inside the function and can report success after a failed Compose action.
  set +e
  "$SCRIPT_DIR/metar.sh" "$command"
  result=$?
  set -e
  (( result == 0 )) || printf '操作失败（退出码 %s），请查看上方错误。\n' "$result"
done
