#!/usr/bin/env python3
"""Publish missing draft assets, or verify an existing published snapshot."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

from published import verify

ROOT = Path(__file__).resolve().parents[1]


def output(*args):
    return subprocess.check_output(args, text=True).strip()


def api(path):
    return json.loads(output('gh', 'api', path))


def release_state(repo, tag):
    result = subprocess.run(['gh', 'api', f'repos/{repo}/releases/tags/{tag}'],
                            capture_output=True, text=True)
    if result.returncode == 0:
        return json.loads(result.stdout)
    # Authentication, rate limits and network failures are not absence.
    if '(HTTP 404)' in result.stderr:
        # The tag endpoint returns published releases only. Drafts are visible
        # in the authenticated collection, including older paginated entries.
        pages = json.loads(output('gh', 'api', '--paginate', '--slurp',
                                  f'repos/{repo}/releases?per_page=100'))
        matches = [item for page in pages for item in page if item.get('tag_name') == tag]
        if len(matches) > 1:
            raise ValueError('同一标签有多个 Release，拒绝选择或覆盖')
        return matches[0] if matches else None
    raise RuntimeError(f'无法读取 Release：{result.stderr.strip()}')


def require_release(repo, tag):
    state = release_state(repo, tag)
    if state is None or type(state.get('draft')) is not bool:
        raise ValueError('Release 消失或状态无效，停止发布')
    return state


def wait_for_release(repo, tag):
    for attempt in range(5):
        state = release_state(repo, tag)
        if state is not None:
            if type(state.get('draft')) is not bool:
                raise ValueError('Release 状态无效，停止发布')
            return state
        if attempt < 4:
            time.sleep(attempt + 1)
    raise ValueError('新建 Release 尚未可读，停止写入并等待人工重试')


def tag_commit(repo, tag):
    # Resolve the exact tag ref; commits/<tag> may be ambiguous with a branch.
    obj = api(f'repos/{repo}/git/ref/tags/{tag}')['object']
    for _ in range(16):
        if obj['type'] == 'commit':
            return obj['sha']
        if obj['type'] != 'tag':
            break
        obj = api(f'repos/{repo}/git/tags/{obj["sha"]}')['object']
    raise ValueError('远程标签未解析到 commit')


def snapshot(state):
    if state is None:
        return None
    # Download counters are expected to change because of this readback.
    return (tuple(state.get(key) for key in ('id', 'tag_name', 'draft', 'published_at')),
            sorted((tuple(item.get(key) for key in
                          ('id', 'name', 'state', 'size', 'digest', 'updated_at'))
                    for item in state.get('assets', [])), key=lambda row: str(row[1])))


def verify_remote(repo, tag, commit, state):
    with tempfile.TemporaryDirectory(prefix='meta-pulse-release-') as tmp:
        directory = Path(tmp)
        subprocess.run(['gh', 'release', 'download', tag, '--repo', repo,
                        '--dir', tmp], check=True)
        verify(directory, tag, commit, tag_commit(repo, tag), state)
        # Detect publication/asset changes during the download, including replacements
        # with identical bytes but different asset IDs.
        if snapshot(release_state(repo, tag)) != snapshot(state):
            raise ValueError('核验期间 Release 发生变化，请重新只读核验')
    print(f'已完成：{tag}，所有公开附件保持原样')


def publish(tag, build_if_needed=False):
    repo = os.environ.get('GH_REPO') or output('gh', 'repo', 'view', '--json', 'nameWithOwner', '--jq', '.nameWithOwner')
    commit = output('git', 'rev-parse', 'HEAD')
    if tag_commit(repo, tag) != commit:
        raise ValueError('远程标签与当前提交不一致')
    state = release_state(repo, tag)
    if state is not None and state.get('draft') is False:
        verify_remote(repo, tag, commit, state)
        return
    if state is not None and state.get('draft') is not True:
        raise ValueError('Release 状态无效')
    directory = ROOT / 'dist' / f'release-{tag}'
    if build_if_needed and not directory.exists():
        subprocess.run([str(ROOT / 'release/build.sh'), tag], check=True)
    # Verify the complete local bundle before any remote mutation. The same
    # structural/hash checks apply, with local metadata instead of API digests.
    import hashlib
    inventory = [{'name': p.name, 'size': p.stat().st_size, 'state': 'uploaded',
                  'digest': 'sha256:' + hashlib.sha256(p.read_bytes()).hexdigest()}
                 for p in directory.iterdir() if p.is_file()]
    verify(directory, tag, commit, commit,
           {'tag_name': tag, 'draft': False, 'assets': inventory})
    local = {entry['name']: entry for entry in inventory}
    try:
        # Re-read after building: a manual publisher may already have finished.
        state = release_state(repo, tag)
        if state is not None and state.get('draft') is False:
            verify_remote(repo, tag, commit, state)
            return
        if state is None:
            try:
                subprocess.run(['gh', 'release', 'create', tag, '--repo', repo,
                                '--verify-tag', '--draft', '--title', f'Meta Pulse {tag}',
                                '--notes-file', str(ROOT / 'releases' / f'{tag}.md')], check=True)
            except subprocess.CalledProcessError:
                state = wait_for_release(repo, tag)
                if state.get('draft') is False:
                    verify_remote(repo, tag, commit, state)
                    return
                if state.get('draft') is not True:
                    raise ValueError('并发创建后 Release 状态无效')
            else:
                state = wait_for_release(repo, tag)
        else:
            state = require_release(repo, tag)
        if state.get('draft') is False:
            verify_remote(repo, tag, commit, state)
            return
        seen = set()
        for entry in state['assets']:
            name = entry['name']
            if (name not in local or name in seen or entry.get('state') != 'uploaded' or
                    any(entry.get(key) != local[name][key] for key in ('size', 'digest'))):
                raise ValueError(f'草稿已有附件与本次构建冲突：{name}；禁止覆盖，请人工核对或发布新版本')
            seen.add(name)
        for name in sorted(set(local) - seen):
            state = require_release(repo, tag)
            if state.get('draft') is False:
                verify_remote(repo, tag, commit, state)
                return
            # Never --clobber: a competing upload must fail rather than delete.
            try:
                subprocess.run(['gh', 'release', 'upload', tag, str(directory / name),
                                '--repo', repo], check=True)
            except subprocess.CalledProcessError:
                state = release_state(repo, tag)
                if state is not None and state.get('draft') is False:
                    verify_remote(repo, tag, commit, state)
                    return
                uploaded = next((item for item in (state or {}).get('assets', [])
                                 if item.get('name') == name), None)
                if (uploaded is None or uploaded.get('state') != 'uploaded' or
                        any(uploaded.get(key) != local[name][key] for key in ('size', 'digest'))):
                    raise ValueError(f'并发补传附件冲突：{name}；未覆盖远端附件')
        state = require_release(repo, tag)
        if state.get('draft') is False:
            verify_remote(repo, tag, commit, state)
            return
        with tempfile.TemporaryDirectory(prefix='meta-pulse-draft-') as tmp:
            subprocess.run(['gh', 'release', 'download', tag, '--repo', repo, '--dir', tmp], check=True)
            verify(Path(tmp), tag, commit, tag_commit(repo, tag), dict(state, draft=False))
            for name in local:
                if (Path(tmp) / name).read_bytes() != (directory / name).read_bytes():
                    raise ValueError(f'草稿回读与本次构建不一致：{name}')
        verified_state = state
        state = require_release(repo, tag)
        if state.get('draft') is False:
            verify_remote(repo, tag, commit, state)
            return
        if snapshot(state) != snapshot(verified_state) or tag_commit(repo, tag) != commit:
            raise ValueError('发布前草稿或标签发生变化，拒绝公开')
        subprocess.run(['gh', 'release', 'edit', tag, '--repo', repo, '--draft=false',
                        '--latest'], check=True)
    except subprocess.CalledProcessError:
        # A competing publisher or a lost response is success only after a full
        # readback of the published release. Partial drafts remain failures.
        state = release_state(repo, tag)
        if state is None or state.get('draft') is not False:
            raise
    state = release_state(repo, tag)
    verify_remote(repo, tag, commit, state)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('tag')
    parser.add_argument('--build-if-needed', action='store_true')
    args = parser.parse_args()
    try:
        publish(args.tag, args.build_if_needed)
    except (ValueError, RuntimeError, OSError, KeyError, TypeError, subprocess.CalledProcessError) as exc:
        raise SystemExit(f'发布失败（未覆盖已有附件）：{exc}') from exc
