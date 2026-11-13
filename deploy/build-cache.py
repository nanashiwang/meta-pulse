#!/usr/bin/env python3
"""Bound disposable build cache; never remove images, volumes or backups."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess

MIN_FREE = 20 * 1024**3


def docker(*args):
    return subprocess.check_output(['docker', *args], text=True).strip()


def local_paths(repo):
    # Host disk checks are meaningless against a remote daemon/builder.
    if os.environ.get('DOCKER_HOST') or os.environ.get('BUILDX_BUILDER'):
        raise RuntimeError('请取消 DOCKER_HOST/BUILDX_BUILDER，使用本机默认 builder')
    if docker('context', 'show') != 'default':
        raise RuntimeError('磁盘保护要求 default Docker context，与 default builder 保持一致')
    endpoint = json.loads(docker('context', 'inspect', '--format', '{{json .Endpoints.docker.Host}}'))
    if not endpoint.startswith('unix://'):
        raise RuntimeError('磁盘保护仅支持本机 Unix socket Docker')
    root = docker('info', '--format', '{{.DockerRootDir}}')
    paths = [Path(repo), Path(root)]
    # Docker's containerd image store may be on a different filesystem.
    if Path('/var/lib/containerd').exists():
        paths.append(Path('/var/lib/containerd'))
    return paths


def check_space(paths):
    for path in paths:
        free = shutil.disk_usage(path).free
        print(f'[meta-pulse] 磁盘 {path}: 可用 {free / 1024**3:.1f} GiB', flush=True)
        if free < MIN_FREE:
            raise RuntimeError(f'{path} 空闲不足 20 GiB；停止更新，请检查磁盘占用（不自动删除业务数据或备份）')


def maintain(repo, check_only=False):
    paths = local_paths(repo)
    if not check_only:
        help_text = docker('buildx', 'prune', '--help')
        args = ['buildx', 'prune', '--builder', 'default', '--all', '--force']
        if '--max-used-space' in help_text and '--min-free-space' in help_text:
            args += ['--max-used-space', '8GB', '--min-free-space', str(MIN_FREE)]
        elif '--keep-storage' in help_text:
            args += ['--keep-storage', '8GB']
        else:
            raise RuntimeError('Docker buildx 不支持缓存容量限制，请升级后重试')
        print('[meta-pulse] 回收未使用构建缓存，保留目标 8 GB', flush=True)
        subprocess.run(['docker', *args], check=True, stdout=subprocess.DEVNULL)
    check_space(paths)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check-only', action='store_true')
    parser.add_argument('repo')
    args = parser.parse_args()
    try:
        maintain(args.repo, args.check_only)
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as exc:
        parser.exit(1, f'[meta-pulse] 磁盘保护失败：{exc}\n')
