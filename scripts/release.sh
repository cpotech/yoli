#!/usr/bin/env bash
# Tag a yoli release and build versioned binaries.
#
# Usage:
#   scripts/release.sh v0.2.0            # tag HEAD as v0.2.0 and build it
#   scripts/release.sh v0.2.0 <commit>   # tag a specific commit
#   YOLI_VERSION=v0.2.0 scripts/release.sh   # use YOLI_VERSION instead of $1
#
# The version string is injected into every binary via the same mechanism
# as scripts/build.sh (yoli/internal/cli.Version), so `yoli version` always
# reports the exact release. Binaries are written to dist/ as
# yoli-<os>-<arch> (cross-compiled for the common platforms) and the host
# binary is rebuilt at the repo root as well.
#
# After tagging, push the tag with `git push origin <tag>` to publish.

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

if ! command -v go >/dev/null 2>&1; then
  echo "release: go command not found on PATH" >&2
  exit 2
fi

version="${1:-${YOLI_VERSION:-}}"
if [[ -z "$version" ]]; then
  echo "release: usage: $0 <version>  (e.g. v0.2.0)" >&2
  exit 2
fi

target="${2:-HEAD}"
if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "release: not a git repository" >&2
  exit 2
fi
if git rev-parse "$version" >/dev/null 2>&1; then
  echo "release: tag '$version' already exists" >&2
  exit 1
fi

echo "release: creating annotated tag $version -> $target"
git tag -a "$version" -m "$version" "$target"

ldflags="-s -w -X yoli/internal/cli.Version=${version}"

# Cross-compile the common platforms into dist/. GOOS/GOARCH are looped here
# since the ldflags version must be identical across every artifact.
mkdir -p dist
platforms=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)
for p in "${platforms[@]}"; do
  goos="${p%/*}"
  goarch="${p#*/}"
  out="dist/yoli-${goos}-${goarch}"
  echo "release: building $out ($version)"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -ldflags="$ldflags" -o "$out" ./cmd/yoli
done

# Rebuild the host binary at the repo root with the same version.
echo "release: building ./yoli ($version)"
CGO_ENABLED=0 go build -ldflags="$ldflags" -o "$repo_root/yoli" ./cmd/yoli

echo "release: ok — built $version for ${#platforms[@]} platforms in dist/"
echo "release: push the tag with: git push origin $version"
