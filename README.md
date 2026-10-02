# yoli

<p align="center">
  <img src="docs/assets/yoli-logo.png" alt="yoli logo" width="128" height="128">
</p>

A small, provider-agnostic coding-agent CLI written in Go.

https://github.com/user-attachments/assets/93ee9f20-f867-400a-9ccf-06af28e14edd

> **⚠️ Experimental:** This project is in early development and may have breaking changes, bugs, or incomplete features. Use at your own risk.

## Why yoli

A coding agent reads your files, runs shell commands, and reaches the network,
so the tool itself is part of your security boundary. yoli keeps that boundary
small and explicit: system prompts, tool definitions, and execution policies
all live in this repo as plain source you can read, diff, and pin. Nothing is
remotely controlled or silently swapped — if behavior changes, you changed it.
Being provider-agnostic is part of the same idea: you pick the model, and no
vendor can downgrade it underneath you.

The dependency tree is deliberately tiny (just `golang.org/x/*` and
`gopkg.in/yaml.v3`), pinned in `go.sum` so a tampered dependency fails the build
instead of slipping through. Go has no install-time scripts, so fetching or
building never runs arbitrary code.

- **Single static binary** — `go build` produces one self-contained executable; what you build is exactly what runs.
- **No hidden execution** — Go doesn't run third-party code during dependency fetch or build.
- **Built-in integrity checks** — `go.sum` and the Go checksum database ensure dependencies can't silently change.

## Quick start

You need [Go 1.23](https://go.dev/dl/) or newer. yoli is developed and tested
on Arch Linux; other Linux distributions should work but are unverified.

**1. Install**

```bash
git clone https://github.com/cpotech/yoli.git
cd yoli
go install ./cmd/yoli      # installs to $(go env GOPATH)/bin — keep that on your PATH
yoli version
```

**2. Add a provider.** yoli reads its settings only from
`~/.config/yoli/config.json`, never from environment variables. Any
OpenAI-compatible endpoint works; for example, OpenRouter:

```json
{
  "default_provider": "openrouter",
  "providers": {
    "openrouter": {
      "base_url": "https://openrouter.ai/api/v1",
      "api_key": "sk-or-v1-…",
      "model": "openai/gpt-4o"
    }
  }
}
```

The model must support tool calling. See
[docs/configuration.md](docs/configuration.md) for every field, and
[docs/self-hosting.md](docs/self-hosting.md) to run your own model.

**3. Run it** from the project you want to work on:

```bash
cd ~/code/my-project
yoli tui                         # interactive session
yoli chat "explain this repo"    # one-shot
```

To keep the agent off your host, use `yoli-sbx` instead (next section).

## Run sandboxed: `yoli-sbx`

The agent's `Bash` tool runs arbitrary commands. `yoli-sbx` runs yoli inside a
[Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) microVM instead: the
agent sees only your project directory, and **your API keys never enter the
sandbox**. A host-side proxy adds them to outgoing requests.

**One-time setup.** You need Docker, the `sbx` CLI (Docker Sandboxes) and
`python3`. Then, from this repo:

```bash
ln -sf "$PWD/scripts/sbx.sh" ~/.local/bin/yoli-sbx   # ~/.local/bin must be on your PATH
```

**Use it** from any project, with the same commands as `yoli`:

```bash
cd ~/code/my-project
yoli-sbx                             # TUI in a sandbox
yoli-sbx tui --provider openrouter   # pick a provider profile
yoli-sbx chat "explain this repo"    # any yoli command works
yoli-sbx acp                         # for your editor (docs/acp.md)
```

The first run in a directory builds the image and creates a sandbox named
`yoli-<dirname>`, which takes a while; later runs start in a couple of seconds.
It uses your normal `~/.config/yoli/config.json`, and it refuses to run from
your home directory. See [docs/sandbox.md](docs/sandbox.md) for managing
sandboxes, editor setup, and how keys are handled.

## Commands

| Command | What it does |
|---|---|
| `yoli tui` | Interactive REPL (see [docs/yoli-tui.md](docs/yoli-tui.md)). |
| `yoli chat <prompt>` | One-shot agent chat. |
| `yoli -p <prompt>` / `--prompt <prompt>` | Shorthand for `chat`. |
| `yoli acp` | Serve the Agent Client Protocol over stdio so editors (Zed, CodeCompanion.nvim, avante.nvim) can run yoli as an agent (see [docs/acp.md](docs/acp.md)). |
| `yoli run --role <role>` | Run the stdio agent with the given role (`coder`, `planner`, `reviewer`). |
| `yoli agent [flags]` | Run the headless agent loop and emit Yolium NDJSON progress/complete events on stdout. |
| `yoli session list \| current \| tree \| branch` | Inspect and operate on session files. |
| `yoli skills list` / `show <name>` | Inspect skills available to the agent (see [Skills](#skills)). |
| `yoli provider list` | List your provider profiles (API keys are never printed). |
| `yoli config path \| get \| set \| list \| providers` | Inspect or change the config file (see [docs/configuration.md](docs/configuration.md)). |
| `yoli version` | Print the CLI version. |

Common flags:

- `--provider <name>` selects a provider profile (`chat`, `tui`, `run`, `agent`, `acp`).
- `--loglevel debug|info|error|none` sets log verbosity (`chat`, `-p`, `tui`).
- `-c`, `-r`, `--session <path|id>`, `--fork <path|id>`, `--no-session` control sessions (see [Sessions](#sessions)).

### `yoli agent` flags

| Flag | Equivalent env var | Description |
|---|---|---|
| `--provider <name>` | *(none)* | Provider profile to use (default: the `default_provider` config key). |
| `--model <slug>` | `AGENT_MODEL` | Override the profile's model; sent to the backend verbatim. |
| `--tools <a,b,c>` | `AGENT_TOOLS` | Comma-separated tool whitelist; defaults to all tools except `ask_question` (which is always excluded in headless mode). |
| `--prompt <text>` | `AGENT_PROMPT` (base64) | Inline prompt text. |
| `--prompt-file <path>` | `AGENT_PROMPT_FILE` | Read prompt from a file. |
| *(env only)* | `AGENT_GOAL` (base64) | Optional goal injected as a separate user message. |
| `--session <path\|id>` | `AGENT_SESSION` | Resume a specific session by path, full id, or unique prefix. |
| `--fork <path\|id>` | `AGENT_FORK` | Fork a source session into a new session whose `parentSession` is the source. |
| `--continue` | `AGENT_CONTINUE` | Continue the most recent session for the cwd. |
| `--no-session` | *(none)* | Run without writing a session file. |
| `--yolium-mode` | *(none)* | Register the `yolium_*` protocol tools for the Yolium orchestrator; leave off for standalone runs. |
| `--events-fd <N>` | *(none)* | Write structured NDJSON events to file descriptor N (Yolium passes 3). |

Output is the Yolium NDJSON protocol (`progress` and `complete` events), not Claude Code's `stream-json`. There is no `--output-format`, `--allowedTools`, `--dangerously-skip-permissions`, or `--verbose` flag.

## Providers and configuration

Endpoints are named **provider profiles** under the `providers` key of
`~/.config/yoli/config.json`; a project's `.yolirc.json` can add or override
them. Pick one with `--provider <name>`, the `default_provider` key, or
`/provider` inside the TUI (`/providers` just lists them). Environment
variables are never read for settings.

| Profile field | Description |
|---|---|
| `base_url` | OpenAI-compatible endpoint (required), e.g. `https://openrouter.ai/api/v1` or a self-hosted vLLM server ([docs/self-hosting.md](docs/self-hosting.md)). |
| `api_key` | Required. Sent as `Authorization: Bearer <key>`. |
| `model` | Model identifier sent to the backend verbatim. |
| `context_window` | Total context window in tokens (input + output). Set to your server's cap (e.g. a vLLM `max_model_len` of 32768) so the loop reserves output headroom and never overflows. Default 180000. |
| `max_tokens` | Per-turn output-token cap (default 8192); lower it to leave more of the window for input. |
| `include_reasoning` | When `true`, show the model's chain-of-thought as a `thinking` line. Default `false`. |

The only other config keys are `default_provider` and `BRAVE_API_KEY` (for the
`WebSearch` tool). A deterministic `faux` provider exists for tests only. See
[docs/configuration.md](docs/configuration.md) and
[docs/providers.md](docs/providers.md).

## Sessions

`yoli chat`, `tui`, `agent` and `acp` auto-save conversations as JSONL under
`~/.yoli/agent/sessions/<cwd-bucket>/<id>.jsonl` (opt out with `--no-session`).
Resume the latest with `-c`, pick one interactively with `-r`, target a specific
one with `--session <path|id>`, or fork with `--fork <path|id>`. See
[docs/session-format.md](docs/session-format.md) for the on-disk format and
[`yoli session`](#commands) for inspection.

## Skills

A skill is a `SKILL.md` with YAML frontmatter that packages a focused
methodology the agent adopts on demand. `yoli agent`, `chat`, and `tui`
advertise available skills in the system prompt; the model fetches a
skill's body with the `Skill` tool when the task matches its trigger. In
the TUI you can also pin one yourself — **Shift-Tab** cycles the active
skill (shown in the prompt as `[plan] > `), or use `/skill <name|off>`.

Three skills are built into the binary: `plan` (an implementation plan, no
code), `code` (implement the plan with tests) and `verify` (read-only review).
For coding requests the agent runs them in that order: plan → code → verify.

Skills load from `./.yoli/skills/` (project), `~/.yoli/skills/` (user),
and the built-ins. Project overrides user overrides built-in. See
[docs/skills.md](docs/skills.md).

## Git workflow

All git operations go through `Bash` (there are no dedicated git tools). The
agent leaves its changes uncommitted: you review them and own the final commit.
Under the Yolium orchestrator, the host owns branch creation, push, and PR
opening. A policy in `Bash` blocks the well-known footguns (`git push`,
branch-creating `checkout -b` / `switch -c`, `git reset --hard`,
`git stash drop`, `gh pr create`) — see `internal/agent/tools/bash_policy.go`.
The policy is a guard rail, not a security boundary; for isolation, use
[`yoli-sbx`](#run-sandboxed-yoli-sbx).

## Layout

```
cmd/yoli/                 # main package → `yoli` binary
internal/
  ai/                     # provider-agnostic chat types + Provider interface
    providers/            # openai-compatible (OpenRouter, vLLM, …), faux
  agent/                  # agent loop, roles, stdio runner
    context/              # AGENTS.md loader
    session/              # JSONL session store (branching, fork/resume)
    skills/               # loader, injector, expander
    tools/                # Read, Write, LS, Bash, Edit, Glob, Grep, WebSearch, Agent, Skill
    yolium/               # NDJSON protocol + bridge tools
  cli/                    # command surface (chat, tui, run, agent, acp, session, skills, provider, config)
skills/                   # built-in skills (plan, code, verify), embedded into the binary via go:embed
scripts/                  # build.sh, release.sh, sbx.sh (yoli-sbx)
deploy/yoli-kit/          # Docker Sandboxes kit used by yoli-sbx
```

`internal/` keeps every package unimportable from outside the module.

## Tests

```bash
go test ./...
```

## Building & versions

Every build stamps a version into the binary (`yoli/internal/cli.Version`),
reported by `yoli version`. The version comes from `git describe --tags
--dirty --always`:

- with a reachable tag: `v0.1.0` or `v0.1.0-3-ga011326-dirty` (commits since tag + sha + dirty tree),
- without a tag: the short commit sha (optionally `-dirty`),
- with no git or linker flag: `dev`.

The version is applied consistently across build paths:

- **Host build** — `scripts/build.sh` (honors `GOOS`/`GOARCH`, `OUTPUT`, `YOLI_VERSION`).
- **Releases** — `scripts/release.sh <version>` (e.g. `v0.2.0`) creates an annotated git tag and cross-compiles versioned binaries into `dist/` (`yoli-<os>-<arch>` for linux/darwin × amd64/arm64), rebuilding the root `yoli` with the same version. Push the tag with `git push origin <version>` to publish.
- **Sandbox image** — `scripts/sbx.sh` builds `yoli:sbx` from `Dockerfile.sbx` with the same version string.

## Docs

- [Sandbox (`yoli-sbx`)](docs/sandbox.md) — run the agent isolated from your host
- [TUI](docs/yoli-tui.md) — keys and slash commands
- [Configuration](docs/configuration.md) — config files and provider profiles
- [Providers](docs/providers.md)
- [Self-hosting](docs/self-hosting.md) — run your own model (vLLM on RunPod)
- [ACP server](docs/acp.md) — run yoli inside editors
- [Skills](docs/skills.md)
- [Session format](docs/session-format.md)
- [Architecture](docs/architecture.md)
- [Feature plans](docs/features/README.md) — planned work, not yet implemented

## License

MIT — see [LICENSE](LICENSE).
