#!/usr/bin/env bash
# 部署脚本的离线回归测试：不连接 Docker、不修改真实 .env。
set -Eeuo pipefail
IFS=$'\n\t'

ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

bash -n "$ROOT/deploy/lib.sh" "$ROOT/deploy/install.sh" "$ROOT/deploy/update.sh"

# 校验生产模板初始化会生成随机凭据，并保持最小文件权限。
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/meta-pulse-deploy-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT
# shellcheck source=lib.sh
source "$ROOT/deploy/lib.sh"
ENV_FILE="$tmp_dir/.env"
ensure_env_file
validate_environment

for key in PULSE_DB_PASSWORD PULSE_DB_ROOT_PASSWORD FORUM_DB_PASSWORD FORUM_DB_ROOT_PASSWORD \
  PULSE_SERVICE_HMAC_SECRET PULSE_USER_BFF_HMAC_SECRET PULSE_ADMIN_HMAC_SECRET PULSE_REWARD_RANDOM_SECRET; do
  value="$(env_value "$key")"
  [[ "${#value}" -ge 32 ]] || { echo "$key 未生成有效随机值" >&2; exit 1; }
  [[ "$value" != replace-me && "$value" != __GENERATE__ ]] || { echo "$key 仍是占位值" >&2; exit 1; }
done

# GNU stat -f treats %Lp as a filename and may emit filesystem data even
# when it fails. Select the platform syntax instead of combining stdout.
case "$(uname -s)" in
  Darwin) mode="$(stat -f '%Lp' "$ENV_FILE")" ;;
  *) mode="$(stat -c '%a' "$ENV_FILE")" ;;
esac
[[ "$mode" == 600 ]] || { echo ".env 权限不是 600：$mode" >&2; exit 1; }

# Compose 中 API 不应拥有 new-api LOG_DB 读取凭据；只有 worker 可以拥有。
api_block="$(awk '/^  pulse-api:/{in_api=1; next} /^  pulse-worker:/{in_api=0} in_api' "$ROOT/docker-compose.yml")"
if grep -q 'NEWAPI_LOG_DSN' <<<"$api_block"; then
  echo 'pulse-api 不应注入 NEWAPI_LOG_DSN' >&2
  exit 1
fi

grep -q 'PULSE_ENV: ${PULSE_ENV:-development}' "$ROOT/docker-compose.yml"
grep -q 'healthcheck:' <<<"$(awk '/^  pulse-worker:/{in_worker=1} /^  forum-mysql:/{in_worker=0} in_worker' "$ROOT/docker-compose.yml")"
grep -q '^\.env$' "$ROOT/.dockerignore"
grep -q '^docker-compose.override.yml$' "$ROOT/.gitignore"
grep -q 'META_PULSE_COMPOSE_OVERRIDE_FILE' "$ROOT/deploy/lib.sh"
grep -q 'compose_files+=(.*COMPOSE_OVERRIDE_FILE' "$ROOT/deploy/lib.sh"
grep -q 'chmod 600 "\$BACKUP_DIR/compose.before.yml"' "$ROOT/deploy/update.sh"
grep -q 'mysqldump' "$ROOT/deploy/lib.sh"
grep -q ': >"\$output_file"' "$ROOT/deploy/lib.sh"

bash "$ROOT/deploy/update_test.sh"

echo '部署脚本离线测试通过'
