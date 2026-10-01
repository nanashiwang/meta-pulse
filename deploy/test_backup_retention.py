#!/usr/bin/env python3
"""Filesystem regressions use disposable directories, never deployment data."""
import contextlib
import fcntl
import importlib.util
import io
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("retention", Path(__file__).with_name("backup-retention.py"))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


class RetentionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name).resolve()
        self.root = self.repo / '.data/deploy-backups'
        self.root.mkdir(parents=True)

    def backup(self, number, status='success'):
        name = f'20261001T{number:06d}Z-' + 'a' * 40 + f'-{number + 1}'
        path = self.root / name
        path.mkdir()
        (path / 'dump.sql').write_text('restore fixture')
        with r.directory(str(path)) as fd:
            if status is not None:
                r.write_state(fd, name, status)
        return name

    def plan(self, keep, current=''):
        with r.backup_root(str(self.repo)) as fd, contextlib.redirect_stdout(io.StringIO()):
            return r.plan(fd, keep, current)

    def finish(self, current, keep):
        with r.backup_root(str(self.repo)) as fd, contextlib.redirect_stdout(io.StringIO()):
            r.finish(fd, current, keep)

    def test_keep_boundaries_preview_and_current(self):
        names = [self.backup(i) for i in range(5)]
        self.assertEqual(self.plan(0), [])
        self.assertEqual(self.plan(5), [])
        self.assertEqual(self.plan(6), [])
        self.assertEqual(self.plan(3), names[1::-1])
        self.assertEqual(self.plan(1), names[3::-1])
        self.assertEqual(len(list(self.root.iterdir())), 5)  # preview is read-only
        current = self.backup(6, 'running')
        self.finish(current, 1)
        self.assertEqual([p.name for p in self.root.iterdir()], [current])

    def test_protected_failed_interrupted_legacy_pin_and_bad_state(self):
        protected = [self.backup(i, status) for i, status in enumerate(('failed', 'running', None))]
        pinned = self.backup(3)
        (self.root / pinned / r.PIN).touch()
        bad = self.backup(4)
        (self.root / bad / r.STATE).write_text('{broken')
        old = self.backup(5)
        current = self.backup(6, 'running')
        self.finish(current, 1)
        self.assertFalse((self.root / old).exists())
        for name in protected + [pinned, bad, current]:
            self.assertTrue((self.root / name).is_dir())

    def test_clock_rollback_still_preserves_current(self):
        newest = self.backup(9)
        current = self.backup(0, 'running')
        self.finish(current, 1)
        self.assertTrue((self.root / current).is_dir())
        self.assertFalse((self.root / newest).exists())

    def test_zero_disables_deletion(self):
        old = self.backup(0)
        current = self.backup(1, 'running')
        self.finish(current, 0)
        self.assertTrue((self.root / old).is_dir())

    def test_unsafe_paths_and_trees(self):
        outside = self.repo / 'outside'
        outside.mkdir()
        (outside / 'precious').write_text('untouched')
        for i, kind in enumerate(('symlink', 'nested', 'hardlink', 'fifo')):
            name = self.backup(i)
            path = self.root / name
            if kind == 'symlink':
                import shutil
                shutil.rmtree(path)
                path.symlink_to(outside, target_is_directory=True)
            elif kind == 'nested':
                (path / 'escape').symlink_to(outside, target_is_directory=True)
            elif kind == 'hardlink':
                os.link(outside / 'precious', path / 'hard')
            else:
                os.mkfifo(path / 'fifo')
        current = self.backup(9, 'running')
        self.finish(current, 1)
        self.assertEqual(len(list(self.root.iterdir())), 5)
        self.assertEqual((outside / 'precious').read_text(), 'untouched')
        for name in ('../outside', '/tmp', '.', '..', '20261001T000000Z-invalid-1'):
            with self.assertRaises(ValueError):
                self.finish(name, 1)
        with self.assertRaises(ValueError):
            with r.backup_root(str(self.repo / '..')):
                pass

    def test_root_and_ancestor_symlinks_rejected(self):
        self.root.rmdir()
        outside = self.repo / 'outside'
        outside.mkdir()
        self.root.symlink_to(outside, target_is_directory=True)
        with self.assertRaises(OSError):
            with r.backup_root(str(self.repo), create=True):
                pass
        self.root.unlink()
        self.root.parent.rmdir()
        self.root.parent.symlink_to(outside, target_is_directory=True)
        with self.assertRaises(OSError):
            with r.backup_root(str(self.repo), create=True):
                pass
        self.assertEqual(list(outside.iterdir()), [])

    def test_symlink_replacement_after_plan_cannot_escape(self):
        old = self.backup(0)
        current = self.backup(1, 'running')
        outside = self.repo / 'outside'
        outside.mkdir()
        (outside / 'precious').write_text('untouched')
        original_plan = r.plan
        def replace(*args):
            result = original_plan(*args)
            (self.root / old).rename(self.root / 'quarantined')
            (self.root / old).symlink_to(outside, target_is_directory=True)
            return result
        with patch.object(r, 'plan', replace), self.assertRaises(OSError):
            self.finish(current, 1)
        self.assertEqual((outside / 'precious').read_text(), 'untouched')

    def test_invalid_counts_and_wrong_state(self):
        for value in ('-1', '', '1.5', '01', '1000000', '1/../2'):
            with self.assertRaises(ValueError):
                r.keep_count(value)
        name = self.backup(0, 'failed')
        with self.assertRaises(ValueError):
            self.finish(name, 1)
        self.assertTrue((self.root / name).exists())

    def test_cleanup_error_preserves_success_and_remaining_backups(self):
        old = self.backup(0)
        current = self.backup(1, 'running')
        original = r.walk
        def fail_delete(fd, device, delete=False):
            if delete:
                raise PermissionError('simulated removal failure')
            return original(fd, device, delete)
        with patch.object(r, 'walk', fail_delete), self.assertRaises(PermissionError):
            self.finish(current, 1)
        with r.directory(str(self.root / current)) as fd:
            self.assertEqual(r.state(fd, current), 'success')
        self.assertTrue((self.root / old / 'dump.sql').is_file())

    def test_real_lock_contention(self):
        lock = self.repo / 'update.lock'
        command = ['bash', '-c', 'exec 9>"$1"; exec python3 "$2" preview "$3"',
                   'test', str(lock), str(Path(r.__file__).resolve()), str(self.repo)]
        with lock.open('w') as stream:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
            blocked = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(blocked.returncode, 0)
        self.assertEqual(subprocess.run(command, capture_output=True).returncode, 0)


if __name__ == '__main__':
    unittest.main()
