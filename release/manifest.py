#!/usr/bin/env python3
"""Generate a complete release inventory and checksums without runtime data."""
import hashlib
import json
from pathlib import Path
import re
import sys
import tarfile


def assets(tag):
    return [f"meta-pulse_{tag}_{suffix}.tar.gz" for suffix in
            ("linux_amd64", "linux_arm64", "web", "source")]


def create(directory, tag, revision):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("invalid release tag")
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("invalid commit")
    names = assets(tag)
    if {p.name for p in directory.iterdir()} != set(names):
        raise ValueError("release asset set is incomplete or contains extra files")
    inventory = []
    for name in names:
        path = directory / name
        with tarfile.open(path, "r:gz") as archive:
            members = archive.getmembers()
            if not members:
                raise ValueError("empty archive")
            for member in members:
                parts = Path(member.name).parts
                if (member.name.startswith("/") or ".." in parts or member.issym() or member.islnk()
                        or any(p in {".env", ".git", ".data", "runtime-keys", "docker-compose.override.yml"}
                               or (p.startswith(".env.") and p != ".env.example") for p in parts)):
                    raise ValueError("archive contains forbidden path")
        inventory.append({"name": name, "size": path.stat().st_size,
                          "sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
    manifest = {"version": tag, "commit": revision, "assets": inventory,
                "deployment": "git-tag-source-build", "schema_version": 1}
    (directory / "release-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    sums = []
    for name in sorted(names + ["release-manifest.json"]):
        sums.append(f"{hashlib.sha256((directory / name).read_bytes()).hexdigest()}  {name}\n")
    (directory / "SHA256SUMS").write_text("".join(sums))


if __name__ == "__main__":
    create(Path(sys.argv[1]), sys.argv[2], sys.argv[3])
