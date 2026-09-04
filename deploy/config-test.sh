#!/usr/bin/env bash
# Render the actual production Compose config with public test credentials,
# then run each process' startup validator without opening network connections.
set -Eeuo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meta-pulse-config-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
cd "$ROOT"
go build -o "$tmp/meta-pulse-tool" ./services/pulse/cmd/tool
python3 - "$ROOT" "$tmp" <<'PY'
import json, os, pathlib, subprocess, sys
root, tmp = map(pathlib.Path, sys.argv[1:])
# Never read the real .env or inherit application credentials from the caller.
env = {k:v for k,v in os.environ.items() if not k.startswith(('PULSE_', 'NEWAPI_', 'FORUM_', 'COMPOSE_'))}
text = (root/'deploy/meta-pulse.env.example').read_text().replace('replace-me', 'test-only-'+'x'*48)
text = text.replace('NEWAPI_LOG_DSN=', 'NEWAPI_LOG_DSN=readonly@tcp(logs:3306)/logs', 1)
fixture = tmp/'production.env'
fixture.write_text(text)
fixture.chmod(0o600)
rendered = subprocess.run(['docker','compose','--env-file',str(fixture),'-f',str(root/'docker-compose.yml'),'config','--format','json'], env=env, check=True, capture_output=True, text=True)
services = json.loads(rendered.stdout)['services']
for role in ('api','worker'):
    configured = services['pulse-'+role]
    if configured.get('ports'):
        raise AssertionError(f'{role} must not publish host ports')
    app_env = {k:str(v) for k,v in configured['environment'].items() if v is not None}
    if role == 'worker':
        assert not app_env.get('PULSE_ADMIN_HMAC_SECRET') and not app_env.get('PULSE_USER_BFF_HMAC_SECRET'), 'worker gained unnecessary API signing keys'
    else:
        assert not app_env.get('NEWAPI_LOG_DSN'), 'API gained LOG_DB credentials'
    command = [str(tmp/'meta-pulse-tool'),'config-check','--role',role]
    result = subprocess.run(command, env={**env, **app_env}, capture_output=True, text=True)
    if result.returncode:
        raise AssertionError(f'{role} production validation failed: {result.stderr}')
    required = ['PULSE_SERVICE_HMAC_SECRET','PULSE_REWARD_RANDOM_SECRET']
    required += ['PULSE_USER_BFF_HMAC_SECRET','PULSE_ADMIN_HMAC_SECRET'] if role == 'api' else ['NEWAPI_LOG_DSN','NEWAPI_INTERNAL_BASE_URL']
    for key in required:
        invalid = {**app_env, key:''}
        assert subprocess.run(command, env={**env, **invalid}, capture_output=True).returncode != 0, f'{role} accepted missing {key}'
# Schema/read-only tooling requires no role signing or random secrets.
tool_env = {**env, 'PULSE_ENV':'production', 'PULSE_DB_DSN':'pulse@tcp(mysql:3306)/meta_pulse'}
subprocess.run([str(tmp/'meta-pulse-tool'),'config-check','--role','tool'], env=tool_env, check=True)
print('生产 Compose API/Worker/Tool 最小权限配置回归通过')
PY
