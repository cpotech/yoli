# Running yoli in a sandbox

yoli's agent reads files, runs arbitrary shell commands through the `Bash`
tool, and reaches the network — so you may want to run it isolated from your
host. yoli ships a [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)
**kit** that runs the agent inside a microVM with its own filesystem and
network, against whichever repository you launch it from. The launch directory
is the only host path the sandbox sees (mounted read-write at the same absolute
path); your home directory, SSH keys and Docker socket are not visible inside.
The script refuses to launch from a directory that contains your home directory
or `~/.config/yoli` (such as `~` or `/`), since that would expose them.

## Prerequisites

- Docker.
- The **`sbx`** CLI (Docker Sandboxes). It installs to `~/.docker/sbx/bin/sbx`
  and is not always on `PATH`; `scripts/sbx.sh` finds it there automatically.
- `python3`. The script uses it to read your yoli config and `sbx`'s JSON
  output.

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

Knobs (environment variables): `NAME` (sandbox name, default `yoli-<dirname>`),
`FORCE_BUILD=1` (rebuild the image, e.g. after changing yoli). If a sandbox with
that name already exists for a different directory (`~/a/app` and `~/b/app`
both default to `yoli-app`), the script stops rather than reuse it; set `NAME`.

sbx runs images from its own store, not Docker's, so the script builds
`yoli:sbx` with Docker and loads it into sbx (`sbx template load`) when sbx does
not have it yet, or when forced. `sbx template ls` lists what sbx has.

## From your editor

With arguments, `scripts/sbx.sh` runs `yoli <args>` in the sandbox for the
current directory (creating it on first use) and passes stdin/stdout straight
through. So an editor can keep running on the host while the agent — and every
command its `Bash` tool runs — stays in the sandbox. For CodeCompanion over ACP
(see [acp.md](acp.md)), with the script linked onto `PATH` as `yoli-sbx`:

```lua
commands = {
  default = { "yoli-sbx", "acp", "--provider", "ten" },
},
```

The editor's working directory picks the sandbox (`yoli-<dirname>`). The editor
edits the files on the host and the agent edits the same files through the
mount, so both see each other's changes. `yoli acp` does all file I/O and
commands itself (it never asks the editor to via `fs/*` or `terminal/*`), so
none of the agent's actions run on the host through the editor. The first start
builds the image and creates the sandbox, which can outlast an editor's connect
timeout — run `scripts/sbx.sh` once in a new project first; later starts take a
couple of seconds.

## Running a Next.js app

The image carries Node 22 with npm 11, pnpm and yarn, and the default network
policy allows the npm registry, GitHub and Google Fonts, so the agent can
`create-next-app`, `npm install`, `npm run build` and `npm run dev` in the
sandbox. The kit publishes container port 3000 (`next dev`) to a free localhost
port on the host:

```bash
sbx ports yoli-<dirname>              # e.g. 127.0.0.1:32771->3000/tcp
```

Publish other ports with `sbx ports yoli-<dirname> --publish <port>`.

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
| `deploy/yoli-kit/spec.yaml` | Kit: defines the `yoli` agent, image, entrypoint, the Brave host allow-rule, and published port 3000. |
| `deploy/yoli-kit/files/…/config.json` | Placeholder config (generated; git-ignored). |
| `Dockerfile.sbx` | Builds `yoli:sbx` = the `shell` sandbox template + npm 11, pnpm, yarn + the yoli binary. |

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

- **The project directory is shared, so the sandbox limits what the agent
  runs, not what it writes.** The agent can leave files there that your host
  later executes: git hooks (`.git/hooks`, which `git status` does not show),
  `.envrc`, editor project config (`.nvim.lua`, `.vscode/`), `Makefile` or
  `package.json` scripts. Check what changed before running project commands on
  the host. For stronger isolation, Docker Sandboxes' `sbx create --clone`
  gives the agent a private clone instead of the shared directory;
  `scripts/sbx.sh` does not use it because the editor-on-host workflow needs
  the shared files.
- The template's own npm (Ubuntu's 9.2) fails installs through the sandbox
  proxy with `ECONNRESET`, and can leave truncated native binaries behind (a
  Next.js build then dies with `Bus error`); the image puts npm 11 ahead of it.

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
