#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
config="$repo_dir/deploy/nginx/meta-pulse.conf"
compose="$repo_dir/docker-compose.yml"

die() {
  echo "gateway check failed: $*" >&2
  exit 1
}

grep -q 'server_name metar.uk;' "$config" || die 'apex community domain is missing'
grep -q 'server_name www.metar.uk;' "$config" || die 'www canonical redirect is missing'
grep -q 'server_name metar.uk www.metar.uk;' "$config" || die 'HTTP ACME/canonical domain block is missing'
grep -q 'location ^~ /.well-known/acme-challenge/' "$config" || die 'ACME webroot route is missing'
grep -q 'return 308 https://metar.uk$request_uri;' "$config" || die 'canonical HTTPS redirect is missing'
grep -A2 'location = /blog {' "$config" | grep -q 'return 308 /blog/;' || die 'slashless blog URL is not canonicalized'
grep -q 'location = /api/user-center/login/callback' "$config" || die 'fixed new-api callback route is missing'
grep -q 'limit_req_zone $binary_remote_addr zone=community_connector:10m rate=10r/m;' "$config" || die 'connector start rate-limit zone is missing'
grep -q 'log_format community_no_query' "$config" || die 'query-free community access log format is missing'
[ "$(grep -c 'access_log /var/log/nginx/community.access.log community_no_query;' "$config")" -eq 2 ] || die 'HTTP and apex HTTPS servers must use query-free access logs'
if grep -q 'community.access.log combined' "$config"; then
  die 'default combined access log exposes sensitive query strings and referrers'
fi
grep -A4 'location = /answer/api/v1/connector/login/pulse_user_center' "$config" | grep -q 'limit_req zone=community_connector' || die 'connector start endpoint is not rate limited'
grep -qF 'location ~ ^/users/(?:auth-landing|confirm-email)$' "$config" || die 'sensitive Answer landing routes are missing'
grep -F -A4 'location ~ ^/users/(?:auth-landing|confirm-email)$' "$config" | grep -q 'access_log off;' || die 'Answer token/binding-key landing URLs are logged'
grep -q 'proxy_pass http://forum/answer/api/v1/connector/redirect/pulse_user_center;' "$config" || die 'callback does not target the Answer Connector receiver'
grep -q 'proxy_set_header Cookie "meta_pulse_forum_flow=\$cookie_meta_pulse_forum_flow";' "$config" || die 'callback browser-flow cookie allowlist is missing'
grep -q 'proxy_set_header Authorization "";' "$config" || die 'callback Authorization stripping is missing'
grep -q 'access_log off;' "$config" || die 'callback query logging is not disabled'
grep -q 'add_header Referrer-Policy "no-referrer" always;' "$config" || die 'callback referrer protection is missing'
grep -q 'proxy_set_header Cookie \$forum_cookie;' "$config" || die 'forum Cookie allowlist is missing'
[ "$(grep -c 'proxy_set_header Authorization "";' "$config")" -eq 1 ] || die 'only the callback may strip Authorization; Answer API auth must remain usable'
[ "$(grep -c 'proxy_set_header X-Pulse-Signature "";' "$config")" -eq 4 ] || die 'all forum routes must strip browser Pulse signatures'
[ "$(grep -c 'proxy_set_header New-Api-User "";' "$config")" -eq 4 ] || die 'all forum routes must strip new-api identity headers'
grep -A12 'location /blog/' "$config" | grep -q 'Strict-Transport-Security' || die 'blog location lost inherited security headers'
grep -A12 'location = /api/user-center/login/callback' "$config" | grep -q 'Strict-Transport-Security' || die 'callback location lost HSTS'
if grep -q 'proxy_set_header Cookie \$http_cookie' "$config"; then
  die 'raw browser cookies must not be forwarded to Answer'
fi
if grep -Eq 'upstream[[:space:]]+(pulse|new_api)|proxy_pass[[:space:]]+http://(pulse|new_api)' "$config"; then
  die 'community gateway must not proxy Pulse or new-api'
fi
grep -q 'PULSE_FORUM_SSO_CALLBACK_URL=https://metar.uk' "$repo_dir/deploy/nginx/README.md" || die 'fixed HTTPS callback is undocumented'

for service in mysql redis pulse-api pulse-worker forum-mysql forum; do
  if awk -v target="$service" '
    $0 ~ "^  " target ":" { inside=1; next }
    inside && /^  [[:alnum:]_-]+:/ { inside=0 }
    inside && /^[[:space:]]+ports:/ { found=1 }
    END { exit found ? 0 : 1 }
  ' "$compose"; then
    die "$service must not publish host ports"
  fi
done

grep -q 'FORUM_BINDING_GUARD_DSN:' "$compose" || die 'forum binding guard DSN is missing'
awk '/^  forum-mysql:/{inside=1; next} inside && /^  [[:alnum:]_-]+:/{inside=0} inside{print}' "$compose" | grep -q -- '--log-bin-trust-function-creators=1' || die 'forum MySQL cannot install binding guard triggers with binary logging enabled'

command -v docker >/dev/null 2>&1 || die 'docker is required'
command -v openssl >/dev/null 2>&1 || die 'openssl is required'

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT INT TERM
mkdir -p "$tmp_dir/letsencrypt/live/metar.uk" "$tmp_dir/blog"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj '/CN=metar.uk' \
  -keyout "$tmp_dir/letsencrypt/live/metar.uk/privkey.pem" \
  -out "$tmp_dir/letsencrypt/live/metar.uk/fullchain.pem" >/dev/null 2>&1

docker run --rm \
  --add-host forum:127.0.0.1 \
  -v "$config:/etc/nginx/conf.d/default.conf:ro" \
  -v "$tmp_dir/letsencrypt:/etc/letsencrypt:ro" \
  -v "$tmp_dir/blog:/var/www/blog:ro" \
  -v "$tmp_dir/blog:/var/www/certbot:ro" \
  nginx:1.27-alpine nginx -t

echo 'gateway config tests passed'
