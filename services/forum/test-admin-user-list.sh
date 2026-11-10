#!/usr/bin/env bash
# Applies the Answer patches in Dockerfile order and checks the admin user list
# patch: server page-size clamp plus the i18n keys the patched page uses.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SOURCE="$(go env GOMODCACHE)/github.com/apache/answer@v1.7.1"
if [[ ! -d "$SOURCE" ]]; then go mod download github.com/apache/answer@v1.7.1; fi
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp -R "$SOURCE/." "$WORK/"
chmod -R u+w "$WORK"
cd "$WORK"
for name in session-retention optional-admin-email admin-user-list admin-privileges-compact; do
  patch -p1 --fuzz=0 < "$ROOT/services/forum/answer-$name.patch" >/dev/null
done
GOWORK=off go test -race ./internal/service/user_admin -run TestMetar -count=1
page=ui/src/pages/Admin/Users/index.tsx
missing=0
if grep -q 'rangeStart + index' "$page"; then echo "unexpected leading row number" >&2; exit 1; fi
if ! grep -q 'user.question_count' "$page"; then echo "missing native post-count fallback" >&2; exit 1; fi
for key in $(grep -o "t(['\`]metar_[a-z_]*" "$page" | sed "s/^t(['\`]//" | grep -v '_$' | sort -u) metar_col_user_id metar_col_email metar_col_reputation metar_col_posts metar_col_api_account metar_col_login_methods metar_col_created_at metar_col_last_login metar_col_role; do
  for lang in zh_CN en_US; do
    if ! grep -q "^      $key:" "i18n/$lang.yaml"; then echo "missing $lang $key" >&2; missing=1; fi
  done
done
exit "$missing"
