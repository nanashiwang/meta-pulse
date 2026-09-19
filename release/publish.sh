#!/usr/bin/env bash
# Published releases are never overwritten; a failed draft can be retried.
set -Eeuo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TAG="${1:-}"
[[ "$TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || exit 1
cd "$ROOT"
[[ "$(git rev-parse "$TAG^{commit}")" == "$(git rev-parse HEAD)" ]] || exit 1
OUTPUT="$ROOT/dist/release-$TAG"
[[ -s "$OUTPUT/SHA256SUMS" && -s "releases/$TAG.md" ]] || exit 1
(cd "$OUTPUT" && sha256sum --check SHA256SUMS)
if draft="$(gh release view "$TAG" --json isDraft --jq .isDraft 2>/dev/null)"; then
  [[ "$draft" == true ]] || { echo '已发布版本不可覆盖，请升级版本号' >&2; exit 1; }
else
  gh release create "$TAG" --verify-tag --draft --title "Meta Pulse $TAG" --notes-file "releases/$TAG.md"
fi
gh release upload "$TAG" "$OUTPUT/"* --clobber
VERIFY="$(mktemp -d)"
trap 'rm -rf "$VERIFY"' EXIT
gh release download "$TAG" --dir "$VERIFY"
diff <(cd "$OUTPUT" && find . -type f | sort) <(cd "$VERIFY" && find . -type f | sort)
cmp "$OUTPUT/SHA256SUMS" "$VERIFY/SHA256SUMS"
(cd "$VERIFY" && sha256sum --check SHA256SUMS)
gh release edit "$TAG" --draft=false --latest --notes-file "releases/$TAG.md"
gh release view "$TAG" --json url,isDraft,assets
