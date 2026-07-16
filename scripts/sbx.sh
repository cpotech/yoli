#!/usr/bin/env bash
# Run yoli in a Docker Sandboxes (sbx) microVM via the yoli kit
# (deploy/yoli-kit). One entry point, usable from any repo:
#
#   scripts/sbx.sh              # yoli TUI in a sandbox on the current directory
#   NAME=foo scripts/sbx.sh     # custom sandbox name
#
# Run other yoli commands in the sandbox with:
#   sbx exec <sandbox> yoli chat "…"
#
# API keys never enter the sandbox. yoli's config (delivered by the kit) carries
# an inert per-provider placeholder as each api_key; the real keys are stored on
# the host as proxy custom-secrets (`sbx secret set-custom`) keyed by provider
# host, and the host-side proxy swaps the placeholder for the real key on
# outbound requests. Both the placeholder config and the secrets are derived
# here from ~/.config/yoli/config.json (the single source of truth).
#
# Knobs (environment variables):
#   IMAGE        kit image tag to build   (default: yoli:sbx)
#   NAME         sandbox name             (default: yoli-<dirname>)
#   FORCE_BUILD  set to 1 to rebuild the image even if it already exists

set -euo pipefail

# Resolve the real script location (via any symlink) so this works when linked
# onto PATH and invoked from another repo. The workspace is always $PWD.
script_path="$(readlink -f "$0" 2>/dev/null || echo "$0")"
repo_root="$(cd "$(dirname "$script_path")/.." && pwd)"
image="${IMAGE:-yoli:sbx}"
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

# 1. Build the kit's image if it is missing, or when explicitly forced.
if [[ "${FORCE_BUILD:-}" == "1" ]] || ! docker image inspect "$image" >/dev/null 2>&1; then
  version="${YOLI_VERSION:-}"
  if [[ -z "$version" ]] && git -C "$repo_root" rev-parse --git-dir >/dev/null 2>&1; then
    version="$(git -C "$repo_root" describe --tags --dirty --always 2>/dev/null || true)"
  fi
  version="${version:-dev}"
  echo "sbx: building $image (version=$version)" >&2
  docker build -f "$repo_root/Dockerfile.sbx" \
    --build-arg "YOLI_VERSION=$version" -t "$image" "$repo_root"
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
    subprocess.run(
        [sbx, "secret", "set-custom", "-g", "--host", host,
         "--placeholder", placeholder, "--value", value],
        check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

res = {}
if cfg.get("default_provider"):
    res["default_provider"] = cfg["default_provider"]
provs = {}
for pname, p in (cfg.get("providers") or {}).items():
    if not isinstance(p, dict):
        continue
    entry = {}
    if p.get("base_url"): entry["base_url"] = p["base_url"]
    if p.get("model"):    entry["model"] = p["model"]
    for k in ("context_window", "max_tokens"):
        if k in p: entry[k] = p[k]
    placeholder = f"yoli-sbx-{pname}"
    entry["api_key"] = placeholder            # inert; proxy swaps it on egress
    provs[pname] = entry
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

# 3. Launch yoli in the sandbox via the kit. Re-attach if it already exists;
#    otherwise create it (agent name "yoli" must match the kit's name).
if "$sbx" ls 2>/dev/null | awk 'NR>1 {print $1}' | grep -qx "$name"; then
  exec "$sbx" run --name "$name"
fi
exec "$sbx" run --kit "$kit_dir" --name "$name" yoli "$workspace"
