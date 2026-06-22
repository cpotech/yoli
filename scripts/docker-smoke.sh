#!/usr/bin/env bash
# Build the yoli Docker image and verify it can print its own version.
# Fails fast and prints a useful message if Docker is not available.

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker-smoke: docker command not found on PATH" >&2
  exit 2
fi

if ! docker info >/dev/null 2>&1; then
  echo "docker-smoke: docker daemon is not reachable" >&2
  exit 2
fi

image_tag="yoli:smoke"
docker build -t "$image_tag" "$repo_root"

# Extract the version constant from the Go source.
expected="$(awk -F\" '/^const Version =/ {print $2}' internal/cli/version.go)"
actual="$(docker run --rm "$image_tag" version | tr -d '\r\n')"

if [ "$actual" != "$expected" ]; then
  echo "docker-smoke: version mismatch — expected '$expected', got '$actual'" >&2
  exit 1
fi

echo "docker-smoke: ok (yoli $expected)"
