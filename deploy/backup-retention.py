#!/usr/bin/env python3
"""Internal update.sh backup lifecycle; run under its inherited Git lock (fd 9)."""
import argparse
from contextlib import contextmanager
import fcntl
import json
import os
from pathlib import Path
import re
import stat
import sys

NAME = re.compile(r"[0-9]{8}T[0-9]{6}Z-(?:[0-9a-f]{40}|[0-9a-f]{64})-[1-9][0-9]*")
STATE = ".update-state.json"
PIN = ".keep"
FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW


def keep_count(value):
    if not re.fullmatch(r"0|[1-9][0-9]{0,5}", value):
        raise ValueError("保留数量必须是 0..999999 的整数（0 禁用清理）")
    return int(value)


@contextmanager
def directory(name, parent=None):
    fd = os.open(name, FLAGS, dir_fd=parent)
    try:
        yield fd
    finally:
        os.close(fd)


@contextmanager
def backup_root(repo, create=False):
    # Do not resolve away symlinks. Open every component without following them.
    path = Path(repo)
    if not path.is_absolute() or ".." in path.parts:
        raise ValueError("仓库路径必须是无 .. 的绝对路径")
    from contextlib import ExitStack
    with ExitStack() as stack:
        fd = stack.enter_context(directory("/"))
        for part in path.parts[1:]:
            fd = stack.enter_context(directory(part, fd))
        for part in (".data", "deploy-backups"):
            if create:
                try:
                    os.mkdir(part, 0o700, dir_fd=fd)
                except FileExistsError:
                    pass
            fd = stack.enter_context(directory(part, fd))
        yield fd


def valid_name(name):
    if not NAME.fullmatch(name):
        raise ValueError("拒绝越界或非部署备份名称: " + repr(name))


def state(fd, name):
    try:
        file_fd = os.open(STATE, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
        with os.fdopen(file_fd) as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
                return None
            value = json.loads(stream.read(4097))
        if value.get("schema") == 1 and value.get("name") == name:
            return value.get("status")
    except (OSError, ValueError, AttributeError):
        pass
    return None


def write_state(fd, name, status):
    # Atomic publication; crash before replace leaves the previous protected state.
    temp = STATE + ".tmp"
    file_fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=fd)
    with os.fdopen(file_fd, "w") as stream:
        json.dump({"schema": 1, "name": name, "status": status}, stream)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temp, STATE, src_dir_fd=fd, dst_dir_fd=fd)
    os.fsync(fd)


def exists(fd, name):
    try:
        os.stat(name, dir_fd=fd, follow_symlinks=False)
        return True
    except FileNotFoundError:
        return False


def walk(fd, device, delete=False):
    """Never follow a symlink, mount on another device, or special file."""
    if os.fstat(fd).st_dev != device:
        raise ValueError("拒绝跨设备备份目录")
    for name in os.listdir(fd):
        info = os.stat(name, dir_fd=fd, follow_symlinks=False)
        if stat.S_ISDIR(info.st_mode):
            with directory(name, fd) as child:
                walk(child, device, delete)
            if delete:
                os.rmdir(name, dir_fd=fd)
        elif stat.S_ISREG(info.st_mode) and info.st_nlink == 1:
            if delete:
                os.unlink(name, dir_fd=fd)
        else:
            raise ValueError("拒绝符号链接、硬链接或特殊文件: " + repr(name))


def plan(fd, keep, current=""):
    if current:
        valid_name(current)
    candidates = []
    for name in sorted(os.listdir(fd), reverse=True):
        if not NAME.fullmatch(name):
            print("KEEP unknown " + repr(name))
            continue
        try:
            with directory(name, fd) as child:
                status = state(child, name)
                if name == current or exists(child, PIN) or status != "success":
                    print("KEEP protected " + name)
                    continue
                walk(child, os.fstat(fd).st_dev)
            candidates.append(name)
        except (OSError, ValueError) as exc:
            print("KEEP unsafe " + name + ": " + str(exc))
    # Current successful backup counts toward N, but is protected regardless of clock order.
    slots = keep
    if current:
        with directory(current, fd) as child:
            if state(child, current) == "success":
                slots = max(0, keep - 1)
    victims = candidates[slots:] if keep else []
    for name in candidates:
        print(("DELETE " if name in victims else "KEEP retained ") + name)
    return victims


def finish(fd, name, keep):
    valid_name(name)
    with directory(name, fd) as child:
        if state(child, name) != "running":
            raise ValueError("当前备份不是 running，拒绝标记成功")
        write_state(child, name, "success")
    for victim in plan(fd, keep, name):
        # Recheck policy and the whole tree before each removal, under the same lock.
        with directory(victim, fd) as child:
            if state(child, victim) != "success" or exists(child, PIN):
                raise ValueError("备份状态已变化，停止清理")
            walk(child, os.fstat(fd).st_dev)
            walk(child, os.fstat(fd).st_dev, delete=True)
        os.rmdir(victim, dir_fd=fd)
        print("REMOVED " + victim)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("create", "failed", "finish", "preview"))
    parser.add_argument("repo")
    parser.add_argument("--name", default="")
    parser.add_argument("--keep", default="3")
    args = parser.parse_args()
    try:
        keep = keep_count(args.keep)
        # Shell holds fd 9 across configuration, backup, rollout, acceptance and cleanup.
        fcntl.flock(9, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with backup_root(args.repo, create=args.action == "create") as fd:
            if args.action == "preview":
                plan(fd, keep)
            else:
                valid_name(args.name)
                if args.action == "create":
                    os.mkdir(args.name, 0o700, dir_fd=fd)
                    with directory(args.name, fd) as child:
                        write_state(child, args.name, "running")
                elif args.action == "finish":
                    finish(fd, args.name, keep)
                else:
                    with directory(args.name, fd) as child:
                        if state(child, args.name) == "running":
                            write_state(child, args.name, "failed")
    except FileNotFoundError:
        if args.action == "preview":
            print("没有可预览的部署备份目录")
            return 0
        raise
    except (OSError, ValueError) as exc:
        print("备份保留失败: " + str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
