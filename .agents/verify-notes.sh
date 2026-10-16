#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
command -v python3 >/dev/null 2>&1 || { echo 'Python 3.9+ is required for workflow checks.' >&2; exit 1; }
test -d .agents/notes || { echo 'Missing .agents/notes directory.' >&2; exit 1; }
exec python3 "$repo_root/.agents/workflow.py" verify "$repo_root"
