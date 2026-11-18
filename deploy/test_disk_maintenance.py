"""Exercise the shell entry with an isolated repository and fake lock/maintenance."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class MaintenanceTests(unittest.TestCase):
    def test_lock_and_failure_boundaries(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            deploy = repo / 'deploy'
            deploy.mkdir()
            subprocess.run(['git', 'init', '-q', tmp], check=True)
            shutil.copy(Path(__file__).with_name('maintain-disk.sh'), deploy)
            calls = repo / 'calls'
            for name in ['backup-retention.py', 'build-cache.py']:
                (deploy / name).write_text("import os, sys\nos.fstat(9)\nwith open(os.environ['CALLS'], 'a') as f: f.write(' '.join(sys.argv[1:]) + '\\n')\nsys.exit(int(os.environ.get('FAIL', '0')))\n")
            bin_dir = repo / 'bin'
            bin_dir.mkdir()
            lock = bin_dir / 'flock'
            lock.write_text('#!/bin/sh\nexit "${LOCK_BUSY:-0}"\n')
            lock.chmod(0o755)
            env = {**os.environ, 'PATH': f'{bin_dir}:{os.environ["PATH"]}', 'CALLS': str(calls), 'META_PULSE_BACKUP_KEEP': '1'}
            def run(**extra):
                return subprocess.run(['bash', str(deploy / 'maintain-disk.sh')], env={**env, **extra}, text=True, capture_output=True)
            result = run(LOCK_BUSY='1')
            self.assertEqual(result.returncode, 0)
            self.assertIn('锁忙', result.stdout)
            self.assertFalse(calls.exists())
            result = run(FAIL='7')
            self.assertEqual(result.returncode, 7)
            self.assertEqual(len(calls.read_text().splitlines()), 1)
            self.assertNotIn('维护成功', result.stdout)
            calls.unlink()
            result = run()
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(calls.read_text().splitlines(), [f'prune {repo} --keep 1', str(repo)])
            self.assertIn('维护成功', result.stdout)


if __name__ == '__main__':
    unittest.main()
