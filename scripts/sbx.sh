#!/usr/bin/env bash
# Run yoli in a Docker Sandboxes (sbx) microVM via the yoli kit
# (deploy/yoli-kit). One entry point, usable from any repo:
#
#   scripts/sbx.sh              # yoli TUI in a sandbox on the current directory
#   scripts/sbx.sh acp          # `yoli acp` in that sandbox, for an editor
#   scripts/sbx.sh chat "…"     # any other yoli command, likewise
#   NAME=foo scripts/sbx.sh     # custom sandbox name
#
# With arguments, stdin/stdout pass straight through to `yoli <args>` in the
# sandbox, so an editor can run the agent sandboxed: e.g. CodeCompanion's ACP
# command `{ "yoli-sbx", "acp" }` with this script linked onto PATH as
# yoli-sbx. Only the current directory is mounted into the sandbox, and the
# script refuses one that contains your home or ~/.config/yoli.
#
# API keys never enter the sandbox. yoli's config (delivered by the kit) carries
# an inert per-provider placeholder as each api_key; the real keys are stored on
# the host as proxy custom-secrets (`sbx secret set-custom`) keyed by provider
# host, and the host-side proxy swaps the placeholder for the real key on
# outbound requests. Both the placeholder config and the secrets are derived
# here from ~/.config/yoli/config.json (the single source of truth).
#
# Knobs (environment variables):
#   NAME         sandbox name             (default: yoli-<dirname>)
#   FORCE_BUILD  set to 1 to rebuild the image even if it already exists

set -euo pipefail

# Setup output goes to stderr (fd 3 keeps the real stdout for the final exec),
# so `acp` mode's stdout carries only ACP frames.
exec 3>&1 1>&2

# Resolve the real script location (via any symlink) so this works when linked
# onto PATH and invoked from another repo. The workspace is always $PWD.
script_path="$(readlink -f "$0" 2>/dev/null || echo "$0")"
repo_root="$(cd "$(dirname "$script_path")/.." && pwd)"
image="yoli:sbx"
kit_dir="$repo_root/deploy/yoli-kit"
workspace="$PWD"
name="${NAME:-yoli-$(basename "$workspace")}"

# Locate the sbx CLI (installed under ~/.docker/sbx/bin, not always on PATH).
sbx="$(command -v sbx || true)"
if [[ -z "$sbx" && -x "$HOME/.docker/sbx/bin/sbx" ]]; then
  sbx="$HOME/.docker/sbx/bin/sbx"
fi
if [[ -z "$sbx" ]]; then
  echo "sbx: Docker Sandboxes CLI not found (looked on PATH and ~/.docker/sbx/bin)" >&2
  exit 2
fi

src_config="${XDG_CONFIG_HOME:-$HOME/.config}/yoli/config.json"

# Never mount a directory that contains your home or the real yoli config
# (e.g. ~ or /): the sandbox would see your keys and SSH keys.
ws_prefix="${workspace%/}/"
for p in "$HOME" "$src_config"; do
  case "$p/" in
    "$ws_prefix"*)
      echo "sbx: refusing to mount $workspace: it contains $p — run from a project directory" >&2
      exit 2 ;;
  esac
done

# 1. Build the kit's image and load it into sbx if sbx lacks it, or when
#    explicitly forced. sbx runs images from its own store, not Docker's.
if [[ "${FORCE_BUILD:-}" == "1" ]] ||
   ! "$sbx" template ls 2>/dev/null | awk '{print $1 ":" $2}' | grep -qx "docker.io/library/$image"; then
  version="${YOLI_VERSION:-}"
  if [[ -z "$version" ]] && git -C "$repo_root" rev-parse --git-dir >/dev/null 2>&1; then
    version="$(git -C "$repo_root" describe --tags --dirty --always 2>/dev/null || true)"
  fi
  version="${version:-dev}"
  echo "sbx: building $image (version=$version)" >&2
  docker build -f "$repo_root/Dockerfile.sbx" \
    --build-arg "YOLI_VERSION=$version" -t "$image" "$repo_root"
  docker save "$image" | "$sbx" template load /dev/stdin
fi

# 2. Render the kit's placeholder config and register a proxy custom-secret per
#    provider/key (matched by host, placeholder "yoli-sbx-<provider>"). The real
#    key values go to the host keychain via sbx; they never enter the sandbox.
if [[ -f "$src_config" ]]; then
  SBX_BIN="$sbx" python3 - "$src_config" "$kit_dir/files/home/.config/yoli/config.json" <<'PY'
import json, os, subprocess, sys
from urllib.parse import urlparse
sbx = os.environ["SBX_BIN"]
cfg = json.load(open(sys.argv[1]))
out = sys.argv[2]

def register(host, placeholder, value):
    if not (host and value):
        return
    r = subprocess.run(
        [sbx, "secret", "set-custom", "-g", "--host", host,
         "--placeholder", placeholder, "--value", value],
        capture_output=True, text=True)
    if r.returncode != 0:
        print(f"sbx: warning: could not register the key for {host}: "
              f"{r.stderr.strip()}", file=sys.stderr)

res = {}
if cfg.get("default_provider"):
    res["default_provider"] = cfg["default_provider"]
provs = {}
for pname, p in (cfg.get("providers") or {}).items():
    if not isinstance(p, dict):
        continue
    placeholder = f"yoli-sbx-{pname}"
    # api_key is the only secret in a profile; it becomes an inert
    # placeholder the proxy swaps on egress. Every other field is kept.
    provs[pname] = {**p, "api_key": placeholder}
    register(urlparse(p.get("base_url", "")).hostname or "", placeholder, p.get("api_key", ""))
if provs:
    res["providers"] = provs
if cfg.get("BRAVE_API_KEY"):
    res["BRAVE_API_KEY"] = "yoli-sbx-brave"
    register("api.search.brave.com", "yoli-sbx-brave", cfg["BRAVE_API_KEY"])

os.makedirs(os.path.dirname(out), exist_ok=True)
with open(out, "w") as f:
    json.dump(res, f, indent=2)
    f.write("\n")
PY
else
  echo "sbx: warning: no config at $src_config — providers will have no key" >&2
  mkdir -p "$kit_dir/files/home/.config/yoli"
  printf '{}\n' > "$kit_dir/files/home/.config/yoli/config.json"
fi

# 3. Create the sandbox on first use (agent name "yoli" must match the kit's
#    name), then attach the TUI, or run the given yoli command (sbx exec
#    starts a stopped sandbox) — with a TTY when run from a terminal (e.g.
#    `tui --provider x`), plain pipes when an editor runs it (`acp`).
#    A same-named sandbox on another directory (~/a/app vs ~/b/app) is never
#    reused: the agent would work on the wrong repo.
mounted="$("$sbx" ls --json | python3 -c '
import json, sys
for s in json.load(sys.stdin)["sandboxes"]:
    if s["name"] == sys.argv[1]:
        print((s.get("workspaces") or ["?"])[0])' "$name")"
if [[ -z "$mounted" ]]; then
  "$sbx" create --kit "$kit_dir" --name "$name" yoli "$workspace"
elif [[ ! "$mounted" -ef "$workspace" ]]; then
  echo "sbx: sandbox $name already mounts $mounted; set NAME= to use another name" >&2
  exit 2
fi
if [[ $# -gt 0 ]]; then
  tty=()
  if [[ -t 0 && -t 3 ]]; then tty=(-t); fi
  exec "$sbx" exec -i "${tty[@]}" "$name" yoli "$@" >&3 3>&-
fi
exec "$sbx" run --name "$name" >&3 3>&-
