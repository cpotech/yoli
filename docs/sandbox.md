# Running yoli in a sandbox (`yoli-sbx`)

yoli's agent reads files, runs arbitrary shell commands through the `Bash`
tool, and reaches the network — so you may want to run it isolated from your
host. `yoli-sbx` runs yoli inside a
[Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) microVM with its own
filesystem and network, against whichever project you launch it from. The
launch directory is the only host path the sandbox sees (mounted read-write at
the same absolute path); your home directory, SSH keys and Docker socket are
not visible inside, and **your API keys never enter the sandbox**.

## Setup (once)

You need:

- Docker.
- The **`sbx`** CLI (Docker Sandboxes). It installs to `~/.docker/sbx/bin/sbx`
  and is not always on `PATH`; `yoli-sbx` finds it there automatically.
- `python3`. The script uses it to read your yoli config and `sbx`'s JSON
  output.
- Your provider profiles in `~/.config/yoli/config.json`, as for plain `yoli`
  (see [configuration.md](configuration.md)).

Then link the launcher onto your `PATH` as `yoli-sbx`, from the yoli repo:

```bash
ln -sf "$PWD/scripts/sbx.sh" ~/.local/bin/yoli-sbx   # ~/.local/bin must be on your PATH
```

`yoli-sbx` is just `scripts/sbx.sh`; the script resolves the symlink to find
the rest of the repo, so keep the clone where it is.

## Usage

Run `yoli-sbx` from the project you want the agent to work on. With no
arguments it opens the TUI; with arguments it runs `yoli <arguments>`:

| Command | What it does |
|---|---|
| `yoli-sbx` | yoli TUI in a sandbox on the current directory. |
| `yoli-sbx tui --provider <name>` | The TUI with a specific provider profile. |
| `yoli-sbx chat "…"` | One-shot chat. |
| `yoli-sbx acp` | ACP server for your editor (see [From your editor](#from-your-editor)). |
| `yoli-sbx <any yoli command>` | e.g. `yoli-sbx version`, `yoli-sbx provider list`. |

```bash
cd ~/code/my-project
yoli-sbx
```

The first run in a directory builds the `yoli:sbx` image and creates a sandbox
named `yoli-<dirname>`, which takes a while. Later runs reuse that sandbox
(starting it if it was stopped) and take a couple of seconds.

Two safety checks stop the launch:

- **Home directory.** It refuses to run from a directory that contains your
  home directory or `~/.config/yoli` (such as `~` or `/`), since the sandbox
  would then see your keys. Run it from a project directory.
- **Name clash.** If `yoli-<dirname>` already exists for a *different*
  directory (`~/a/app` and `~/b/app` both default to `yoli-app`), it stops
  rather than let the agent work on the wrong project. Pick another name with
  `NAME`.

Environment variables:

| Variable | Effect |
|---|---|
| `NAME=<name>` | Sandbox name (default `yoli-<dirname>`). |
| `FORCE_BUILD=1` | Rebuild the image, e.g. after changing yoli's code. |

sbx runs images from its own store, not Docker's, so `yoli-sbx` builds
`yoli:sbx` with Docker and loads it into sbx (`sbx template load`) when sbx does
not have it yet, or when `FORCE_BUILD=1`. After a rebuild, check
`yoli-sbx version`; if a sandbox still runs the old yoli, remove it (below) and
run `yoli-sbx` again.

## Managing sandboxes

These use the `sbx` CLI directly. If it isn't on your `PATH`, use the full path
`~/.docker/sbx/bin/sbx` or add `~/.docker/sbx/bin` to your `PATH`.

```bash
sbx ls                                 # list sandboxes and the directory each one mounts
sbx stop yoli-<dirname>                # stop a sandbox (yoli-sbx restarts it)
sbx rm --force yoli-<dirname>          # remove a sandbox
sbx ports yoli-<dirname>               # show published ports
sbx secret ls                          # list stored proxy secrets (keys are masked)
sbx secret rm -g --placeholder yoli-sbx-<provider>   # remove a key for a profile you deleted
```

## From your editor

`yoli-sbx acp` passes stdin/stdout straight through to `yoli acp` in the
sandbox. So an editor can keep running on the host while the agent — and every
command its `Bash` tool runs — stays in the sandbox. Use `yoli-sbx acp`
wherever an editor setup in [acp.md](acp.md) says `yoli acp`. For
CodeCompanion:

```lua
commands = {
  default = { "yoli-sbx", "acp" },
  -- or pick a profile: { "yoli-sbx", "acp", "--provider", "openrouter" },
},
```

The editor's working directory picks the sandbox (`yoli-<dirname>`). The editor
edits the files on the host and the agent edits the same files through the
mount, so both see each other's changes. `yoli acp` does all file I/O and
commands itself (it never asks the editor to via `fs/*` or `terminal/*`), so
none of the agent's actions run on the host through the editor.

The first start builds the image and creates the sandbox, which can outlast an
editor's connect timeout — run `yoli-sbx` once in a new project first.

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
would expose your real keys to the agent — `yoli-sbx`:

1. Writes a **placeholder** config into the kit, identical to yours but with each
   `api_key` replaced by an inert per-provider placeholder (`yoli-sbx-<provider>`).
   The kit delivers it to `~/.config/yoli/config.json` inside the sandbox.
2. Registers each real key as a host-side
   [proxy-managed secret](https://docs.docker.com/ai/sandboxes/security/credentials/)
   (`sbx secret set-custom`), keyed by the provider's host and the same
   placeholder. If registering a key fails, it prints a warning naming the host.

At runtime yoli sends the placeholder as its auth header; the host-side proxy
swaps in the real key on the outbound request. The real keys never enter the
sandbox's filesystem or environment. Both the placeholder config and the secrets
are refreshed from `~/.config/yoli/config.json` on every run, so it stays the
single source of truth. A running sandbox picks up config changes when it next
starts (`sbx stop yoli-<dirname>`, then `yoli-sbx`).

## What's in the repo

| Path | Purpose |
|---|---|
| `scripts/sbx.sh` | The `yoli-sbx` launcher: build the image, register secrets, create and run the sandbox. |
| `deploy/yoli-kit/spec.yaml` | Kit: defines the `yoli` agent, image, entrypoint, the Brave host allow-rule, and published port 3000. |
| `deploy/yoli-kit/files/…/config.json` | Placeholder config (generated; git-ignored). |
| `Dockerfile.sbx` | Builds `yoli:sbx` = the `shell` sandbox template + npm 11, pnpm, yarn + the yoli binary. |

## Testing

A sandboxed yoli should answer normally; a `401` means a key never reached the
provider. Check with a free model so it costs nothing (`ten` here is a profile
for `tencent/hy3:free` on OpenRouter — use one you have):

```bash
yoli-sbx chat --provider ten "reply with exactly: it works"
```

Confirm no real keys leaked into the sandbox:

```bash
sbx exec yoli-<dirname> bash -lc \
  'grep -o "yoli-sbx-[a-z]*" ~/.config/yoli/config.json; \
   grep -rE "sk-|BSA" ~ 2>/dev/null && echo LEAK || echo "no real keys"'
```

## Notes and limitations

- **The project directory is shared, so the sandbox limits what the agent
  runs, not what it writes.** The agent can leave files there that your host
  later executes: git hooks (`.git/hooks`, which `git status` does not show),
  `.envrc`, editor project config (`.nvim.lua`, `.vscode/`), `Makefile` or
  `package.json` scripts. Check what changed before running project commands on
  the host. For stronger isolation, Docker Sandboxes' `sbx create --clone`
  gives the agent a private clone instead of the shared directory;
  `yoli-sbx` does not use it because the editor-on-host workflow needs the
  shared files.
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
