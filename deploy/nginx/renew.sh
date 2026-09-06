#!/usr/bin/env bash
# 使用 Certbot webroot 续期 metar.uk，并在成功后安全重载网关。
set -Eeuo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)"
ENV_FILE="${META_PULSE_ENV_FILE:-$REPO_ROOT/.env}"
COMPOSE_FILE="$REPO_ROOT/docker-compose.yml"
OVERRIDE_FILE="${META_PULSE_COMPOSE_OVERRIDE_FILE:-$REPO_ROOT/docker-compose.override.yml}"
DRY_RUN=0

case "${1:-}" in
  "") ;;
  --dry-run) DRY_RUN=1 ;;
  *) echo "用法：$0 [--dry-run]" >&2; exit 2 ;;
esac

command -v docker >/dev/null 2>&1 || { echo '[meta-pulse] 错误：缺少 docker' >&2; exit 1; }
[[ -f "$ENV_FILE" && -f "$COMPOSE_FILE" && -f "$OVERRIDE_FILE" ]] || { echo '[meta-pulse] 错误：缺少生产 Compose 配置' >&2; exit 1; }
[[ -f /etc/letsencrypt/live/metar.uk/fullchain.pem ]] || { echo '[meta-pulse] 错误：metar.uk 证书尚未签发' >&2; exit 1; }

mkdir -p "$REPO_ROOT/.data/certbot/.well-known/acme-challenge"
chmod 755 "$REPO_ROOT/.data/certbot" "$REPO_ROOT/.data/certbot/.well-known" "$REPO_ROOT/.data/certbot/.well-known/acme-challenge"

renew_args=(renew --no-random-sleep-on-renew --webroot -w /var/www/certbot)
(( DRY_RUN == 1 )) && renew_args+=(--dry-run)
docker run --rm \
  -v /etc/letsencrypt:/etc/letsencrypt \
  -v /var/lib/letsencrypt:/var/lib/letsencrypt \
  -v /var/log/letsencrypt:/var/log/letsencrypt \
  -v "$REPO_ROOT/.data/certbot:/var/www/certbot" \
  certbot/certbot "${renew_args[@]}"

docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" -f "$OVERRIDE_FILE" exec -T gateway nginx -t
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" -f "$OVERRIDE_FILE" exec -T gateway nginx -s reload
echo '[meta-pulse] 证书检查与网关重载完成'
