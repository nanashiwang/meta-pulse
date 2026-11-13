import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch
from types import SimpleNamespace

spec = importlib.util.spec_from_file_location('build_cache', Path(__file__).with_name('build-cache.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class CacheTests(unittest.TestCase):
    def test_bounded_cache_only(self):
        with patch.object(m, 'local_paths', return_value=[Path('/repo')]), patch.object(m, 'docker', return_value='--max-used-space --min-free-space'), patch.object(m.subprocess, 'run') as run, patch.object(m.shutil, 'disk_usage', return_value=SimpleNamespace(free=m.MIN_FREE)):
            m.maintain('/repo')
            self.assertEqual(run.call_args.args[0], ['docker', 'buildx', 'prune', '--builder', 'default', '--all', '--force', '--max-used-space', '8GB', '--min-free-space', str(m.MIN_FREE)])

    def test_old_buildx_capacity_option(self):
        with patch.object(m, 'local_paths', return_value=[]), patch.object(m, 'docker', return_value='--keep-storage'), patch.object(m.subprocess, 'run') as run:
            m.maintain('/repo')
            self.assertEqual(run.call_args.args[0][-2:], ['--keep-storage', '8GB'])

    def test_low_space_is_not_success_even_after_gc(self):
        with patch.object(m, 'local_paths', return_value=[Path('/repo'), Path('/docker')]), patch.object(m, 'docker', return_value='--keep-storage'), patch.object(m.subprocess, 'run'), patch.object(m.shutil, 'disk_usage', side_effect=[SimpleNamespace(free=m.MIN_FREE), SimpleNamespace(free=1)]):
            with self.assertRaisesRegex(RuntimeError, '/docker 空闲不足'):
                m.maintain('/repo')

    def test_check_only_never_prunes(self):
        with patch.object(m, 'local_paths', return_value=[]), patch.object(m.subprocess, 'run') as run:
            m.maintain('/repo', True)
            run.assert_not_called()

    def test_gc_failure_propagates(self):
        with patch.object(m, 'local_paths', return_value=[]), patch.object(m, 'docker', return_value='--keep-storage'), patch.object(m.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'docker')):
            with self.assertRaises(subprocess.CalledProcessError):
                m.maintain('/repo')

    def test_unsupported_cli_does_not_fall_back_to_unbounded_prune(self):
        with patch.object(m, 'local_paths', return_value=[]), patch.object(m, 'docker', return_value=''), patch.object(m.subprocess, 'run') as run:
            with self.assertRaises(RuntimeError):
                m.maintain('/repo')
            run.assert_not_called()

    def test_remote_and_overrides_refused(self):
        with patch.dict(m.os.environ, {}, clear=True), patch.object(m, 'docker', side_effect=['default', '"tcp://remote:2376"']):
            with self.assertRaisesRegex(RuntimeError, 'Unix socket'):
                m.local_paths('/repo')
        for key in ['DOCKER_HOST', 'BUILDX_BUILDER']:
            with patch.dict(m.os.environ, {key: 'custom'}, clear=True):
                with self.assertRaises(RuntimeError):
                    m.local_paths('/repo')


if __name__ == '__main__':
    unittest.main()
