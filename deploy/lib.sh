#!/usr/bin/env bash
# Meta Pulse deployment helpers. This file is sourced by install.sh/update.sh.

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
TEMPLATE_FILE="$SCRIPT_DIR/meta-pulse.env.example"
ENV_FILE="${META_PULSE_ENV_FILE:-$REPO_ROOT/.env}"
COMPOSE_FILE="$REPO_ROOT/docker-compose.yml"

log() {
  printf '[meta-pulse] %s\n' "$*"
}

warn() {
  printf '[meta-pulse] 警告：%s\n' "$*" >&2
}

die() {
  printf '[meta-pulse] 错误：%s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"
}

absolute_path() {
  local path="$1"
  if [[ "$path" == /* ]]; then
    printf '%s\n' "$path"
  else
    printf '%s/%s\n' "$PWD" "$path"
  fi
}

# Read KEY from the env file without sourcing it. Never execute deployment
# configuration as shell code because it contains credentials.
env_value() {
  local key="$1"
  awk -F= -v key="$key" '
    $1 == key { sub(/^[^=]*=/, ""); print; exit }
  ' "$ENV_FILE"
}

set_env_value() {
  local key="$1"
  local value="$2"
  local temp
  temp="$(mktemp "${TMPDIR:-/tmp}/meta-pulse-env.XXXXXX")"
  awk -v key="$key" -v value="$value" '
    BEGIN { prefix = key "="; replaced = 0 }
    index($0, prefix) == 1 {
      print prefix value
      replaced = 1
      next
    }
    { print }
    END {
      if (!replaced) print prefix value
    }
  ' "$ENV_FILE" >"$temp"
  chmod 600 "$temp"
  mv "$temp" "$ENV_FILE"
}

ensure_generated_secret() {
  local key="$1"
  local current
  current="$(env_value "$key")"
  case "$current" in
    ""|replace-me|CHANGE_ME|__GENERATE__)
      set_env_value "$key" "$(openssl rand -hex 32)"
      ;;
  esac
}

ensure_generated_password() {
  local key="$1"
  local current
  current="$(env_value "$key")"
  case "$current" in
    ""|replace-me|CHANGE_ME|__GENERATE__)
      set_env_value "$key" "$(openssl rand -hex 24)"
      ;;
  esac
}

ensure_env_file() {
  if [[ -e "$ENV_FILE" && ! -f "$ENV_FILE" ]]; then
    die "环境文件不是普通文件：$ENV_FILE"
  fi
  if [[ ! -f "$ENV_FILE" ]]; then
    [[ -f "$TEMPLATE_FILE" ]] || die "找不到生产配置模板：$TEMPLATE_FILE"
    mkdir -p "$(dirname "$ENV_FILE")"
    cp "$TEMPLATE_FILE" "$ENV_FILE"
    log "已创建生产配置：$ENV_FILE"
  fi
  chmod 600 "$ENV_FILE"

  ensure_generated_password PULSE_DB_PASSWORD
  ensure_generated_password PULSE_DB_ROOT_PASSWORD
  ensure_generated_password FORUM_DB_PASSWORD
  ensure_generated_password FORUM_DB_ROOT_PASSWORD
  ensure_generated_secret PULSE_SERVICE_HMAC_SECRET
  ensure_generated_secret PULSE_USER_BFF_HMAC_SECRET
  ensure_generated_secret PULSE_ADMIN_HMAC_SECRET
  ensure_generated_secret PULSE_REWARD_RANDOM_SECRET

  # Optional shell variables make non-interactive provisioning possible while
  # keeping DSNs out of command-line arguments and process listings.
  local key
  for key in PULSE_ENV NEWAPI_LOG_DSN FORUM_DB_DSN NEWAPI_INTERNAL_BASE_URL; do
    if [[ -n "${!key:-}" ]]; then
      set_env_value "$key" "${!key}"
    fi
  done
}

# Updates fail closed: only installation may create or repair credentials.
require_existing_env_file() {
  [[ -f "$ENV_FILE" ]] || die "更新需要已有配置：${ENV_FILE}；请从备份恢复，禁止重新生成已有数据卷的凭据"
}

require_env_value() {
  local key="$1"
  local value
  value="$(env_value "$key")"
  [[ -n "${value//[[:space:]]/}" ]] || die "$key 未配置，请编辑 $ENV_FILE"
}

validate_environment() {
  local pulse_env
  pulse_env="$(env_value PULSE_ENV)"
  [[ "$pulse_env" == production ]] || die "部署脚本只接受 PULSE_ENV=production（当前：${pulse_env:-未设置}）"

  local key value
  for key in PULSE_DB_PASSWORD PULSE_DB_ROOT_PASSWORD FORUM_DB_PASSWORD FORUM_DB_ROOT_PASSWORD; do
    require_env_value "$key"
    value="$(env_value "$key")"
    case "$value" in
      replace-me|CHANGE_ME|__GENERATE__) die "$key 仍为占位密码，请恢复真实凭据" ;;
    esac
  done
  for key in PULSE_SERVICE_HMAC_SECRET PULSE_USER_BFF_HMAC_SECRET PULSE_ADMIN_HMAC_SECRET PULSE_REWARD_RANDOM_SECRET; do
    value="$(env_value "$key")"
    [[ "${#value}" -ge 32 && "$value" != replace-me && "$value" != CHANGE_ME && "$value" != __GENERATE__ ]] \
      || die "$key 必须是至少 32 字节的非占位密钥"
  done

  if [[ "$(env_value PULSE_REWARD_SHADOW_MODE)" == false ]]; then
    warn "PULSE_REWARD_SHADOW_MODE=false：请确认 new-api Benefit、对账和回滚已完成真实环境验收"
  fi
}

validate_worker_environment() {
  require_env_value NEWAPI_LOG_DSN
  require_env_value NEWAPI_INTERNAL_BASE_URL
}

compose() (
  # Compose normally prefers exported shell values over --env-file. Clear only
  # application configuration in this subshell so the validated/backed-up file
  # is also the configuration that is deployed. Docker connection vars remain.
  local key
  for key in $(compgen -e); do
    case "$key" in
      PULSE_*|NEWAPI_*|FORUM_*|COMPOSE_PROJECT_NAME) unset "$key" ;;
    esac
  done
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
)

validate_compose() {
  log "校验 Docker Compose 配置"
  compose config >/dev/null
}

backup_database_if_running() {
  local service="$1"
  local output_file="$2"
  local container status

  container="$(compose ps -q "$service" 2>/dev/null || true)"
  [[ -n "$container" ]] || return 0
  status="$(docker inspect --format '{{.State.Status}}' "$container" 2>/dev/null || true)"
  if [[ "$status" != running ]]; then
    warn "$service 当前未运行，跳过数据库备份"
    return 0
  fi

  log "备份数据库：$service"
  : >"$output_file"
  chmod 600 "$output_file"
  compose exec -T "$service" sh -c \
    'exec env MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump --single-transaction --routines --events --triggers --hex-blob -uroot "$MYSQL_DATABASE"' \
    >>"$output_file"
}

wait_for_service() {
  local service="$1"
  local timeout="${2:-180}"
  local deadline=$((SECONDS + timeout))
  local container status

  log "等待服务就绪：$service"
  while (( SECONDS < deadline )); do
    container="$(compose ps -q "$service" 2>/dev/null || true)"
    if [[ -n "$container" ]]; then
      status="$(docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' "$container" 2>/dev/null || true)"
      case "$status" in
        "running healthy"*) return 0 ;;
        "running no-healthcheck") return 0 ;;
        "exited"*|"dead"*)
          compose logs --tail=80 "$service" >&2 || true
          return 1
          ;;
      esac
    fi
    sleep 2
  done
  compose ps >&2 || true
  compose logs --tail=80 "$service" >&2 || true
  return 1
}

wait_for_ready() {
  local timeout="${1:-120}"
  local deadline=$((SECONDS + timeout))
  while (( SECONDS < deadline )); do
    if compose exec -T pulse-api wget --spider --quiet http://127.0.0.1:8088/readyz >/dev/null 2>&1; then
      log "Pulse API /readyz 已通过"
      return 0
    fi
    sleep 2
  done
  compose logs --tail=120 pulse-api >&2 || true
  return 1
}

check_host_prerequisites() {
  [[ "$(uname -s)" == Linux ]] || die "部署脚本目标为 Linux 服务器"
  require_command docker
  require_command git
  require_command openssl
  docker info >/dev/null 2>&1 || die "Docker daemon 未运行，或当前用户无权访问 Docker"
  docker compose version >/dev/null 2>&1 || die "需要 Docker Compose v2（docker compose）"
  [[ -f "$COMPOSE_FILE" ]] || die "找不到 Compose 文件：$COMPOSE_FILE"
  git -C "$REPO_ROOT" rev-parse --show-toplevel >/dev/null 2>&1 || die "当前目录不是 Git 仓库"
}

show_runtime_status() {
  compose ps
}

show_failure_logs() {
  warn "最近容器日志："
  compose logs --tail=120 pulse-api pulse-worker forum 2>&1 || true
}
