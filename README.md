# yoli

A small, provider-agnostic coding-agent CLI written in Go.

https://github.com/user-attachments/assets/93ee9f20-f867-400a-9ccf-06af28e14edd

> **Note:** yoli has only been developed and tested on Arch Linux. It should
> work on other Linux distributions, but those are currently unverified.

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

## Running in a container (recommended)

The agent can still make mistakes — an unintended `rm`, an overbroad `git`
command, a stray network request — so running yoli in a container confines
those actions to a disposable environment instead of your host.

### Quick way: the wrapper script

[`scripts/yoli-docker.sh`](scripts/yoli-docker.sh) builds the image on first
use and runs yoli with the current directory mounted. Arguments pass straight
through to yoli:

```bash
export OPENROUTER_API_KEY=...           # your key
scripts/yoli-docker.sh       # interactive tui
FORCE_BUILD=1 scripts/yoli-docker.sh ... # rebuild the image first
```

Credentials come from one of two places: `OPENROUTER_API_KEY` (and
`BRAVE_API_KEY`) forwarded from your environment, or — if those aren't set —
your host config at `~/.config/yoli/config.json`, which the script mounts
read-only so yoli reads the stored keys and `default_model` itself. Set them
once with `yoli config set` and the container picks them up. See
[docs/configuration.md](docs/configuration.md).

### Locking down the network (firewall)

Yoli needs the network to reach the model. The firewall mode applies a smarter
policy: **outbound internet is allowed; your LAN, router, and cloud metadata
endpoints are blocked; no inbound access.** Just add `FIREWALL=1` to the
wrapper script:

```bash
export OPENROUTER_API_KEY=...                 # required
FIREWALL=1 scripts/yoli-docker.sh             # interactive tui
FIREWALL=1 scripts/yoli-docker.sh chat "hi"   # one-shot chat
```

Under the hood this runs [`docker-compose.egress.yml`](docker-compose.egress.yml):
a sidecar owns a network namespace and installs iptables rules
([`deploy/egress-firewall.sh`](deploy/egress-firewall.sh)) that the yoli
container joins, so the rules cover every protocol, not just HTTP. Edit those
rules to tighten the policy.

## Layout

```
cmd/yoli/                 # main package → `yoli` binary
internal/
  ai/                     # provider-agnostic chat types + Provider interface
    providers/            # openrouter, faux
  agent/                  # agent loop, roles, stdio runner
    context/              # AGENTS.md loader
    session/              # JSONL session store (branching, fork/resume)
    skills/               # loader, injector, expander
    tools/                # Read, Write, LS, Bash, Edit, Glob, Grep, WebSearch, Agent
    yolium/               # NDJSON protocol + bridge tools
  cli/                    # command surface (chat, tui, run, agent, session, skills, config)
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
| `yoli skills list` / `show <name>` | Inspect skills available to the agent. |
| `yoli config path` | Print the resolved user config file path. |
| `yoli config get <key>` | Print the effective value of a known config key. |
| `yoli config set <key> <value>` | Persist a value into the user config file. |
| `yoli config list` | Print every known key with its value and source (`env`, `project`, `user`, or `default`). |

### `yoli agent` flags

| Flag | Equivalent env var | Description |
|---|---|---|
| `--model <slug>` | `AGENT_MODEL` | OpenRouter model slug (default: `openrouter/free`). |
| `--tools <a,b,c>` | `AGENT_TOOLS` | Comma-separated tool whitelist; defaults to all tools except `ask_question` (which is always excluded in headless mode). |
| `--prompt <text>` | `AGENT_PROMPT` (base64) | Inline prompt text. |
| `--prompt-file <path>` | `AGENT_PROMPT_FILE` | Read prompt from a file. |
| *(env only)* | `AGENT_GOAL` (base64) | Optional goal injected as a separate user message. |
| `--session <path\|id>` | `AGENT_SESSION` | Resume a specific session by path, full id, or unique prefix. |
| `--fork <path\|id>` | `AGENT_FORK` | Fork a source session into a new session whose `parentSession` is the source. |
| `--continue` | `AGENT_CONTINUE` | Continue the most recent session for the cwd. |
| `--no-session` | *(none)* | Run without writing a session file. |
| *(env only)* | `OPENROUTER_API_KEY` | Required. May also come from `yoli config set openrouter_api_key`. |

Output is the Yolium NDJSON protocol (`progress` and `complete` events), not Claude Code's `stream-json`. There is no `--output-format`, `--allowedTools`, `--dangerously-skip-permissions`, or `--verbose` flag.

## Sessions

`yoli chat` and `yoli agent` auto-save conversations as JSONL under
`~/.yoli/agent/sessions/<cwd-bucket>/<id>.jsonl` (opt out with `--no-session`).
Resume the latest with `-c`, pick one interactively with `-r`, target a specific
one with `--session <path|id>`, or fork with `--fork <path|id>`. See
[docs/session-format.md](docs/session-format.md) for the on-disk format and
[`yoli session`](#commands) for inspection.

## Providers

| Provider | Required env var |
|---|---|
| `openrouter` | `OPENROUTER_API_KEY` |
| `faux` | none (deterministic stub for tests) |

Provider credentials and defaults can also be stored via `yoli config set`
so they persist across shells. See [docs/configuration.md](docs/configuration.md).

## Tests

```bash
go test ./...
```

## Docs

- [Architecture](docs/architecture.md)
- [Providers](docs/providers.md)
- [Configuration](docs/configuration.md)
- [Skills](docs/skills.md)

## License

MIT — see [LICENSE](LICENSE).
