# Architecture

Yoli is a single Go module rooted at the repository root. All code lives
under `internal/` so it stays unimportable from outside the module.

## `internal/ai`

Provider-agnostic AI abstractions. Defines the `Provider` and `Tool`
interfaces that the rest of the system targets, plus the built-in providers
under `internal/ai/providers`: `FauxProvider` and `OpenRouterProvider`.

Providers are pure: they read credentials from environment variables and
expose a uniform streaming interface. They have no knowledge of how the
agent is hosted (CLI, Yolium protocol, sub-agent).

## `internal/agent`

The agent loop and its tools. Contains:

- The core `agent.Run` loop that drives a provider through a tool-call
  cycle.
- File-system and shell tools (`Read`, `Write`, `LS`, `Edit`, `Glob`,
  `Grep`, `Bash`) that operate inside the working directory, plus
  `path-safety` checks shared between them, and the `WebSearch` tool.
- The AGENTS.md auto-loader (`internal/agent/context`) and the skills
  loader/injector/expander (`internal/agent/skills`) that inject per-skill
  prompts into the system message.
- The Yolium stdio protocol layer (`internal/agent/yolium`, `@@YOLIUM:`
  framed JSON messages) and the stdio runner (`agent.RunStdio`) that hosts
  an agent over stdin/stdout.
- The `Agent` tool, which lets an agent spawn a child agent against a
  different role prompt by re-invoking the same `yoli` binary.
- The session store (`internal/agent/session`): a JSONL persistence
  layer that records conversations by cwd, supports branching (a single
  file can hold multiple alternate continuations), and exposes
  `Create`/`Open`/`Resolve`/`ForkFrom` for the CLI surfaces.

## `internal/cli`

The `yoli` command-line entry point. Owns:

- Argument parsing for the `version`, `config`, `chat`, `tui`, `run`,
  `agent`, `session`, and `skills` subcommands. Dispatch is a plain
  `switch` in `cli.Run` — no third-party argument parser.
- Session resolution for `chat` and `agent`: parsing `--no-session`,
  `-c`, `--session`, and `--fork`, then handing a `*session.Session` to
  the loop so seed messages, the user prompt, and every assistant/tool
  reply land on disk in one place.
- The user/project config layer (see [configuration.md](configuration.md)).
  The CLI is the only layer that reads config files; providers continue to
  read `os.Getenv`, so the CLI exports stored config values into the
  process environment via `ApplyEnvDefaults` before delegating to the
  agent.

## Dependency direction

```
internal/cli  →  internal/agent  →  internal/ai
```

`internal/ai` has no dependencies on the others. `internal/agent` consumes
provider/tool interfaces from `internal/ai`. `internal/cli` is the only
consumer of `internal/agent`.

## Tests

Each package owns its tests next to its sources (`*_test.go`). CLI tests
re-exec the test binary with `YOLI_CLI_TEST_HELPER=1` so they exercise the
real argv → exit-code surface in an isolated subprocess. `internal/docs`
is a test-only doc guard: it pins `README.md` to the current code so dead
relative links and stale tool names fail the build.

Run everything with:

```bash
go test ./...
```
