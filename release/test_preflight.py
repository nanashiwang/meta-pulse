from pathlib import Path
import subprocess
import tempfile
import unittest

from preflight import check


class PreflightTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git('init', '-q')
        self.git('config', 'user.name', 'Release Test')
        self.git('config', 'user.email', 'release@example.invalid')
        (self.root / 'releases').mkdir()
        self.write('VERSION', '0.2.26\n')
        self.write('releases/v0.2.26.md', '# Meta Pulse v0.2.26\n\n修复发布。\n')
        self.write('CHANGELOG.md', '## v0.2.26\n\n[说明](releases/v0.2.26.md)\n')
        self.commit()

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.root), *args], stderr=subprocess.PIPE)

    def write(self, path, value):
        (self.root / path).write_text(value)

    def commit(self):
        self.git('add', '.')
        self.git('commit', '-qm', '测试发布元数据')

    def test_committed_metadata_before_and_after_annotated_tag(self):
        self.assertEqual(check(self.root), 'v0.2.26')
        self.assertEqual(check(self.root, 'v0.2.26'), 'v0.2.26')
        self.git('tag', '-a', 'v0.2.26', '-m', '发布')
        self.assertEqual(check(self.root, 'v0.2.26', tagged=True), 'v0.2.26')

    def test_v025_failure_is_rejected_before_tag_creation(self):
        self.write('VERSION', '0.2.24\n')
        self.commit()
        with self.assertRaisesRegex(ValueError, 'VERSION=0.2.24 不一致'):
            check(self.root, 'v0.2.25')

    def test_missing_empty_non_chinese_and_wrong_title_notes(self):
        for notes, error in [(None, '缺少 releases'), ('', '版本标题'),
                             ('# Meta Pulse v0.2.26\nEnglish only', '中文更新说明'),
                             ('# Meta Pulse v0.2.25\n修复发布', '版本标题')]:
            with self.subTest(notes=notes):
                if notes is None:
                    (self.root / 'releases/v0.2.26.md').unlink()
                else:
                    self.write('releases/v0.2.26.md', notes)
                self.commit()
                with self.assertRaisesRegex(ValueError, error):
                    check(self.root, 'v0.2.26')

    def test_uncommitted_fix_cannot_hide_broken_commit(self):
        self.write('VERSION', '0.2.24\n')
        self.commit()
        self.write('VERSION', '0.2.26\n')
        with self.assertRaisesRegex(ValueError, '不一致'):
            check(self.root, 'v0.2.26')

    def test_missing_and_moved_tag(self):
        with self.assertRaisesRegex(ValueError, '缺少标签'):
            check(self.root, 'v0.2.26', tagged=True)
        self.git('tag', 'v0.2.26')
        self.write('unrelated', 'new commit')
        self.commit()
        with self.assertRaisesRegex(ValueError, '当前提交不一致'):
            check(self.root, 'v0.2.26', tagged=True)

    def test_changelog_entry_and_link_required(self):
        for content, error in [('## v0.2.25', '条目'), ('## v0.2.26', '链接')]:
            self.write('CHANGELOG.md', content)
            self.commit()
            with self.assertRaisesRegex(ValueError, error):
                check(self.root)

    def test_missing_version_and_invalid_tag(self):
        for tag in ['0.2.26', 'v00.2.26', '../v0.2.26']:
            with self.assertRaisesRegex(ValueError, '格式无效'):
                check(self.root, tag)
        (self.root / 'VERSION').unlink()
        self.commit()
        with self.assertRaisesRegex(ValueError, '缺少 VERSION'):
            check(self.root)


if __name__ == '__main__':
    unittest.main()
