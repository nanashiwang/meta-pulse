"""A mounted/open directory must remain readable across rebuild and failure."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class BlogBuildTest(unittest.TestCase):
    def test_live_directory_survives_success_failure_and_retry(self):
        for fail in (False, True):
            with self.subTest(fail=fail), tempfile.TemporaryDirectory() as tmp:
                repo = Path(tmp) / 'repo with spaces'
                deploy = repo / 'deploy'
                deploy.mkdir(parents=True)
                shutil.copy2(ROOT / 'deploy/build-blog.sh', deploy / 'build-blog.sh')
                blog = repo / 'sites/blog'
                dist = blog / 'docs/.vitepress/dist'
                (dist / 'metar').mkdir(parents=True)
                (blog / 'package-lock.json').write_text('{}')
                (dist / 'index.html').write_text('old blog')
                (dist / 'metar/index.html').write_text('old community')
                mock = repo / 'mock'
                mock.mkdir()
                docker = mock / 'docker'
                docker.write_text('''#!/usr/bin/env python3
import os, pathlib, sys
if sys.argv[1] == 'info': sys.exit(0)
p = pathlib.Path(os.environ['MOCK_DIST'])
p.mkdir(parents=True, exist_ok=True)
(p / 'index.html').write_text('new blog')
if os.environ['MOCK_FAIL'] == '1': sys.exit(23)
''')
                docker.chmod(0o755)
                env = {**os.environ, 'PATH': str(mock) + ':' + os.environ['PATH'],
                       'MOCK_DIST': str(dist), 'MOCK_FAIL': '1' if fail else '0'}
                # Like a bind mount, this handle references the old directory
                # inode even when the path is renamed. Unlinking its children
                # would make subsequent requests fail.
                handle = os.open(dist, os.O_RDONLY)
                try:
                    for _ in range(2):
                        result = subprocess.run(['bash', str(deploy / 'build-blog.sh')],
                                                env=env, capture_output=True, text=True)
                        self.assertEqual(result.returncode, 23 if fail else 0, result.stderr)
                        for path, expected in [('index.html', b'old blog'),
                                               ('metar/index.html', b'old community')]:
                            fd = os.open(path, os.O_RDONLY, dir_fd=handle)
                            with os.fdopen(fd, 'rb') as old:
                                self.assertEqual(old.read(), expected)
                        self.assertEqual((dist / 'index.html').read_text(), 'new blog')
                finally:
                    os.close(handle)


if __name__ == '__main__':
    unittest.main()
