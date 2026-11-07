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
patch -p1 --fuzz=0 < "$ROOT/services/forum/answer-optional-admin-email.patch"
GOWORK=off go test -race ./internal/schema ./internal/service/user_admin -run TestMetar -count=1
