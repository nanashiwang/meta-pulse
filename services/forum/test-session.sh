#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SOURCE="$(go env GOMODCACHE)/github.com/apache/answer@v1.7.1"
if [[ ! -d "$SOURCE" ]]; then go mod download github.com/apache/answer@v1.7.1; fi
WORK="$(mktemp -d)"
cp -R "$SOURCE/." "$WORK/"
chmod -R u+w "$WORK"
cd "$WORK"
patch -p1 --fuzz=0 < "$ROOT/services/forum/answer-session-retention.patch"
cp "$ROOT/services/forum/session-tests/retention_test.go" internal/repo/auth/metar_retention_test.go
cp "$ROOT/services/forum/session-tests/middleware/retention_test.go" internal/base/middleware/metar_retention_test.go
GOWORK=off go test -race ./internal/repo/auth ./internal/base/middleware -run TestMetar -count=1
GOWORK=off go test ./internal/controller ./internal/base/middleware ./internal/service/auth
