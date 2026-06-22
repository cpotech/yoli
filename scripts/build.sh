#!/usr/bin/env bash
# Build the yoli CLI binary at the repo root.
# Honors GOOS/GOARCH if set; otherwise builds for the host platform.
# Injects a version string derived from `git describe --tags --dirty
# --always` into yoli/internal/cli.Version. With a reachable tag this
# yields e.g. v0.1.0 or v0.1.0-3-ga011326-dirty; without one it falls
# back to the short commit sha (optionally with -dirty). Reports "dev"
# only when git itself is unavailable or the tree has no commits.

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

if ! command -v go >/dev/null 2>&1; then
  echo "build: go command not found on PATH" >&2
  exit 2
fi

output="${OUTPUT:-$repo_root/yoli}"

version="${YOLI_VERSION:-}"
if [[ -z "$version" ]]; then
  if command -v git >/dev/null 2>&1 \
      && git -C "$repo_root" rev-parse --git-dir >/dev/null 2>&1; then
    version="$(git -C "$repo_root" describe --tags --dirty --always 2>/dev/null || true)"
  fi
fi
if [[ -z "$version" ]]; then
  version="dev"
fi

ldflags="-s -w -X yoli/internal/cli.Version=${version}"

echo "build: compiling yoli -> $output (version=$version)"
CGO_ENABLED="${CGO_ENABLED:-0}" go build -ldflags="$ldflags" -o "$output" ./cmd/yoli

reported="$("$output" version | tr -d '\r\n')"
echo "build: ok (yoli $reported)"
