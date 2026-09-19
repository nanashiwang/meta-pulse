#!/usr/bin/env bash
# Release inputs come only from the exact Git tag, never local .env or volumes.
set -Eeuo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TAG="${1:-}"
[[ "$TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo '用法：release/build.sh vX.Y.Z' >&2; exit 1; }
cd "$ROOT"
REVISION="$(git rev-parse HEAD)"
[[ "$(git rev-parse "$TAG^{commit}")" == "$REVISION" ]] || { echo '标签与当前提交不一致' >&2; exit 1; }
git diff --quiet && git diff --cached --quiet || { echo '存在未提交的受跟踪改动' >&2; exit 1; }
[[ "v$(cat VERSION)" == "$TAG" && -s "releases/$TAG.md" ]] || { echo '版本号或中文更新说明缺失' >&2; exit 1; }
OUTPUT="$ROOT/dist/release-$TAG"
[[ ! -e "$OUTPUT" ]] || { echo "输出目录已存在：$OUTPUT" >&2; exit 1; }
STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT
mkdir -p "$STAGING/source" "$STAGING/output"
git archive "$REVISION" | tar -x -C "$STAGING/source"
SOURCE_DATE_EPOCH="$(git show -s --format=%ct "$REVISION")"
export SOURCE_DATE_EPOCH
cd "$STAGING/source"
FLAGS="-s -w -X github.com/nanashiwang/meta-pulse/internal/buildinfo.Version=${TAG#v} -X github.com/nanashiwang/meta-pulse/internal/buildinfo.Revision=$REVISION"
for arch in amd64 arm64; do
  package="meta-pulse_${TAG}_linux_${arch}"
  mkdir "$STAGING/$package"
  for component in api worker tool; do
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -buildvcs=false -ldflags "$FLAGS" \
      -o "$STAGING/$package/meta-pulse-$component" "./services/pulse/cmd/$component"
    if [[ "$(uname -s)" == Linux && "$(go env GOHOSTARCH)" == "$arch" ]]; then
      [[ "$("$STAGING/$package/meta-pulse-$component" --version)" == "Meta Pulse ${TAG#v} ($REVISION)" ]] || exit 1
    fi
  done
  cp VERSION "$STAGING/$package/"
  printf '%s\n' "$REVISION" >"$STAGING/$package/REVISION"
  tar -czf "$STAGING/output/$package.tar.gz" -C "$STAGING" "$package"
done
(cd sites/blog && npm ci --ignore-scripts && npm run build)
./deploy/build-community.sh
tar -czf "$STAGING/output/meta-pulse_${TAG}_web.tar.gz" -C sites/blog/docs/.vitepress/dist .
git -C "$ROOT" archive --format=tar.gz --prefix="meta-pulse-$TAG/" \
  --output="$STAGING/output/meta-pulse_${TAG}_source.tar.gz" "$REVISION"
python3 "$ROOT/release/manifest.py" "$STAGING/output" "$TAG" "$REVISION"
mkdir -p "$ROOT/dist"
mv "$STAGING/output" "$OUTPUT"
printf '发布附件已构建：%s\n' "$OUTPUT"
