import pathlib
import subprocess
import sys
import unittest

class RetiredIngestTest(unittest.TestCase):
    def test_old_command_fails_before_any_external_access(self):
        result = subprocess.run([sys.executable, str(pathlib.Path(__file__).with_name('accept-ingest.py'))], capture_output=True, text=True, env={})
        self.assertEqual(result.returncode, 1)
        self.assertIn('API 消费摄入已退役', result.stderr)

if __name__ == '__main__':
    unittest.main()
