#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
config="$repo_dir/deploy/nginx/meta-pulse.conf"
compose="$repo_dir/docker-compose.yml"

die() {
  echo "gateway check failed: $*" >&2
  exit 1
}

grep -q 'location \^~ /api/pulse/' "$config" || die 'missing explicit new-api BFF route'
grep -q 'proxy_pass http://new_api;' "$config" || die 'BFF route does not target new-api'
if grep -Eq 'upstream[[:space:]]+pulse|proxy_pass[[:space:]]+http://pulse' "$config"; then
  die 'Pulse raw upstream must not be public'
fi
grep -q 'server_name forum.yourdomain.com;' "$config" || die 'forum subdomain is missing'
grep -q 'proxy_set_header Cookie \$forum_cookie;' "$config" || die 'forum Cookie allowlist is missing'
grep -q 'PULSE_FORUM_SSO_CALLBACK_URL=https://forum.yourdomain.com' "$repo_dir/deploy/nginx/README.md" || die 'fixed HTTPS callback is undocumented'

if awk '
  /^  pulse-api:/ { in_pulse=1; next }
  in_pulse && /^  [[:alnum:]_-]+:/ { in_pulse=0 }
  in_pulse && /^[[:space:]]+ports:/ { found=1 }
  END { exit found ? 0 : 1 }
' "$compose"; then
  die 'pulse-api must not publish host ports'
fi

command -v docker >/dev/null 2>&1 || die 'docker is required'
command -v openssl >/dev/null 2>&1 || die 'openssl is required'

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT INT TERM
mkdir -p "$tmp_dir/tls" "$tmp_dir/blog"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj '/CN=yourdomain.com' \
  -keyout "$tmp_dir/tls/privkey.pem" \
  -out "$tmp_dir/tls/fullchain.pem" >/dev/null 2>&1

docker run --rm \
  --add-host new-api:127.0.0.1 \
  --add-host forum:127.0.0.1 \
  -v "$config:/etc/nginx/conf.d/default.conf:ro" \
  -v "$tmp_dir/tls:/etc/nginx/tls:ro" \
  -v "$tmp_dir/blog:/var/www/blog:ro" \
  nginx:1.27-alpine nginx -t

echo 'gateway config tests passed'
