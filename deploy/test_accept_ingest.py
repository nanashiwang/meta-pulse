import contextlib
import importlib.util
import io
import json
import pathlib
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('accept', pathlib.Path(__file__).with_name('accept-ingest.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class AcceptanceTest(unittest.TestCase):
    def setUp(self):
        self.before = dict(value='999:8', version=8, watermark_at='2026-09-19T00:00:00Z', lag_seconds=200)
        self.after = dict(value='1000:11', version=11, watermark_at='2026-09-19T00:00:01Z', lag_seconds=199)
        self.logs = [dict(msg='usage ingest batch completed', fetched=250, yielded=True) for _ in range(3)]

    def test_progress_is_not_caught_up(self):
        code, text = m.verdict(self.before, self.after, self.logs)
        self.assertEqual(code, 0)
        self.assertIn('不代表已追平', text)

    def test_idle_is_inconclusive_even_with_increasing_lag(self):
        after = dict(self.before, lag_seconds=99999)
        logs = [dict(msg='usage ingest batch completed', fetched=0) for _ in range(3)]
        self.assertEqual(m.verdict(self.before, after, logs)[0], 2)

    def test_missing_batches_and_same_second_are_inconclusive(self):
        self.assertEqual(m.verdict(self.before, self.after, self.logs[:2])[0], 2)
        self.assertEqual(m.verdict(self.before, dict(self.after, watermark_at='2026-09-19T00:00:00Z'), self.logs)[0], 2)
        self.assertEqual(m.verdict(self.before, self.before, self.logs)[0], 2)

    def test_failure_not_hidden_by_three_successes(self):
        self.assertEqual(m.verdict(self.before, self.after, [dict(msg='usage ingest failed')] + self.logs)[0], 1)

    def test_restart_and_regression(self):
        self.assertEqual(m.verdict(self.before, self.after, self.logs, False)[0], 2)
        self.assertEqual(m.verdict(self.after, self.before, self.logs)[0], 1)

    def test_logs_allowlist(self):
        rows = m.batches('noise\n' + json.dumps(dict(msg='usage ingest failed', error='SECRET', fetched=4)))
        self.assertEqual(rows, [dict(msg='usage ingest failed', fetched=4)])

    def test_live_orchestration_only_reads(self):
        calls = []
        def command(args):
            calls.append(args)
            if args[:2] == ['git', 'rev-parse']:
                return 'abc'
            if args[:2] == ['git', 'status']:
                return ''
            if args[:2] == ['docker', 'logs']:
                return '\n'.join(json.dumps(x) for x in self.logs)
            raise AssertionError(args)
        runtime = dict(running=True, revision='abc', batch_size=250, container='worker')
        with patch.object(m, 'run', side_effect=command), patch.object(m, 'runtime', return_value=runtime), \
             patch.object(m, 'compose', side_effect=[json.dumps(self.before), json.dumps(self.after)]) as compose, \
             patch.object(m.time, 'sleep'), patch('sys.argv', ['accept']), contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(m.main(), 0)
        self.assertEqual(compose.call_count, 2)
        for call in compose.call_args_list:
            self.assertEqual(call.args, ('exec', '-T', 'pulse-worker', 'meta-pulse-tool', 'ingest-snapshot'))
        self.assertEqual(calls[-1][:3], ['docker', 'logs', '--since'])

    def test_runtime_reads_container_not_template(self):
        data = dict(State=dict(StartedAt='now', Running=True), RestartCount=0,
                    Image='sha256:actual', Config=dict(Env=['PULSE_INGEST_BATCH_SIZE=73',
                    'NEWAPI_LOG_DSN=SECRET'], Labels={'org.opencontainers.image.revision': 'abc'}))
        with patch.object(m, 'compose', return_value='cid'), patch.object(m, 'run', return_value=json.dumps([data])):
            result = m.runtime()
        self.assertEqual(result['batch_size'], 73)
        self.assertNotIn('SECRET', json.dumps(result))

    def test_watermark_regression(self):
        self.assertEqual(m.verdict(self.before, dict(self.after, watermark_at='2026-09-18T00:00:00Z'), self.logs)[0], 1)

    def test_wrong_revision_stops_before_sampling(self):
        with patch.object(m, 'run', side_effect=['abc', '']), \
             patch.object(m, 'runtime', return_value=dict(running=True, revision='old', batch_size=250)), \
             patch.object(m, 'compose') as compose, patch('sys.argv', ['accept']), \
             contextlib.redirect_stdout(io.StringIO()), self.assertRaises(RuntimeError):
            m.main()
        compose.assert_not_called()


if __name__ == '__main__':
    unittest.main()
