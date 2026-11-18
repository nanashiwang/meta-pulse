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
    def setUp(self):
        self.paths = patch.object(m, 'local_paths', return_value=[Path('/repo')])
        self.total = patch.object(m, 'cache_total', return_value=7 * 1000**3)
        self.disk = patch.object(m.shutil, 'disk_usage', return_value=SimpleNamespace(free=m.RECOVER_FREE))
        self.paths.start()
        self.total.start()
        self.disk.start()
        self.addCleanup(patch.stopall)

    def test_bounded_cache_only(self):
        with patch.object(m, 'docker', return_value='--max-used-space --min-free-space'), patch.object(m.subprocess, 'run') as run:
            m.maintain('/repo')
            self.assertEqual(run.call_args.args[0], ['docker', 'buildx', 'prune', '--builder', 'default', '--all', '--force', '--max-used-space', '8GB', '--min-free-space', str(m.MIN_FREE)])
            self.assertEqual(run.call_count, 1)

    def test_old_buildx_capacity_option(self):
        with patch.object(m, 'docker', return_value='--keep-storage'), patch.object(m.subprocess, 'run') as run:
            m.maintain('/repo')
            self.assertEqual(run.call_args.args[0][-2:], ['--keep-storage', '8GB'])

    def test_shared_space_triggers_full_gc_and_readback(self):
        with patch.object(m, 'cache_total', side_effect=[34530000000, 34530000000, 0]) as total, patch.object(m, 'docker', return_value='--keep-storage'), patch.object(m.subprocess, 'run') as run:
            m.maintain('/repo')
            self.assertEqual(run.call_args.args[0], ['docker', 'buildx', 'prune', '--builder', 'default', '--all', '--force'])
            self.assertEqual(run.call_count, 2)
            self.assertEqual(total.call_count, 3)

    def test_low_free_space_triggers_gc_even_with_small_cache(self):
        with patch.object(m.shutil, 'disk_usage', side_effect=[SimpleNamespace(free=25 * 1024**3), SimpleNamespace(free=m.RECOVER_FREE)]), patch.object(m, 'docker', return_value='--keep-storage'), patch.object(m.subprocess, 'run') as run:
            m.maintain('/repo')
            self.assertEqual(run.call_count, 2)

    def test_low_space_is_not_success_even_after_gc(self):
        with patch.object(m.shutil, 'disk_usage', return_value=SimpleNamespace(free=1)), patch.object(m, 'docker', return_value='--keep-storage'), patch.object(m.subprocess, 'run'):
            with self.assertRaisesRegex(RuntimeError, '/repo 空闲不足'):
                m.maintain('/repo')

    def test_cache_still_over_limit_fails(self):
        with patch.object(m, 'cache_total', return_value=20 * 1000**3), patch.object(m, 'docker', return_value='--keep-storage'), patch.object(m.subprocess, 'run'):
            with self.assertRaisesRegex(RuntimeError, '缓存仍超过'):
                m.maintain('/repo')

    def test_check_only_never_prunes_or_queries_cache(self):
        with patch.object(m.subprocess, 'run') as run, patch.object(m, 'cache_total') as total:
            m.maintain('/repo', True)
            run.assert_not_called()
            total.assert_not_called()

    def test_gc_failure_propagates(self):
        with patch.object(m, 'docker', return_value='--keep-storage'), patch.object(m.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'docker')):
            with self.assertRaises(subprocess.CalledProcessError):
                m.maintain('/repo')

    def test_unsupported_cli_does_not_fall_back_to_unbounded_prune(self):
        with patch.object(m, 'docker', return_value=''), patch.object(m.subprocess, 'run') as run:
            with self.assertRaises(RuntimeError):
                m.maintain('/repo')
            run.assert_not_called()

    def test_summary_includes_shared_and_rejects_missing_total(self):
        self.total.stop()
        with patch.object(m, 'docker', return_value='Shared:\t27.82GB\nPrivate:\t6.706GB\nReclaimable:\t34.53GB\nTotal:\t34.53GB'):
            self.assertEqual(m.cache_total(), 34530000000)
        for output in ['Total: 0B', 'Total: 12.5kB', 'Total: 1MB', 'Total: 1TB']:
            with patch.object(m, 'docker', return_value=output):
                self.assertGreaterEqual(m.cache_total(), 0)
        with patch.object(m, 'docker', return_value='unexpected output'):
            with self.assertRaisesRegex(RuntimeError, '无法读取'):
                m.cache_total()

    def test_remote_and_overrides_refused(self):
        self.paths.stop()
        with patch.dict(m.os.environ, {}, clear=True), patch.object(m, 'docker', side_effect=['default', '"tcp://remote:2376"']):
            with self.assertRaisesRegex(RuntimeError, 'Unix socket'):
                m.local_paths('/repo')
        for key in ['DOCKER_HOST', 'BUILDX_BUILDER']:
            with patch.dict(m.os.environ, {key: 'custom'}, clear=True):
                with self.assertRaises(RuntimeError):
                    m.local_paths('/repo')


if __name__ == '__main__':
    unittest.main()
