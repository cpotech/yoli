#!/usr/bin/env bash
# Run yoli inside a container against the current directory.
# Builds the image on first use (or when FORCE_BUILD=1), then runs yoli
# with the current working directory mounted at /work and OPENROUTER_API_KEY
# forwarded. All arguments are passed straight through to yoli, e.g.:
#
#   scripts/yoli-docker.sh chat "list the files here"
#   scripts/yoli-docker.sh            # no args -> interactive tui
#
# Credentials: OPENROUTER_API_KEY (and BRAVE_API_KEY) are read from the
# environment if set; otherwise the host's yoli config at
# ~/.config/yoli/config.json is mounted into the container so yoli reads the
# stored keys (and default_model) itself. Environment variables take priority.
#
# Knobs (environment variables):
#   IMAGE        image tag to build/run (default: yoli:local)
#   FORCE_BUILD  set to 1 to rebuild even if the image already exists
#   NO_NETWORK   set to 1 to run with --network none (offline tasks)
#   FIREWALL     set to 1 to run the hardened egress-firewall setup

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
image="${IMAGE:-yoli:local}"
compose_file="$repo_root/docker-compose.egress.yml"

# Host yoli config (XDG-aware). Mounted read-only so yoli picks up stored
# credentials and defaults without re-exporting them.
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/yoli"
config_file="$config_dir/config.json"

if ! command -v docker >/dev/null 2>&1; then
  echo "yoli-docker: docker command not found on PATH" >&2
  exit 2
fi

if ! docker info >/dev/null 2>&1; then
  echo "yoli-docker: docker daemon is not reachable" >&2
  exit 2
fi

# Hardened mode: delegate to the egress-firewall compose setup instead of a
# plain `docker run`. The firewall sidecar owns the network namespace, so the
# simple yoli:local image isn't needed here.
if [[ "${FIREWALL:-}" == "1" ]]; then
  if [[ "${NO_NETWORK:-}" == "1" ]]; then
    echo "yoli-docker: warning: NO_NETWORK is ignored in firewall mode (the firewall manages the network)" >&2
  fi

  # The compose file defaults ${YOLI_WORKDIR:-.} to ., which resolves to the
  # compose project dir (repo root), not the caller's cwd. Set it explicitly.
  export YOLI_WORKDIR="$PWD"

  # Honor XDG_CONFIG_HOME for the read-only config mount.
  if [[ -f "$config_file" ]]; then
    export YOLI_CONFIG_DIR="$config_dir"
  fi

  compose_args=(-f "$compose_file" run --rm)
  # compose `run` allocates a TTY by default; disable it when stdin/stdout are
  # not terminals so the command also works when piped or run in CI.
  if [[ ! -t 0 || ! -t 1 ]]; then
    compose_args+=(-T)
  fi
  if [[ "${FORCE_BUILD:-}" == "1" ]]; then
    compose_args+=(--build)
  fi

  exec docker compose "${compose_args[@]}" yoli "$@"
fi

# Build the image if it is missing, or when explicitly forced.
if [[ "${FORCE_BUILD:-}" == "1" ]] || ! docker image inspect "$image" >/dev/null 2>&1; then
  echo "yoli-docker: building image $image" >&2
  docker build -t "$image" "$repo_root"
fi

run_args=(--rm -v "$PWD":/work)

# Allocate an interactive TTY only when stdin/stdout are terminals, so the
# script also works when piped or run in CI.
if [[ -t 0 && -t 1 ]]; then
  run_args+=(-it)
fi

# Mount the host config (read-only) so yoli reads stored keys and defaults.
# The container runs as root, so its config path is /root/.config/yoli.
if [[ -f "$config_file" ]]; then
  run_args+=(-v "$config_dir":/root/.config/yoli:ro)
fi

# Forward the API key from the environment if present (overrides the config
# file). Only warn when neither source can supply it.
if [[ -n "${OPENROUTER_API_KEY:-}" ]]; then
  run_args+=(-e OPENROUTER_API_KEY)
elif [[ ! -f "$config_file" ]]; then
  echo "yoli-docker: warning: OPENROUTER_API_KEY is not set and no config at $config_file" >&2
fi

# Forward the Brave Search key from the environment if present.
if [[ -n "${BRAVE_API_KEY:-}" ]]; then
  run_args+=(-e BRAVE_API_KEY)
fi

if [[ "${NO_NETWORK:-}" == "1" ]]; then
  run_args+=(--network none)
fi

exec docker run "${run_args[@]}" "$image" "$@"
