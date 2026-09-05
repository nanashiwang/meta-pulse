#!/usr/bin/env bash
# Execute update.sh against a disposable Git repo with mocked Docker/network.
set -Eeuo pipefail
IFS=$'\n\t'
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meta-pulse-update-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
export REAL_GIT="$(command -v git)"
mkdir -p "$tmp/repo/deploy" "$tmp/mock"
cp "$ROOT/deploy/"{update.sh,lib.sh,meta-pulse.env.example} "$tmp/repo/deploy/"
cp "$ROOT/docker-compose.yml" "$tmp/repo/"
"$REAL_GIT" -C "$tmp/repo" init -q
"$REAL_GIT" -C "$tmp/repo" add deploy docker-compose.yml
"$REAL_GIT" -C "$tmp/repo" -c user.name=Test -c user.email=test@example.invalid -c commit.gpgsign=false -c core.hooksPath=/dev/null commit -qm fixture
export MOCK_LOG="$tmp/commands.log"
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
    *' merge --ff-only origin/'*) printf 'merge\n' >>"$MOCK_LOG"; exit 0 ;;
  esac
fi
exec "$REAL_GIT" "$@"
MOCK
cat >"$tmp/mock/docker" <<'MOCK'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >>"$MOCK_LOG"
case " $* " in
  *' inspect '*)
    if [[ "$*" == *'.State.Health'* ]]; then printf 'running healthy\n'; else printf 'running\n'; fi
    ;;
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
OVERRIDE
: >"$MOCK_LOG"
MOCK_FETCH_SUCCEED=1 bash "$tmp/repo/deploy/update.sh" >"$tmp/output" 2>&1 || { cat "$tmp/output" >&2; exit 1; }
grep -Eq -- '-f .*/docker-compose\.yml -f .*/docker-compose\.override\.yml config' "$MOCK_LOG"
cmp "$tmp/original.env" "$tmp/repo/.env"
awk '
 / stop -t 15 pulse-api$/{stopped=1}
 / migrate-up$/{if(!stopped) exit 1; migrated=1}
 / up -d pulse-api/{if(!migrated) exit 1; started=1}
 END {if(!stopped || !migrated || !started) exit 1}
' "$MOCK_LOG"
printf '更新配置只读、锁和原配置备份回归通过\n'
