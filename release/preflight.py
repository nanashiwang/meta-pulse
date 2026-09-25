#!/usr/bin/env python3
"""Check committed release metadata before creating a tag or building assets."""
import argparse
from pathlib import Path
import re
import subprocess
import sys


TAG_PATTERN = r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"


def check(root, tag=None, tagged=False):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(root), *args], text=True,
                                       stderr=subprocess.PIPE).strip()

    def read(path):
        try:
            return git("show", f"HEAD:{path}")
        except subprocess.CalledProcessError as exc:
            raise ValueError(f"当前提交缺少 {path}；请先提交版本及中文更新说明") from exc

    version = read("VERSION")
    tag = tag or f"v{version}"
    if not re.fullmatch(TAG_PATTERN, tag):
        raise ValueError(f"版本标签格式无效：{tag}")
    if f"v{version}" != tag:
        raise ValueError(f"标签 {tag} 与当前提交 VERSION={version} 不一致")
    if tagged:
        try:
            revision = git("rev-parse", f"refs/tags/{tag}^{{commit}}")
        except subprocess.CalledProcessError as exc:
            raise ValueError(f"本地缺少标签 {tag}") from exc
        if revision != git("rev-parse", "HEAD"):
            raise ValueError(f"标签 {tag} 与当前提交不一致")
    notes = read(f"releases/{tag}.md")
    if not re.search(rf"^# Meta Pulse {re.escape(tag)}$", notes, re.MULTILINE):
        raise ValueError(f"releases/{tag}.md 缺少对应版本标题")
    if not re.search(r"[\u4e00-\u9fff]", notes):
        raise ValueError(f"releases/{tag}.md 缺少中文更新说明")
    changelog = read("CHANGELOG.md")
    if not re.search(rf"^## {re.escape(tag)}$", changelog, re.MULTILINE):
        raise ValueError(f"CHANGELOG.md 缺少 {tag} 条目")
    if f"releases/{tag}.md" not in changelog:
        raise ValueError(f"CHANGELOG.md 缺少 {tag} 更新说明链接")
    return tag


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag", nargs="?", help="计划发布的标签，默认使用已提交 VERSION")
    parser.add_argument("--tagged", action="store_true", help="额外要求标签存在且指向 HEAD")
    args = parser.parse_args()
    try:
        tag = check(Path(__file__).resolve().parents[1], args.tag, args.tagged)
    except (ValueError, subprocess.CalledProcessError) as exc:
        print(f"发布前检查失败：{exc}", file=sys.stderr)
        return 1
    print(f"发布前一致性检查通过：{tag}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
