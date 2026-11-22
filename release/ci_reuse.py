#!/usr/bin/env python3
"""Reuse only the latest main-push CI for the exact release commit; otherwise rerun."""
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib.parse import urlencode


WORKFLOW = '.github/workflows/ci.yml'


def latest_run(runs, repo, sha):
    if not isinstance(runs, list) or any(not isinstance(run, dict) for run in runs):
        raise ValueError("Invalid workflow run list")
    matching = [run for run in runs
                if run.get('head_sha') == sha
                and run.get('event') == 'push'
                and run.get('head_branch') == 'main'
                and run.get('path') == WORKFLOW
                and run.get('repository', {}).get('full_name') == repo
                and run.get('head_repository', {}).get('full_name') == repo]
    return max(matching, key=lambda run: int(run['id']), default=None)


def fetch_runs(repo, sha):
    query = urlencode({'head_sha': sha, 'event': 'push', 'branch': 'main',
                       'per_page': 100})
    result = subprocess.run(
        ['gh', 'api', f'repos/{repo}/actions/workflows/ci.yml/runs?{query}'],
        check=True, capture_output=True, text=True, timeout=30)
    return json.loads(result.stdout)['workflow_runs']


def resolve(repo, sha, fetch=fetch_runs, clock=time.monotonic, sleep=time.sleep,
            wait_seconds=1800, discovery_seconds=60):
    start = clock()
    while True:
        try:
            run = latest_run(fetch(repo, sha), repo, sha)
        except (subprocess.SubprocessError, OSError, ValueError, KeyError, TypeError, AttributeError):
            print('CI 查询异常，回退完整 CI。', flush=True)
            return None
        if run and run.get('status') == 'completed':
            if run.get('conclusion') == 'success':
                print(f"复用同一提交 CI run {run['id']}。", flush=True)
                return run
            print('最新 CI 未成功，回退完整 CI。', flush=True)
            return None
        limit = wait_seconds if run else discovery_seconds
        remaining = limit - (clock() - start)
        if remaining <= 0:
            print('CI 未出现或等待超时，回退完整 CI。', flush=True)
            return None
        print('等待同一提交的主分支 CI…', flush=True)
        sleep(min(20, remaining))


def main():
    repo = os.environ['GITHUB_REPOSITORY']
    # Peel annotated tags via checkout, rather than assuming a tag object is a commit.
    sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repo):
        raise ValueError('Invalid repository')
    if not re.fullmatch(r'[0-9a-f]{40}', sha):
        raise ValueError('Invalid commit SHA')
    run = resolve(repo, sha)
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write(f"reuse={'true' if run else 'false'}\n")
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        evidence = (f"复用 [{run['id']}](https://github.com/{repo}/actions/runs/{run['id']})"
                    if run else '执行 Release 内完整 CI 补跑')
        with Path(os.environ['GITHUB_STEP_SUMMARY']).open('a') as summary:
            summary.write(f'CI 验证：`{sha}` — {evidence}。\n')


if __name__ == '__main__':
    main()
