import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import ci_reuse


REPO = 'owner/repo'
SHA = 'a' * 40


def run(**changes):
    value = dict(id=10, head_sha=SHA, event='push', head_branch='main',
                 path=ci_reuse.WORKFLOW, repository={'full_name': REPO},
                 head_repository={'full_name': REPO},
                 status='completed', conclusion='success')
    value.update(changes)
    return value


class ReuseTests(unittest.TestCase):
    def resolve(self, snapshots, **options):
        elapsed = [0]
        def sleep(seconds):
            elapsed[0] += seconds
        with patch.object(ci_reuse, 'fetch_runs', side_effect=snapshots) as fetch:
            result = ci_reuse.resolve(REPO, SHA, fetch=fetch,
                                      clock=lambda: elapsed[0], sleep=sleep,
                                      wait_seconds=40, discovery_seconds=20, **options)
        return result, elapsed[0]

    def test_only_exact_commit_main_push_from_own_workflow_and_repo(self):
        for changes in [dict(head_sha='b' * 40), dict(event='pull_request'),
                        dict(head_branch='feature'), dict(path='.github/workflows/release.yml'),
                        dict(repository={'full_name': 'fork/repo'}),
                        dict(head_repository={'full_name': 'fork/repo'})]:
            with self.subTest(changes=changes):
                self.assertIsNone(ci_reuse.latest_run([run(**changes)], REPO, SHA))
        self.assertEqual(ci_reuse.latest_run([run()], REPO, SHA), run())

    def test_latest_failed_run_does_not_reuse_older_success(self):
        for conclusion in ['failure', 'cancelled', 'skipped', 'neutral', 'timed_out', None]:
            with self.subTest(conclusion=conclusion):
                result, elapsed = self.resolve([[run(), run(id=11, conclusion=conclusion)]])
                self.assertIsNone(result)
                self.assertEqual(elapsed, 0)

    def test_running_rerun_waits_instead_of_reusing_prior_success(self):
        result, elapsed = self.resolve([
            [run(id=9), run(status='in_progress', conclusion=None)], [run()]])
        self.assertEqual(result, run())
        self.assertEqual(elapsed, 20)

    def test_discovery_grace_handles_branch_and_tag_push_race(self):
        result, elapsed = self.resolve([[], [run()]])
        self.assertEqual(result, run())
        self.assertEqual(elapsed, 20)

    def test_missing_and_timed_out_runs_fall_back(self):
        for snapshots, expected in [([[], []], 20),
                                     ([[run(status='queued', conclusion=None)]] * 3, 40)]:
            result, elapsed = self.resolve(snapshots)
            self.assertIsNone(result)
            self.assertEqual(elapsed, expected)

    def test_api_permission_network_and_malformed_response_fall_back(self):
        for error in [subprocess.CalledProcessError(1, 'gh'),
                      subprocess.TimeoutExpired('gh', 30), OSError('offline'),
                      ValueError('bad JSON'), KeyError('workflow_runs'), TypeError('null')]:
            with self.subTest(error=error):
                self.assertIsNone(self.resolve([error])[0])

    def test_api_query_filters_and_timeout(self):
        with patch.object(ci_reuse.subprocess, 'run') as request:
            request.return_value.stdout = json.dumps({'workflow_runs': [run()]})
            self.assertEqual(ci_reuse.fetch_runs(REPO, SHA), [run()])
        args, kwargs = request.call_args
        self.assertIn('actions/workflows/ci.yml/runs?head_sha=' + SHA, args[0][-1])
        self.assertIn('event=push&branch=main', args[0][-1])
        self.assertEqual(kwargs['timeout'], 30)

    def test_cli_outputs_are_explicit_for_reuse_and_fallback(self):
        for result, expected in [(run(), 'true'), (None, 'false')]:
            with tempfile.TemporaryDirectory() as tmp:
                output, summary = Path(tmp) / 'output', Path(tmp) / 'summary'
                with patch.dict(os.environ, GITHUB_REPOSITORY=REPO,
                                GITHUB_OUTPUT=str(output), GITHUB_STEP_SUMMARY=str(summary)), \
                     patch.object(ci_reuse.subprocess, 'check_output', return_value=SHA + '\n'), \
                     patch.object(ci_reuse, 'resolve', return_value=result):
                    ci_reuse.main()
                self.assertEqual(output.read_text(), f'reuse={expected}\n')
                self.assertIn(SHA, summary.read_text())

    def test_workflow_publish_gate_rejects_failed_or_cancelled_paths(self):
        # Evaluate the actual restricted GitHub expression for every dependency state.
        workflow = (Path(__file__).parents[1] / '.github/workflows/release.yml').read_text()
        publish = workflow.split('  publish:\n', 1)[1]
        expression = publish.split('${{', 1)[1].split('}}', 1)[0].strip()
        for cancelled in [False, True]:
            for preflight in ['success', 'failure', 'cancelled', 'skipped']:
                for reuse in ['true', 'false', '']:
                    for verify in ['success', 'failure', 'cancelled', 'skipped']:
                        translated = expression.replace('!cancelled()', repr(not cancelled))
                        translated = translated.replace('needs.preflight.result', repr(preflight))
                        translated = translated.replace('needs.preflight.outputs.reuse-ci', repr(reuse))
                        translated = translated.replace('needs.verify.result', repr(verify))
                        translated = ' '.join(translated.replace('&&', 'and').replace('||', 'or').split())
                        actual = eval(translated, {'__builtins__': {}})
                        expected = not cancelled and preflight == 'success' and (reuse == 'true' or verify == 'success')
                        self.assertEqual(actual, expected)
        self.assertIn('needs: [preflight, verify]', publish)
        self.assertIn("if: needs.preflight.outputs.reuse-ci != 'true'", workflow)


if __name__ == '__main__':
    unittest.main()
