#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SOURCE="$(go env GOMODCACHE)/github.com/apache/answer@v1.7.1"
if [[ ! -d "$SOURCE" ]]; then go mod download github.com/apache/answer@v1.7.1; fi
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp -R "$SOURCE/." "$WORK/"
chmod -R u+w "$WORK"
python3 "$ROOT/services/forum/normalize-source.py" "$WORK"
cd "$WORK"
for name in session-retention optional-admin-email admin-user-list admin-privileges-compact mail-errors question-author operator-publishing discussion-labels; do
 patch -p1 --fuzz=0 < "$ROOT/services/forum/answer-$name.patch"
done
GOWORK=off go test ./internal/router ./internal/controller -run TestMetarPublishing -count=1
