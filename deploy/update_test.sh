#!/usr/bin/env bash
# Execute update.sh against a disposable Git repo with mocked Docker/network.
set -Eeuo pipefail
IFS=$'\n\t'
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meta-pulse-update-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
export REAL_GIT="$(command -v git)"
mkdir -p "$tmp/repo/deploy/nginx" "$tmp/repo/metar-frontend/src" "$tmp/mock"
cp "$ROOT/deploy/"{update.sh,lib.sh,meta-pulse.env.example} "$tmp/repo/deploy/"
printf 'server {}\n' >"$tmp/repo/deploy/nginx/meta-pulse.conf"
printf 'body{}\n' >"$tmp/repo/metar-frontend/src/styles.css"
cat >"$tmp/repo/deploy/build-community.sh" <<'BUILD'
#!/usr/bin/env bash
printf 'build-community\n' >>"$MOCK_LOG"
BUILD
cat >"$tmp/repo/deploy/build-blog.sh" <<'BUILD'
#!/usr/bin/env bash
printf 'build-blog\n' >>"$MOCK_LOG"
BUILD
chmod +x "$tmp/repo/deploy/build-community.sh" "$tmp/repo/deploy/build-blog.sh"
cp "$ROOT/docker-compose.yml" "$tmp/repo/"
"$REAL_GIT" -C "$tmp/repo" init -q
"$REAL_GIT" -C "$tmp/repo" add deploy docker-compose.yml
"$REAL_GIT" -C "$tmp/repo" -c user.name=Test -c user.email=test@example.invalid -c commit.gpgsign=false -c core.hooksPath=/dev/null commit -qm fixture
export MOCK_LOG="$tmp/commands.log"
export MOCK_REPO="$tmp/repo"
cat >"$tmp/mock/uname" <<'MOCK'
#!/usr/bin/env bash
printf 'Linux\n'
MOCK
cat >"$tmp/mock/flock" <<'MOCK'
#!/usr/bin/env bash
printf 'lock\n' >>"$MOCK_LOG"
[[ "${MOCK_FLOCK_FAIL:-0}" != 1 ]]
MOCK
cat >"$tmp/mock/git" <<'MOCK'
#!/usr/bin/env bash
for arg in "$@"; do
  if [[ "$arg" == fetch ]]; then
    printf 'fetch\n' >>"$MOCK_LOG"
    if [[ "${MOCK_FETCH_SUCCEED:-0}" == 1 ]]; then exit 0; fi
    exit 42  # Stop before any real network/Git update.
  fi
done
if [[ "${MOCK_FETCH_SUCCEED:-0}" == 1 ]]; then
  case " $* " in
    *' show-ref --verify --quiet refs/remotes/origin/'*) exit 0 ;;
    *' merge --ff-only origin/'*)
      changed=0
      if [[ "${MOCK_NGINX_CHANGED:-0}" == 1 ]]; then
        printf '# updated\n' >>"$MOCK_REPO/deploy/nginx/meta-pulse.conf"
        changed=1
      fi
      if [[ "${MOCK_COMMUNITY_CHANGED:-0}" == 1 ]]; then
        printf '/* updated */\n' >>"$MOCK_REPO/metar-frontend/src/styles.css"
        changed=1
      fi
      if [[ "$changed" == 1 ]]; then
        "$REAL_GIT" -C "$MOCK_REPO" add deploy/nginx/meta-pulse.conf metar-frontend/src/styles.css
        "$REAL_GIT" -C "$MOCK_REPO" -c user.name=Test -c user.email=test@example.invalid -c commit.gpgsign=false commit -qm fixture-update
      fi
      printf 'merge\n' >>"$MOCK_LOG"
      exit 0
      ;;
  esac
fi
exec "$REAL_GIT" "$@"
MOCK
cat >"$tmp/mock/docker" <<'MOCK'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >>"$MOCK_LOG"
case " $* " in
  *' config --services '*)
    [[ "${MOCK_GATEWAY:-0}" == 1 ]] && printf 'gateway\n'
    ;;
  *' inspect '*)
    if [[ "$*" == *'org.opencontainers.image.revision'* ]]; then
      if [[ "${MOCK_WRONG_REVISION:-0}" == 1 ]]; then printf 'old-image\n'; else "$REAL_GIT" -C "$MOCK_REPO" rev-parse HEAD; fi
    elif [[ "$*" == *'org.opencontainers.image.version'* ]]; then
      cat "$MOCK_REPO/VERSION"
    elif [[ "$*" == *'.Mounts'* ]]; then
      [[ "${MOCK_RUNTIME_KEYS:-0}" == 1 ]] && printf 'volume\n'
    elif [[ "$*" == *'.State.Health'* ]]; then printf 'running healthy\n'; else printf 'running\n'; fi
    ;;
  *' cp '*)
    [[ "${MOCK_RUNTIME_COPY_FAIL:-0}" == 0 ]] || exit 45
    printf 'public-test-private-key-fixture' >"${@: -1}/role.key"
    chmod 600 "${@: -1}/role.key"
    ;;
  *' ps -a -q '*) printf 'fixture-container\n' ;;
  *' ps -q '*) printf 'fixture-container\n' ;;
  *' config '*)
    [[ -z "${NEWAPI_LOG_DSN+x}" && -z "${PULSE_DB_PASSWORD+x}" ]] || exit 43
    printf 'services: {}\n'
    ;;
esac
exit 0
MOCK
chmod +x "$tmp/mock/"*
export PATH="$tmp/mock:$PATH"
# Missing configuration must not be silently recreated.
if bash "$tmp/repo/deploy/update.sh" --skip-worker >"$tmp/output" 2>&1; then
  echo 'missing configuration accepted' >&2; exit 1
fi
[[ ! -e "$tmp/repo/.env" ]]
grep -q '更新需要已有配置' "$tmp/output" || { cat "$tmp/output" >&2; exit 1; }

# Build a fixture using install's initializer, never the user's real env.
(
  source "$tmp/repo/deploy/lib.sh"
  ensure_env_file
  set_env_value NEWAPI_LOG_DSN 'readonly@tcp(logs:3306)/logs'
) > /dev/null
cp "$tmp/repo/.env" "$tmp/original.env"

# Empty secrets and placeholder passwords must fail without repairing the file.
for entry in 'PULSE_REWARD_RANDOM_SECRET=' 'PULSE_DB_PASSWORD=replace-me'; do
  key="${entry%%=*}"; value="${entry#*=}"
  ( source "$tmp/repo/deploy/lib.sh"; set_env_value "$key" "$value" )
  cp "$tmp/repo/.env" "$tmp/before.env"
  if bash "$tmp/repo/deploy/update.sh" >"$tmp/output" 2>&1; then echo 'invalid config accepted' >&2; exit 1; fi
  cmp "$tmp/before.env" "$tmp/repo/.env"
  [[ ! -d "$tmp/repo/.data" ]]
  cp "$tmp/original.env" "$tmp/repo/.env"
done

# A competing updater must exit before any config/backup mutation.
if MOCK_FLOCK_FAIL=1 bash "$tmp/repo/deploy/update.sh" >"$tmp/output" 2>&1; then echo 'lock failure ignored' >&2; exit 1; fi
grep -q '已有另一个' "$tmp/output"
cmp "$tmp/original.env" "$tmp/repo/.env"
[[ ! -d "$tmp/repo/.data" ]]

# Shell values cannot replace the file or leak into effective Compose config.
: >"$MOCK_LOG"
if NEWAPI_LOG_DSN='untrusted-shell-dsn' PULSE_DB_PASSWORD='untrusted-shell-password' bash "$tmp/repo/deploy/update.sh" >"$tmp/output" 2>&1; then
  echo 'mock fetch failure ignored' >&2; exit 1
fi
grep -q '^fetch$' "$MOCK_LOG"
backup="$(find "$tmp/repo/.data/deploy-backups" -name .env -type f)"
[[ -n "$backup" ]]
cmp "$tmp/original.env" "$backup"
cmp "$tmp/original.env" "$tmp/repo/.env"
# The lock precedes the first Compose config rendering (not just Git fetch).
awk '/^lock$/{locked=1} / config$/{if(!locked) exit 1}' "$MOCK_LOG"
# Successful rollout must include the host-local Compose override and drain all
# old API writers before migration/restart.
cat >"$tmp/repo/docker-compose.override.yml" <<'OVERRIDE'
services:
  pulse-api:
    ports:
      - "10.77.0.2:8088:8088"
  gateway:
    image: nginx:1.27-alpine
OVERRIDE
mkdir -p "$tmp/repo/sites/blog/docs/.vitepress/dist/metar"
printf '<!doctype html>' >"$tmp/repo/sites/blog/docs/.vitepress/dist/index.html"
printf '<!doctype html>' >"$tmp/repo/sites/blog/docs/.vitepress/dist/metar/index.html"
: >"$MOCK_LOG"
MOCK_GATEWAY=1 MOCK_FETCH_SUCCEED=1 bash "$tmp/repo/deploy/update.sh" >"$tmp/output" 2>&1 || { cat "$tmp/output" >&2; exit 1; }
grep -Eq -- '-f .*/docker-compose\.yml -f .*/docker-compose\.override\.yml config' "$MOCK_LOG"
cmp "$tmp/original.env" "$tmp/repo/.env"
awk '
 / stop -t 15 pulse-api$/{stopped=1}
 / migrate-up$/{if(!stopped) exit 1; migrated=1}
 / up -d pulse-api/{if(!migrated) exit 1; started=1}
 END {if(!stopped || !migrated || !started) exit 1}
' "$MOCK_LOG"
grep -q 'run --rm --no-deps --entrypoint nginx gateway -t' "$MOCK_LOG"
grep -q 'up -d gateway' "$MOCK_LOG"
grep -q 'exec -T gateway nginx -s reload' "$MOCK_LOG"
# 直接挂载的 Nginx 文件在 Git 快进后可能更换 inode；配置变更时必须重建网关，不能只 reload。
: >"$MOCK_LOG"
MOCK_GATEWAY=1 MOCK_FETCH_SUCCEED=1 MOCK_NGINX_CHANGED=1 bash "$tmp/repo/deploy/update.sh" >"$tmp/output" 2>&1 || { cat "$tmp/output" >&2; exit 1; }
grep -q 'up -d --force-recreate --no-deps gateway' "$MOCK_LOG"
grep -q '检测到网关配置或博客目录变更' "$tmp/output"

# 正式前端复用原型视觉 CSS；该文件变化也必须触发 METAR 重建。
: >"$MOCK_LOG"
MOCK_GATEWAY=1 MOCK_FETCH_SUCCEED=1 MOCK_COMMUNITY_CHANGED=1 bash "$tmp/repo/deploy/update.sh" >"$tmp/output" 2>&1 || { cat "$tmp/output" >&2; exit 1; }
grep -q '^build-community$' "$MOCK_LOG"
! grep -q '^build-blog$' "$MOCK_LOG"

# Back up private role key mounts even for stopped containers, and never
# continue an upgrade when an existing key volume cannot be copied.
: >"$MOCK_LOG"
MOCK_RUNTIME_KEYS=1 MOCK_GATEWAY=1 MOCK_FETCH_SUCCEED=1 bash "$tmp/repo/deploy/update.sh" >"$tmp/output" 2>&1 || { cat "$tmp/output" >&2; exit 1; }
grep -q 'runtime-keys/api/' "$MOCK_LOG"
grep -q 'runtime-keys/worker/' "$MOCK_LOG"
: >"$MOCK_LOG"
if MOCK_RUNTIME_KEYS=1 MOCK_RUNTIME_COPY_FAIL=1 bash "$tmp/repo/deploy/update.sh" >"$tmp/output" 2>&1; then
  echo 'runtime key backup failure ignored' >&2; exit 1
fi
! grep -q '^fetch$' "$MOCK_LOG"

# Release upgrades require publication, pin the commit and reject stale images.
cat >"$tmp/repo/deploy/verify-release.py" <<'VERIFY'
import os, sys
with open(os.environ['MOCK_LOG'], 'a') as f:
    f.write('verify-release\n')
if os.environ.get('MOCK_RELEASE_FAIL') == '1':
    sys.exit(41)
VERIFY
printf '0.1.0\n' >"$tmp/repo/VERSION"
"$REAL_GIT" -C "$tmp/repo" add VERSION deploy/verify-release.py
"$REAL_GIT" -C "$tmp/repo" -c user.name=Test -c user.email=test@example.invalid -c commit.gpgsign=false commit -qm release-fixture
"$REAL_GIT" -C "$tmp/repo" tag v0.1.0
: >"$MOCK_LOG"
MOCK_GATEWAY=1 MOCK_FETCH_SUCCEED=1 bash "$tmp/repo/deploy/update.sh" --release v0.1.0 >"$tmp/output" 2>&1 || { cat "$tmp/output" >&2; exit 1; }
grep -q '^verify-release$' "$MOCK_LOG"
grep -q '^build-blog$' "$MOCK_LOG"
grep -q '^build-community$' "$MOCK_LOG"
grep -q '^v0.1.0$' "$tmp/repo/.data/deployed-release.txt"
# A release rebuild replaces the blog dist directory inode. A plain reload
# cannot refresh Docker's bind mount, even when nginx.conf did not change.
grep -q 'up -d --force-recreate --no-deps gateway' "$MOCK_LOG"
cmp "$tmp/original.env" "$tmp/repo/.env"
for failure in MOCK_RELEASE_FAIL MOCK_WRONG_REVISION; do
  : >"$MOCK_LOG"
  if env "$failure=1" MOCK_GATEWAY=1 MOCK_FETCH_SUCCEED=1 bash "$tmp/repo/deploy/update.sh" --release v0.1.0 >"$tmp/output" 2>&1; then
    echo "release validation ignored: $failure" >&2; exit 1
  fi
  if [[ "$failure" == MOCK_RELEASE_FAIL ]]; then ! grep -q ' stop -t 15 pulse-api' "$MOCK_LOG"; fi
done
printf '0.1.1\n' >"$tmp/repo/VERSION"
"$REAL_GIT" -C "$tmp/repo" add VERSION
"$REAL_GIT" -C "$tmp/repo" -c user.name=Test -c user.email=test@example.invalid -c commit.gpgsign=false commit -qm next-fixture
before="$("$REAL_GIT" -C "$tmp/repo" rev-parse HEAD)"
if MOCK_FETCH_SUCCEED=1 bash "$tmp/repo/deploy/update.sh" --release v0.1.0 >"$tmp/output" 2>&1; then
  echo 'release silently accepted a newer checkout' >&2; exit 1
fi
grep -q '当前代码超前或分叉' "$tmp/output"
[[ "$("$REAL_GIT" -C "$tmp/repo" rev-parse HEAD)" == "$before" ]]
printf '更新配置只读、锁、备份、网关和指定发布版本回归通过\n'
