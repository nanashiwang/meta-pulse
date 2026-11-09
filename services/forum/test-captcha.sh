#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SOURCE="$(go env GOMODCACHE)/github.com/apache/answer@v1.7.1"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp -R "$SOURCE/." "$WORK/"
chmod -R u+w "$WORK"
cp "$ROOT/services/forum/captcha-tests/strategy_test.go" "$WORK/internal/service/action/metar_captcha_test.go"
cd "$WORK"
GOWORK=off go test -race ./internal/service/action -run TestMetar -count=1
