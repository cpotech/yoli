# yoli

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

```bash
go build -o yoli ./cmd/yoli
./yoli version
```

Or install onto your `$PATH`:

```bash
go install ./cmd/yoli
# yoli is now available at $(go env GOBIN || echo $(go env GOPATH)/bin)/yoli
```

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
  cli/                    # command surface (chat, tui, run, agent, session, skills, config)
skills/                   # built-in skills (plan, …), embedded into the binary via go:embed
```

`internal/` keeps every package unimportable from outside the module.

## Git workflow

All git operations go through `Bash` (there are no dedicated git tools). Worktree agents commit locally only; the host orchestrator owns branch creation, push, and PR opening. A policy in `Bash` blocks the well-known footguns (`git push`, branch-creating `checkout -b` / `switch -c`, `git reset --hard`, `git stash drop`, `gh pr create`) — see `internal/agent/tools/bash_policy.go`.

## Commands

A global `--loglevel debug|info|error|none` flag may precede any command.

| Command | What it does |
|---|---|
| `yoli version` | Print the CLI version. |
| `yoli chat <prompt>` | One-shot agent chat via OpenRouter. |
| `yoli -p <prompt>` / `--prompt <prompt>` | Shorthand for `chat`. |
| `yoli tui` | Run an interactive line-based REPL (see [docs/yoli-tui.md](docs/yoli-tui.md)). |
| `yoli run --role <role>` | Run the stdio agent with the given role (`coder`, `planner`, `reviewer`). |
| `yoli agent [flags]` | Run the headless agent loop and emit Yolium NDJSON progress/complete events on stdout. |
| `yoli session list \| current \| tree \| branch` | Inspect and operate on session files. |
| `yoli skills list` / `show <name>` | Inspect skills available to the agent (see [Skills](#skills)). |
| `yoli config path` | Print the resolved user config file path. |
| `yoli config get <key>` | Print the effective value of a known config key. |
| `yoli config set <key> <value>` | Persist a value into the user config file. |
| `yoli config list` | Print every known key with its value and source (`project`, `user`, or `default`). |

### `yoli agent` flags

| Flag | Equivalent env var | Description |
|---|---|---|
| `--model <slug>` | `AGENT_MODEL` | Model identifier, sent to the backend verbatim (default: `openrouter/free`). |
| `--tools <a,b,c>` | `AGENT_TOOLS` | Comma-separated tool whitelist; defaults to all tools except `ask_question` (which is always excluded in headless mode). |
| `--prompt <text>` | `AGENT_PROMPT` (base64) | Inline prompt text. |
| `--prompt-file <path>` | `AGENT_PROMPT_FILE` | Read prompt from a file. |
| *(env only)* | `AGENT_GOAL` (base64) | Optional goal injected as a separate user message. |
| `--session <path\|id>` | `AGENT_SESSION` | Resume a specific session by path, full id, or unique prefix. |
| `--fork <path\|id>` | `AGENT_FORK` | Fork a source session into a new session whose `parentSession` is the source. |
| `--continue` | `AGENT_CONTINUE` | Continue the most recent session for the cwd. |
| `--no-session` | *(none)* | Run without writing a session file. |

Connection settings come from the active provider profile in the config
file, not env vars (see [docs/configuration.md](docs/configuration.md)):

| Profile field | Description |
|---|---|
| `api_key` | Required. |
| `base_url` | OpenAI-compatible endpoint (required). See [docs/self-hosting.md](docs/self-hosting.md). |
| `model` | Model identifier sent to the backend verbatim. |
| `context_window` | Total context window in tokens (input + output). Set to your server's cap (e.g. a vLLM `max_model_len` of 32768) so the loop reserves output headroom and never overflows. Default 180000. |
| `max_tokens` | Per-turn output-token cap (default 8192); lower it to leave more of the window for input. |

Output is the Yolium NDJSON protocol (`progress` and `complete` events), not Claude Code's `stream-json`. There is no `--output-format`, `--allowedTools`, `--dangerously-skip-permissions`, or `--verbose` flag.

## Sessions

`yoli chat` and `yoli agent` auto-save conversations as JSONL under
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

Skills load from `./.yoli/skills/` (project), `~/.yoli/skills/` (user),
and the built-ins embedded in the binary — currently `plan`, which
produces a structured implementation plan without writing code. Project
overrides user overrides built-in. See [docs/skills.md](docs/skills.md).

## Providers

| Provider | Configuration |
|---|---|
| openai-compatible | a provider profile: `base_url` + `api_key` (+ `model`, limits) |
| `faux` | none (deterministic stub for tests) |

Any OpenAI-compatible endpoint works — point a profile's `base_url` at
OpenRouter or a self-hosted vLLM server (e.g. on a RunPod GPU pod); see
[docs/self-hosting.md](docs/self-hosting.md). All settings live in
provider profiles in the config file, edited by hand; environment
variables are not read. See
[docs/configuration.md](docs/configuration.md).

Endpoints are named profiles under the `providers` key of the config
file, selected with `--provider <name>` (on `chat`, `tui`, `run`, and
`agent`), the `default_provider` config key, or the `/provider` command
inside the TUI. `yoli config providers` lists the defined profiles.

> **Note:** yoli has only been developed and tested on Arch Linux. It should
> work on other Linux distributions, but those are currently unverified.

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

## Docs

- [Architecture](docs/architecture.md)
- [Providers](docs/providers.md)
- [Configuration](docs/configuration.md)
- [Skills](docs/skills.md)

## License

MIT — see [LICENSE](LICENSE).
