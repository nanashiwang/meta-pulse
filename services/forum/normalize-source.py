#!/usr/bin/env python3
"""Normalize the pinned upstream Reply component before applying text patches.

The Go module ZIP preserves this file as CRLF, unlike a Git export. Do not
normalize the module cache itself: callers pass their writable build copy.
"""
from pathlib import Path
import sys


def normalize(root: Path):
    source = root / 'ui/src/components/Comment/components/Reply/index.tsx'
    raw = source.read_bytes()
    normalized = raw.replace(b'\r\n', b'\n')
    if normalized != raw:
        source.write_bytes(normalized)


if __name__ == '__main__':
    normalize(Path(sys.argv[1]))
