#!/usr/bin/env bash
# 部署脚本的离线回归测试：不连接 Docker、不修改真实 .env。
set -Eeuo pipefail
IFS=$'\n\t'

ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

bash -n "$ROOT/deploy/lib.sh" "$ROOT/deploy/install.sh" "$ROOT/deploy/update.sh" "$ROOT/deploy/build-blog.sh" "$ROOT/deploy/build-community.sh" "$ROOT/deploy/nginx/renew.sh" "$ROOT/deploy/nginx/install-renewal-timer.sh"

# 校验生产模板初始化会生成随机凭据，并保持最小文件权限。
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/meta-pulse-deploy-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT
# shellcheck source=lib.sh
source "$ROOT/deploy/lib.sh"
ENV_FILE="$tmp_dir/.env"
ensure_env_file
validate_environment

for key in PULSE_DB_PASSWORD PULSE_DB_ROOT_PASSWORD FORUM_DB_PASSWORD FORUM_DB_ROOT_PASSWORD \
  PULSE_SERVICE_HMAC_SECRET PULSE_FORUM_HMAC_SECRET PULSE_USER_BFF_HMAC_SECRET PULSE_ADMIN_HMAC_SECRET PULSE_REWARD_RANDOM_SECRET; do
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

# Keep community callback and credential-isolation checks in the default test
# path even on hosts where Docker is unavailable for nginx -t.
grep -q 'access_log /dev/stdout community_no_query;' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -q 'location = /api/user-center/login/callback' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -q 'limit_req_zone $binary_remote_addr zone=community_connector:10m rate=10r/m;' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -q 'location = /answer/api/v1/user-center/agent' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -A35 'location = /answer/api/v1/user-center/agent' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -qF 'proxy_set_header Accept-Encoding "";'
grep -A40 'location = /answer/api/v1/user-center/agent' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -qF '"login_redirect_url":"/answer/api/v1/connector/login/pulse_user_center"'
grep -A40 'location = /answer/api/v1/user-center/agent' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -qF '"sign_up_redirect_url":"/users/register"'
grep -A8 'location = /answer/api/v1/user-center/login/redirect' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -q 'return 302 /answer/api/v1/connector/login/pulse_user_center;'
grep -A8 'location = /answer/api/v1/user-center/login/ {' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -q 'return 302 /answer/api/v1/connector/login/pulse_user_center;'
grep -A8 'location = /answer/api/v1/user-center/sign-up/redirect' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -q 'return 302 /users/register;'
grep -A8 'location = /answer/api/v1/user-center/sign-up/ {' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -q 'return 302 /users/register;'
grep -qF 'location ~ ^/users/(?:auth-landing|confirm-email)$' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -q 'proxy_pass http://forum/answer/api/v1/connector/redirect/pulse_user_center;' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -q 'proxy_set_header Cookie "meta_pulse_forum_flow=\$cookie_meta_pulse_forum_flow";' "$ROOT/deploy/nginx/meta-pulse.conf"
[[ "$(grep -c 'proxy_set_header Authorization "";' "$ROOT/deploy/nginx/meta-pulse.conf")" -eq 1 ]]
[[ "$(grep -c 'proxy_set_header X-Pulse-Signature "";' "$ROOT/deploy/nginx/meta-pulse.conf")" -eq 5 ]]
[[ "$(grep -c 'proxy_set_header New-Api-User "";' "$ROOT/deploy/nginx/meta-pulse.conf")" -eq 5 ]]
grep -A12 'location /blog/' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -q 'Strict-Transport-Security'
grep -q 'location = / {' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -A14 'location = / {' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -q 'try_files /blog/metar/index.html =404;'
grep -q 'location = /metar-runtime-config.js' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -q 'location ^~ /metar-assets/' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -A14 'location = / {' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -q "style-src 'self';"
! grep -A14 'location = / {' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -q 'unsafe-inline'
grep -A10 'location \^~ /metar-assets/' "$ROOT/deploy/nginx/meta-pulse.conf" | grep -q 'Cache-Control "no-cache"'
! grep -Eq 'upstream[[:space:]]+(pulse|new_api)|proxy_pass[[:space:]]+http://(pulse|new_api)' "$ROOT/deploy/nginx/meta-pulse.conf"
grep -q 'FORUM_BINDING_GUARD_DSN:' "$ROOT/docker-compose.yml"

community_dist="$tmp_dir/community"
python3 "$ROOT/metar-frontend/production/build.py" --output "$community_dist" >/dev/null
[[ -s "$community_dist/index.html" && -s "$community_dist/assets/app.js" && -s "$community_dist/assets/favicon.svg" && -s "$community_dist/runtime-config.js" ]]
! grep -R -E 'SEED_POSTS|CANDIDATES|X-Pulse-Signature|New-Api-User|未发送到线上' "$community_dist" >/dev/null

bash "$ROOT/deploy/update_test.sh"

echo '部署脚本离线测试通过'
