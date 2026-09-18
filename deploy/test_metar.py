"""Exercise the launcher and menu without Docker, production files or network."""
import json
import os
from pathlib import Path
import pty
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class MetarTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='metar-cli-')
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name).resolve()
        self.repo = self.base / 'repo with spaces'
        self.deploy = self.repo / 'deploy'
        self.deploy.mkdir(parents=True)
        for name in ('metar.sh', 'lib.sh', 'install-cli.sh'):
            shutil.copy2(ROOT / 'deploy' / name, self.deploy / name)
        (self.repo / '.env').write_text('PULSE_ENV=production\n')
        (self.repo / 'docker-compose.yml').write_text('services: {}\n')
        (self.repo / 'docker-compose.override.yml').write_text('services: {}\n')
        subprocess.run(['git', 'init', '-q', str(self.repo)], check=True)
        self.mock = self.base / 'mock'
        self.mock.mkdir()
        self.log = self.base / 'calls.jsonl'
        self.env = {**os.environ, 'PATH': f'{self.mock}:{os.environ["PATH"]}',
                    'MOCK_LOG': str(self.log)}
        for key in list(self.env):
            if key.startswith(('META_PULSE_', 'PULSE_', 'NEWAPI_', 'FORUM_')):
                del self.env[key]
        self.script(self.mock / 'uname', '#!/bin/sh\necho Linux\n')
        self.script(self.mock / 'flock', '#!/bin/sh\n[ "${FAIL_LOCK:-0}" = 0 ]\n')
        self.script(self.mock / 'docker', '''#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
with open(os.environ['MOCK_LOG'], 'a') as f:
 f.write(json.dumps({'args': args, 'pulse': os.getenv('PULSE_DB_PASSWORD')})+'\\n')
if os.getenv('FAIL_ACTION') in args: sys.exit(42)
if args[0] == 'inspect': print('running healthy')
elif '--services' in args: print('pulse-api\\npulse-worker\\nforum\\ngateway')
elif 'ps' in args and '-q' in args: print('fixture-container')
''')
        self.script(self.deploy / 'update.sh', '''#!/usr/bin/env python3
import json, os, sys
print(json.dumps({'args': sys.argv[1:], 'env': os.environ.get('META_PULSE_ENV_FILE')}))
''')

    def script(self, path, content):
        path.write_text(content)
        path.chmod(0o755)

    def run_cli(self, *args, env=None):
        return subprocess.run([str(self.deploy / 'metar.sh'), *args], cwd=self.base,
                              env={**self.env, **(env or {})}, capture_output=True, text=True)

    def calls(self):
        return [json.loads(line)['args'] for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def test_update_forwards_arguments_and_custom_env(self):
        result = self.run_cli('--env-file', 'custom.env', 'update', '--ref', 'main', '--skip-forum')
        self.assertEqual(result.returncode, 0, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['args'], ['--ref', 'main', '--skip-forum'])
        self.assertEqual(data['env'], str(self.base / 'custom.env'))
        self.assertFalse(self.calls())

    def test_lifecycle_checks_health_and_preserves_overrides(self):
        for action in ('start', 'restart', 'stop', 'status'):
            result = self.run_cli(action, env={'PULSE_DB_PASSWORD': 'must-not-override'})
            self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls()
        self.assertTrue(any('up' in c and '--no-build' in c for c in calls))
        self.assertTrue(any('exec' in c and 'http://127.0.0.1:8088/readyz' in c for c in calls))
        for line in self.log.read_text().splitlines():
            call = json.loads(line)
            if call['args'][0] == 'compose' and '--env-file' in call['args']:
                self.assertIsNone(call['pulse'])
                self.assertIn(str(self.repo / 'docker-compose.override.yml'), call['args'])

    def test_failure_and_lock_conflict_do_not_report_success(self):
        for action in ('start', 'restart', 'stop'):
            result = self.run_cli(action, env={'FAIL_ACTION': 'up' if action == 'start' else action})
            self.assertEqual(result.returncode, 42)
            self.assertNotIn('服务健康检查通过', result.stdout)
        self.log.unlink()
        result = self.run_cli('stop', env={'FAIL_LOCK': '1'})
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any('stop' in c for c in self.calls()))

    def test_uninstall_requires_confirmation_and_never_removes_volumes(self):
        self.assertNotEqual(self.run_cli('uninstall').returncode, 0)
        self.assertFalse(any('down' in c for c in self.calls()))
        self.assertEqual(self.run_cli('uninstall', '--yes').returncode, 0)
        down = [c for c in self.calls() if 'down' in c]
        self.assertEqual(len(down), 1)
        self.assertEqual(down[0][-1], 'down')
        self.assertTrue((self.repo / '.env').exists())

    def test_logs_validate_service_and_options(self):
        self.assertEqual(self.run_cli('logs', 'pulse-worker', '-f').returncode, 0)
        self.assertIn(['logs', '--tail=100', '--follow', 'pulse-worker'], [c[-4:] for c in self.calls()])
        self.assertNotEqual(self.run_cli('logs', 'new-api').returncode, 0)
        self.assertNotEqual(self.run_cli('uninstall', '--volumes').returncode, 0)
        self.assertNotEqual(self.run_cli('stop', '--volumes').returncode, 0)

    def test_installed_launcher_tracks_repo_and_refuses_unrelated_file(self):
        bindir = self.base / 'bin'
        args = ['bash', str(self.deploy / 'install-cli.sh'), '--bin-dir', str(bindir)]
        subprocess.run(args, check=True, capture_output=True)
        subprocess.run(args, check=True, capture_output=True)
        result = subprocess.run([str(bindir / 'metar'), 'help'], cwd='/', capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('数字菜单', result.stdout)
        self.script(self.deploy / 'metar.sh', '#!/bin/bash\necho updated-launcher\n')
        result = subprocess.run([str(bindir / 'metar')], capture_output=True, text=True)
        self.assertEqual(result.stdout.strip(), 'updated-launcher')
        (bindir / 'metar').write_text('unrelated command')
        self.assertNotEqual(subprocess.run(args, capture_output=True).returncode, 0)
        self.assertEqual((bindir / 'metar').read_text(), 'unrelated command')

    def test_menu_recovers_after_failure_and_exits(self):
        master, slave = pty.openpty()
        try:
            proc = subprocess.Popen([str(self.deploy / 'metar.sh')], stdin=slave,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                    env={**self.env, 'FAIL_ACTION': 'restart'})
            os.write(master, b'9\n2\n5\n0\n')
            output = proc.communicate(timeout=10)[0].decode()
            self.assertEqual(proc.returncode, 0, output)
            self.assertIn('请输入 0 到 7', output)
            self.assertIn('操作失败（退出码 42）', output)
            self.assertTrue(any('ps' in c for c in self.calls()))
        finally:
            os.close(master)
            os.close(slave)


if __name__ == '__main__':
    unittest.main()
