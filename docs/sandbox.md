# Running yoli in a sandbox

yoli's agent reads files, runs arbitrary shell commands through the `Bash`
tool, and reaches the network — so you may want to run it isolated from your
host. yoli ships a [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)
**kit** that runs the agent inside a microVM with its own filesystem and
network, against whichever repository you launch it from.

## Prerequisites

- Docker.
- The **`sbx`** CLI (Docker Sandboxes). It installs to `~/.docker/sbx/bin/sbx`
  and is not always on `PATH`; `scripts/sbx.sh` finds it there automatically.

## Usage

`scripts/sbx.sh` is the single entry point. Run it from any repository:

```bash
scripts/sbx.sh                        # yoli TUI in a sandbox on the current dir
```

It opens yoli's TUI inside the sandbox. To run other yoli commands in the same
sandbox, use `sbx exec` (the sandbox is named `yoli-<dirname>`):

```bash
sbx exec yoli-<dirname> yoli chat "list the files here"
```

Run it against another repo by invoking it from there. For convenience, symlink
it onto your `PATH` (the script resolves symlinks):

```bash
ln -sf "$PWD/scripts/sbx.sh" ~/.local/bin/yoli-sbx
cd /some/other/repo && yoli-sbx
```

Knobs (environment variables): `IMAGE` (image tag, default `yoli:sbx`), `NAME`
(sandbox name, default `yoli-<dirname>`), `FORCE_BUILD=1` (rebuild the image).

## How your API keys stay out of the sandbox

yoli reads its provider profiles only from `~/.config/yoli/config.json` (see
[configuration.md](configuration.md)). Rather than mounting that file — which
would expose your real keys to the agent — `scripts/sbx.sh`:

1. Writes a **placeholder** config into the kit, identical to yours but with each
   `api_key` replaced by an inert per-provider placeholder (`yoli-sbx-<provider>`).
   The kit delivers it to `~/.config/yoli/config.json` inside the sandbox.
2. Registers each real key as a host-side
   [proxy-managed secret](https://docs.docker.com/ai/sandboxes/security/credentials/)
   (`sbx secret set-custom`), keyed by the provider's host and the same
   placeholder.

At runtime yoli sends the placeholder as its auth header; the host-side proxy
swaps in the real key on the outbound request. The real keys never enter the
sandbox's filesystem or environment. Both the placeholder config and the secrets
are derived from `~/.config/yoli/config.json`, which stays the single source of
truth.

## What's in the repo

| Path | Purpose |
|---|---|
| `scripts/sbx.sh` | Entry point: build image, register secrets, launch the kit. |
| `deploy/yoli-kit/spec.yaml` | Kit: defines the `yoli` agent, image, entrypoint, and the Brave host allow-rule. |
| `deploy/yoli-kit/files/…/config.json` | Placeholder config (generated; git-ignored). |
| `Dockerfile.sbx` | Builds `yoli:sbx` = the `shell` sandbox template + the yoli binary. |

## Testing

The `sandbox`-created agent should answer normally; a `401` would mean a key
never reached the provider. Verify with a free model without spending tokens
(`ten` here is `tencent/hy3:free` on OpenRouter — adjust to a provider you have):

```bash
sbx exec yoli-<dirname> yoli chat --provider ten "reply with exactly: it works"
```

Confirm no real keys leaked into the sandbox:

```bash
sbx exec yoli-<dirname> bash -lc \
  'grep -o "yoli-sbx-[a-z]*" ~/.config/yoli/config.json; \
   grep -rE "sk-|BSA" ~ 2>/dev/null && echo LEAK || echo "no real keys"'
```

## Cleanup

```bash
sbx ls                                 # list sandboxes
sbx rm --force yoli-<dirname>          # remove a sandbox
sbx secret ls                          # list stored proxy secrets
```

## Notes and limitations

- `sbx kit` and `sbx secret set-custom` are **experimental** in the current
  Docker Sandboxes release; flags may change.
- Placeholders are unique **per provider**, so two profiles on the same host
  (e.g. two OpenRouter models) each get their own secret.
- `openrouter.ai` and `*.proxy.runpod.net` are allowed by the default network
  policy; the Brave search host is allowed by the kit. A new provider on a host
  outside those needs an entry in the kit's `network.allowedDomains`.
- The kit does **not** use its own `credentials`/`serviceAuth` wiring: this sbx
  build does not inject env-sourced kit credentials at runtime, so injection is
  done with `sbx secret set-custom` instead.

See also [configuration.md](configuration.md) and [self-hosting.md](self-hosting.md).
