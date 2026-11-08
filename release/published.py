#!/usr/bin/env python3
"""Verify an already published Release without modifying GitHub assets."""
import hashlib
import json
from pathlib import Path
import re
import sys
from manifest import assets
from importlib.util import module_from_spec, spec_from_file_location

ROOT = Path(__file__).parents[1]
SPEC = spec_from_file_location("verify_release", ROOT / "deploy/verify-release.py")
VERIFY_RELEASE = module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(VERIFY_RELEASE)


def _sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify(directory, tag, commit, remote_commit, release):
    directory = Path(directory)
    if remote_commit != commit:
        raise ValueError("remote Git tag resolves to a different commit")
    if release.get("tag_name") != tag or release.get("draft") is not False:
        raise ValueError("published Release metadata is not final or tag-mismatched")
    expected = set(assets(tag))
    expected.update(("release-manifest.json", "SHA256SUMS"))
    if {p.name for p in directory.iterdir()} != expected:
        raise ValueError("downloaded asset set is missing files or contains extra files")
    if any(p.is_symlink() or not p.is_file() for p in directory.iterdir()):
        raise ValueError("downloaded asset must be a regular file")
    remote = {}
    for entry in release.get("assets", []):
        name, digest, size = entry.get("name"), entry.get("digest"), entry.get("size")
        if name in remote or not isinstance(name, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest or ""):
            raise ValueError("published Release has invalid or duplicate asset metadata")
        if entry.get("state") != "uploaded" or type(size) is not int or size <= 0:
            raise ValueError("published Release has invalid asset size")
        remote[name] = (digest.removeprefix("sha256:"), size)
    if set(remote) != expected:
        raise ValueError("published Release asset set is incomplete or contains extra files")

    try:
        manifest = json.loads((directory / "release-manifest.json").read_text())
    except (OSError, ValueError) as exc:
        raise ValueError("published release-manifest.json is unreadable") from exc
    VERIFY_RELEASE.verify(manifest, tag, commit)
    if manifest.get("deployment") != "git-tag-source-build":
        raise ValueError("published manifest deployment source is invalid")
    manifest_assets = {entry["name"]: entry for entry in manifest["assets"]}

    sums = {}
    try:
        sum_lines = (directory / "SHA256SUMS").read_text().splitlines()
    except OSError as exc:
        raise ValueError("published asset is missing: SHA256SUMS") from exc
    for line in sum_lines:
        match = re.fullmatch(r"([0-9a-f]{64})  ([^\s]+)", line)
        if not match or match.group(2) in sums:
            raise ValueError("published SHA256SUMS is malformed")
        sums[match.group(2)] = match.group(1)
    if set(sums) != expected - {"SHA256SUMS"}:
        raise ValueError("published SHA256SUMS inventory is incomplete or contains extra files")

    for name in sorted(expected):
        path = directory / name
        if not path.is_file():
            raise ValueError(f"published asset is missing: {name}")
        actual = _sha256(path)
        if path.stat().st_size != remote[name][1] or actual != remote[name][0] or (
                name in sums and actual != sums[name]):
            raise ValueError(f"published asset checksum or size mismatch: {name}")
        if name in manifest_assets and (manifest_assets[name]["size"] != path.stat().st_size or
                                        manifest_assets[name]["sha256"] != actual):
            raise ValueError(f"published manifest mismatch: {name}")
    print(f"已核实正式 Release：{tag} ({commit})，附件未修改")


if __name__ == "__main__":
    directory, tag, commit, remote_commit, release_json = sys.argv[1:]
    verify(directory, tag, commit, remote_commit, json.loads(Path(release_json).read_text()))
