"""Exercise the real shell entrypoint against a local GitHub CLI double."""
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest

from manifest import assets, create

ROOT = Path(__file__).resolve().parents[1]
TAG = 'v0.1.0'

FAKE_GH = r'''#!/usr/bin/env python3
import json, os, pathlib, shutil, sys
root = pathlib.Path(os.environ['FAKE_REMOTE'])
args = sys.argv[1:]
with (root / 'calls').open('a') as f: f.write(json.dumps(args) + '\n')
state = json.loads((root / 'state.json').read_text())
mode = os.environ.get('FAKE_MODE', '')
list_calls = int((root / 'list_calls').read_text()) if (root / 'list_calls').exists() else 0
def save(): (root / 'state.json').write_text(json.dumps(state))
def fail(message): print(message, file=sys.stderr); sys.exit(1)
if args[0] == 'api':
    path = next(arg for arg in args if arg.startswith('repos/'))
    if '/git/ref/tags/' in path:
        kind = 'tag' if mode == 'annotated' else 'commit'
        sha = 'f' * 40 if mode == 'tag-conflict' else os.environ['FAKE_COMMIT']
        print(json.dumps({'object': {'type': kind, 'sha': sha}}))
    elif '/git/tags/' in path:
        print(json.dumps({'object': {'type': 'commit', 'sha': os.environ['FAKE_COMMIT']}}))
    elif '/releases?per_page=' in path:
        if mode == 'draft-list-error': fail('gh: Forbidden (HTTP 403)')
        if mode == 'create-delay-list' and state is not None and state['tag_name'] == 'v0.1.0' and list_calls < 2:
            list_calls += 1
            (root / 'list_calls').write_text(str(list_calls))
            print(json.dumps([[], []]))
            sys.exit(0)
        print(json.dumps([[], [] if state is None else [state]]))
        if mode == 'create-delay-list':
            list_calls += 1
            (root / 'list_calls').write_text(str(list_calls))
    elif '/releases/tags/' in path:
        if mode in ('403', '500', 'network'):
            fail('gh: ' + ('TLS handshake timeout' if mode == 'network' else '(HTTP ' + mode + ')'))
        if state is None or state['draft'] or mode == 'draft-list-error': fail('gh: Not Found (HTTP 404)')
        print(json.dumps(state))
    else: fail('unexpected endpoint: ' + path)
elif args[:2] == ['release', 'download']:
    dest = pathlib.Path(args[args.index('--dir') + 1])
    for p in (root / 'assets').iterdir(): shutil.copy2(p, dest / p.name)
    if mode == 'corrupt-download':
        with (dest / 'meta-pulse_v0.1.0_web.tar.gz').open('ab') as f: f.write(b'bad')
    if mode == 'replace-during-download': state['assets'][0]['id'] += 1
    for asset in state['assets']: asset['download_count'] = asset.get('download_count', 0) + 1
    save()
elif args[:2] == ['release', 'create']:
    if mode == 'create-race':
        state = json.loads((root / 'complete.json').read_text()); save(); fail('release exists')
    if state is not None: fail('release exists')
    state = {'id': 123, 'tag_name': 'v0.1.0', 'draft': True, 'assets': []}; save()
elif args[:2] == ['release', 'upload']:
    if '--clobber' in args: fail('clobber forbidden')
    if not state['draft']: fail('MUTATION OF PUBLISHED RELEASE')
    if mode in ('upload-race', 'upload-partial-error'):
        state = json.loads((root / 'complete.json').read_text())
        if mode == 'upload-partial-error': state['draft'] = True
        save(); fail('upload response lost')
    p = pathlib.Path(args[3])
    if any(a['name'] == p.name for a in state['assets']): fail('duplicate')
    shutil.copy2(p, root / 'assets' / p.name)
    import hashlib
    state['assets'].append({'id': len(state['assets']) + 1, 'name': p.name, 'state': 'uploaded',
                           'size': p.stat().st_size, 'digest': 'sha256:' + hashlib.sha256(p.read_bytes()).hexdigest()})
    save()
elif args[:2] == ['release', 'edit']:
    if not state['draft']: fail('MUTATION OF PUBLISHED RELEASE')
    state['draft'] = False; save()
else: fail('unexpected command: ' + str(args))
'''


class PublishTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'repo'
        self.remote = Path(self.temp.name) / 'remote'
        self.root.mkdir()
        self.remote.mkdir()
        shutil.copytree(ROOT / 'release', self.root / 'release', ignore=shutil.ignore_patterns('__pycache__'))
        (self.root / 'deploy').mkdir()
        shutil.copy2(ROOT / 'deploy/verify-release.py', self.root / 'deploy/verify-release.py')
        (self.root / 'releases').mkdir()
        (self.root / 'releases' / f'{TAG}.md').write_text(f'# Meta Pulse {TAG}\n\n修复发布。\n')
        (self.root / 'VERSION').write_text('0.1.0\n')
        (self.root / 'CHANGELOG.md').write_text(f'## {TAG}\n\n[说明](releases/{TAG}.md)\n')
        for args in [('init', '-q'), ('config', 'user.name', 'test'),
                     ('config', 'user.email', 'test@example.invalid'), ('add', '.'),
                     ('commit', '-qm', '测试'), ('tag', TAG)]:
            subprocess.run(['git', *args], cwd=self.root, check=True, capture_output=True)
        self.commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=self.root, text=True).strip()
        self.bundle = self.remote / 'assets'
        self.bundle.mkdir()
        self.make_bundle(self.bundle, 1)
        self.state = self.inventory(self.bundle)
        self.save_state()
        (self.remote / 'complete.json').write_text(json.dumps(self.state))
        self.bin = Path(self.temp.name) / 'bin'
        self.bin.mkdir()
        (self.bin / 'gh').write_text(FAKE_GH)
        (self.bin / 'gh').chmod(0o755)

    def make_bundle(self, directory, mtime):
        for name in assets(TAG):
            buffer = io.BytesIO()
            with tarfile.open(fileobj=buffer, mode='w') as archive:
                member = tarfile.TarInfo('VERSION')
                member.size = 5
                archive.addfile(member, io.BytesIO(b'0.1.0'))
            (directory / name).write_bytes(gzip.compress(buffer.getvalue(), mtime=mtime))
        create(directory, TAG, self.commit)

    def inventory(self, directory):
        return {'id': 123, 'tag_name': TAG, 'draft': False, 'assets': [
            {'id': i, 'name': p.name, 'state': 'uploaded', 'size': p.stat().st_size,
             'digest': 'sha256:' + hashlib.sha256(p.read_bytes()).hexdigest()}
            for i, p in enumerate(sorted(directory.iterdir()), 1)]}

    def save_state(self):
        (self.remote / 'state.json').write_text(json.dumps(self.state))

    def local_build(self):
        directory = self.root / 'dist' / f'release-{TAG}'
        directory.mkdir(parents=True)
        self.make_bundle(directory, 2)
        return directory

    def run_publish(self, mode='', success=True):
        env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'],
                   GH_REPO='owner/repo', FAKE_REMOTE=str(self.remote), FAKE_COMMIT=self.commit,
                   FAKE_MODE=mode)
        result = subprocess.run(['bash', 'release/publish.sh', TAG, '--build-if-needed'],
                                cwd=self.root, env=env, text=True, capture_output=True)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return [json.loads(line) for line in (self.remote / 'calls').read_text().splitlines()]

    def assert_read_only(self, calls):
        self.assertFalse(any(c[:2] in (['release', 'upload'], ['release', 'create'], ['release', 'edit']) for c in calls))

    def test_replay_without_local_build_and_download_counter_changes(self):
        self.assert_read_only(self.run_publish())
        self.assert_read_only(self.run_publish())
        self.assertFalse((self.root / 'dist').exists())

    def test_different_gzip_bytes_from_same_source_are_accepted_read_only(self):
        local = self.local_build()
        name = assets(TAG)[0]
        self.assertNotEqual((local / name).read_bytes(), (self.bundle / name).read_bytes())
        self.assertEqual(gzip.decompress((local / name).read_bytes()), gzip.decompress((self.bundle / name).read_bytes()))
        self.assert_read_only(self.run_publish())

    def test_missing_remote_asset_fails_without_writes(self):
        path = self.bundle / assets(TAG)[0]
        path.unlink()
        self.state['assets'] = [e for e in self.state['assets'] if e['name'] != path.name]
        self.save_state()
        self.assert_read_only(self.run_publish(success=False))

    def test_manifest_commit_conflict_even_with_consistent_checksums(self):
        document = json.loads((self.bundle / 'release-manifest.json').read_text())
        document['commit'] = 'b' * 40
        (self.bundle / 'release-manifest.json').write_text(json.dumps(document))
        self.refresh_sums()
        self.assert_read_only(self.run_publish(success=False))

    def refresh_sums(self):
        (self.bundle / 'SHA256SUMS').write_text(''.join(
            f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n'
            for p in sorted(self.bundle.iterdir()) if p.name != 'SHA256SUMS'))
        self.state = self.inventory(self.bundle)
        self.save_state()

    def test_draft_listing_errors_do_not_create_release(self):
        self.assert_read_only(self.run_publish('draft-list-error', success=False))

    def test_remote_tag_conflict(self):
        self.assert_read_only(self.run_publish('tag-conflict', success=False))

    def test_annotated_tag(self):
        self.assert_read_only(self.run_publish('annotated'))

    def test_corrupt_download(self):
        self.assert_read_only(self.run_publish('corrupt-download', success=False))

    def test_asset_replacement_during_download_is_failure(self):
        self.assert_read_only(self.run_publish('replace-during-download', success=False))

    def test_read_errors_never_create_release(self):
        for mode in ('403', '500', 'network'):
            with self.subTest(mode=mode):
                self.assert_read_only(self.run_publish(mode, success=False))

    def test_new_draft_upload_publish_then_repeat(self):
        self.local_build()
        for p in self.bundle.iterdir(): p.unlink()
        self.state = None
        self.save_state()
        calls = self.run_publish()
        self.assertEqual(sum(c[:2] == ['release', 'upload'] for c in calls), 6)
        self.assertEqual(sum(c[:2] == ['release', 'edit'] for c in calls), 1)
        (self.remote / 'calls').write_text('')
        self.assert_read_only(self.run_publish())

    def test_draft_resume_only_missing_identical_assets(self):
        local = self.local_build()
        for p in self.bundle.iterdir(): p.unlink()
        shutil.copy2(local / 'SHA256SUMS', self.bundle / 'SHA256SUMS')
        self.state = self.inventory(self.bundle)
        self.state['draft'] = True
        self.save_state()
        calls = self.run_publish()
        self.assertEqual(sum(c[:2] == ['release', 'upload'] for c in calls), 5)

    def test_conflicting_draft_is_never_overwritten(self):
        self.local_build()
        self.state['draft'] = True
        self.save_state()
        self.assert_read_only(self.run_publish(success=False))

    def test_create_race_reconciles_published_bundle(self):
        self.local_build()
        self.state = None
        self.save_state()
        calls = self.run_publish('create-race')
        self.assertFalse(any(c[:2] in (['release', 'upload'], ['release', 'edit']) for c in calls))

    def test_new_release_waits_for_draft_to_become_listed(self):
        self.local_build()
        for p in self.bundle.iterdir(): p.unlink()
        self.state = None
        self.save_state()
        calls = self.run_publish('create-delay-list')
        self.assertEqual(sum(c[:2] == ['release', 'upload'] for c in calls), 6)
        self.assertEqual(sum(c[:2] == ['release', 'edit'] for c in calls), 1)
        self.assertEqual(len(list(self.bundle.iterdir())), 6)

    def test_upload_race_reconciles_published_bundle(self):
        self.local_build()
        self.state['draft'] = True
        self.state['assets'] = []
        self.save_state()
        calls = self.run_publish('upload-race')
        self.assertEqual(sum(c[:2] == ['release', 'upload'] for c in calls), 1)
        self.assertFalse(any(c[:2] == ['release', 'edit'] for c in calls))

    def test_upload_failure_does_not_swallow_conflicting_draft(self):
        self.local_build()
        self.state['draft'] = True
        self.state['assets'] = []
        self.save_state()
        calls = self.run_publish('upload-partial-error', success=False)
        self.assertFalse(any(c[:2] == ['release', 'edit'] for c in calls))


if __name__ == '__main__':
    unittest.main()
