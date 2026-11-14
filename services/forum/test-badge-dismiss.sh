#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SOURCE="$(go env GOMODCACHE)/github.com/apache/answer@v1.7.1"
if [[ ! -d "$SOURCE" ]]; then go mod download github.com/apache/answer@v1.7.1; fi
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp -R "$SOURCE/." "$WORK/"
chmod -R u+w "$WORK"
cd "$WORK"
patch -p1 --fuzz=0 < "$ROOT/services/forum/answer-badge-dismiss.patch"
cp "$ROOT/services/forum/badge-tests/dismiss_test.go" internal/service/notification/metar_dismiss_test.go
GOWORK=off go test -race ./internal/service/notification -run TestMetarBadge -count=1
GOWORK=off go build ./internal/service/notification ./internal/controller

npm ci --prefix "$ROOT/services/forum/badge-tests/ui" --ignore-scripts --no-audit --no-fund
BADGE_SOURCE="$WORK/ui/src/components/Modal/BadgeModal.tsx" node --test "$ROOT/services/forum/badge-tests/ui/dismiss.test.cjs"
