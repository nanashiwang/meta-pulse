#!/usr/bin/env python3
"""Require a public published manifest matching the locally resolved Git tag."""
import json
import re
import sys
import urllib.request


def verify(document, tag, commit):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("invalid release tag")
    if document.get("schema_version") != 1 or document.get("version") != tag or document.get("commit") != commit:
        raise ValueError("published manifest does not match Git tag")
    expected = {f"meta-pulse_{tag}_{suffix}.tar.gz" for suffix in
                ("linux_amd64", "linux_arm64", "web", "source")}
    entries = document.get("assets", [])
    if len(entries) != 4 or {a.get("name") for a in entries} != expected:
        raise ValueError("release assets are incomplete")
    for entry in entries:
        if not re.fullmatch(r"[0-9a-f]{64}", entry.get("sha256", "")) or entry.get("size", 0) <= 0:
            raise ValueError("invalid release checksum or size")


if __name__ == "__main__":
    tag, commit = sys.argv[1:]
    if not re.fullmatch(r"v\d+\.\d+\.\d+", tag) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        sys.exit("无效版本或提交")
    url = f"https://github.com/nanashiwang/meta-pulse/releases/download/{tag}/release-manifest.json"
    with urllib.request.urlopen(url, timeout=30) as response:
        document = json.loads(response.read(65537))
    verify(document, tag, commit)
    print(f"已核实正式 Release：{tag} ({commit})")
