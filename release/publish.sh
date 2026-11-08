#!/usr/bin/env bash
# Public releases are verified read-only; no release asset is ever overwritten.
set -Eeuo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TAG="${1:-}"
[[ "$TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || exit 1
cd "$ROOT"
python3 "$ROOT/release/preflight.py" "$TAG" --tagged
exec python3 "$ROOT/release/publish.py" "$@"
