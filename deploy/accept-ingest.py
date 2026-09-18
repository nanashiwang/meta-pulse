#!/usr/bin/env python3
"""Read-only production acceptance; Python standard library only."""
import argparse
import datetime as dt
import json
import pathlib
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parent.parent


def run(args):
    # Never echo command output on failure: Docker/config errors can contain DSNs.
    p = subprocess.run(args, cwd=ROOT, capture_output=True, text=True, timeout=30)
    if p.returncode:
        raise RuntimeError('只读采集命令失败（原始输出已隐藏，避免泄露凭据）')
    return p.stdout.strip()


def compose(*args):
    return run(['bash', '-c', 'source "$1"; shift; compose "$@"',
                'accept-ingest', str(ROOT / 'deploy/lib.sh'), *args])


def runtime():
    cid = compose('ps', '-q', 'pulse-worker')
    if not cid or '\n' in cid:
        raise RuntimeError('需要唯一运行中的 pulse-worker')
    data = json.loads(run(['docker', 'inspect', cid]))[0]
    state = data['State']
    env = dict(x.split('=', 1) for x in data['Config']['Env'] if '=' in x)
    revision = data['Config'].get('Labels', {}).get('org.opencontainers.image.revision', 'unknown')
    return dict(container=cid, image=data['Image'], revision=revision,
                batch_size=int(env.get('PULSE_INGEST_BATCH_SIZE', '').strip() or '250'),
                started_at=state['StartedAt'], restarts=data['RestartCount'],
                running=state['Running'])


def batches(raw):
    result = []
    for line in raw.splitlines():
        try:
            row = json.loads(line)
        except ValueError:
            continue
        if row.get('msg') in ('usage ingest batch completed', 'usage ingest failed'):
            # Explicit allowlist: errors may contain source fields or credentials.
            result.append({k: row[k] for k in ('time', 'msg', 'fetched', 'accepted',
                          'replayed', 'conflicts', 'manual_review', 'yielded', 'elapsed_ms') if k in row})
    return result


def position(value):
    parts = tuple(map(int, value.split(':'))) if value else (0, 0)
    if len(parts) != 2 or min(parts) < 0:
        raise ValueError('invalid cursor')
    return parts


def verdict(before, after, logs, stable=True):
    if not stable:
        return 2, '证据不足：观察期间 Worker 被替换、重启或配置变化'
    if any(x['msg'] == 'usage ingest failed' for x in logs):
        return 1, '失败：观察窗口内存在 usage ingest failed，不能以随后成功掩盖'
    if position(after['value']) < position(before['value']) or after['version'] < before['version']:
        return 1, '失败：游标 value/version 回退'
    if before['watermark_at'] and after['watermark_at'] and (
            dt.datetime.fromisoformat(after['watermark_at'].replace('Z', '+00:00')) <
            dt.datetime.fromisoformat(before['watermark_at'].replace('Z', '+00:00'))):
        return 1, '失败：watermark 回退'
    if len(logs) < 3:
        return 2, '证据不足：未采集到连续至少三个 Worker 批次'
    if not any(x.get('fetched', 0) > 0 for x in logs):
        return 2, '证据不足：没有新源流量或可摄入源行；空批次不能证明失败或已追平'
    if position(after['value']) <= position(before['value']) or after['version'] <= before['version']:
        return 2, '证据不足：未观察到已提交游标前进'
    if not after['watermark_at'] or after['watermark_at'] == before['watermark_at']:
        return 2, '证据不足：watermark 未变化，可能仍在同秒积压内续跑'
    return 0, '通过：已观察到分批续跑和持久游标前进；不代表已追平（未独立核验源端尾部）'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--seconds', type=int, default=180, help='观察时长，至少 90 秒，默认 180')
    args = parser.parse_args()
    if args.seconds < 90:
        parser.error('--seconds 必须至少为 90')
    commit = run(['git', 'rev-parse', 'HEAD'])
    if run(['git', 'status', '--porcelain', '--untracked-files=no']):
        raise RuntimeError('tracked 工作区不干净，无法核验 commit')
    initial = runtime()
    print(json.dumps(dict(commit=commit, runtime=initial), ensure_ascii=False), flush=True)
    if not initial['running'] or initial['revision'] != commit or initial['batch_size'] <= 0:
        raise RuntimeError('Worker 未运行、镜像 revision 与 HEAD 不一致或批量无效；请重建 Worker')
    snapshot = lambda: json.loads(compose('exec', '-T', 'pulse-worker', 'meta-pulse-tool', 'ingest-snapshot'))
    before = snapshot()
    # Anchor after the first snapshot: no pre-window batches count toward acceptance.
    since = dt.datetime.now(dt.timezone.utc).isoformat()
    print(json.dumps(dict(before=before, since=since), ensure_ascii=False), flush=True)
    time.sleep(args.seconds)
    # Read logs before final snapshot, so included batches have committed by that snapshot.
    logs = batches(run(['docker', 'logs', '--since', since, initial['container']]))
    after = snapshot()
    final = runtime()
    code, message = verdict(before, after, logs, initial == final)
    print(json.dumps(dict(after=after, batches=logs, batch_count=len(logs),
                         lag_delta=after['lag_seconds'] - before['lag_seconds'],
                         version_delta=after['version'] - before['version']), ensure_ascii=False, indent=2))
    print(message)
    return code


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (RuntimeError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired, OSError):
        print('失败：验收采集或配置核验失败；未改动游标和服务，请检查容器、版本及数据库连接')
        raise SystemExit(1)
