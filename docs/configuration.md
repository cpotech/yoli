# Configuration

Yoli reads configuration from four layered sources, with later sources
taking precedence:

1. **Defaults** — built into the CLI.
2. **User config** — `~/.config/yoli/config.json` (or
   `$XDG_CONFIG_HOME/yoli/config.json` if `XDG_CONFIG_HOME` is set).
3. **Project config** — `.yolirc.json` in the current working directory.
4. **Environment** — `YOLI_API_KEY`, `YOLI_MODEL`, etc.

The effective value of every key, plus the source it came from, is visible
through `yoli config list`.

## Recognised keys

| Key | Env var | Purpose |
|---|---|---|
| `YOLI_PROVIDER` | `YOLI_PROVIDER` | Name of the provider profile to activate (see [Provider profiles](#provider-profiles)). As an env var it is an explicit selection: an unknown name is an error. |
| `default_provider` | — | Name of the provider profile used when no `--provider` flag or `YOLI_PROVIDER` env var is given. An unknown name warns and falls back to the flat keys below. |
| `default_model` | `YOLI_MODEL` | Model identifier sent to the backend verbatim. Exported into the env by `ApplyEnvDefaults`. |
| `default_role` | — | Reserved: default role prompt for `yoli run`. Not yet read by `run`. |
| `base_url` | `YOLI_BASE_URL` | Any OpenAI-compatible endpoint (required). See [self-hosting.md](self-hosting.md). Exported into the env by `ApplyEnvDefaults`. |
| `api_key` | `YOLI_API_KEY` | Credential for the endpoint, sent as `Authorization: Bearer <key>`. Exported into the env by `ApplyEnvDefaults`. |
| `brave_api_key` | `BRAVE_API_KEY` | Credential for the `WebSearch` tool (Brave Search API). Exported into the env by `ApplyEnvDefaults`. |
| `subagent_max_depth` | — | Reserved: maximum nesting depth for the `Agent` tool. |
| `YOLI_CONTEXT_WINDOW` | `YOLI_CONTEXT_WINDOW` | Total context window in tokens (input + output) the backend accepts. Set this to your server's cap (e.g. a vLLM `max_model_len` of 32768) so the loop reserves output headroom before compacting input, keeping requests within the window. Defaults to 180000. Invalid or non-positive values warn on stderr and fall back to the default. |
| `YOLI_MAX_TOKENS` | `YOLI_MAX_TOKENS` | Per-turn output-token cap (default 8192). Lower it to leave more of the window for input. Invalid or non-positive values warn on stderr and fall back to the default. |

Unknown keys in a config file are ignored with a warning on stderr. The
`providers` key is special: it holds structured profile objects rather
than a flat string value (next section).

## Provider profiles

The `providers` object maps profile names to OpenAI-compatible endpoint
definitions, so several backends can be configured at once and selected
per invocation:

```json
{
  "default_provider": "runpod",
  "providers": {
    "runpod": {
      "base_url": "https://mypod.proxy.runpod.net/v1",
      "api_key": "…",
      "model": "InternScience/Agents-A1",
      "context_window": 57344,
      "max_tokens": 8192
    },
    "openrouter": {
      "base_url": "https://openrouter.ai/api/v1",
      "api_key": "sk-or-v1-…",
      "model": "openai/gpt-4o"
    }
  }
}
```

Profile fields: `base_url`, `api_key`, `model`, `context_window`,
`max_tokens`. The last three are optional and fall back to the flat
keys / environment; note that `context_window` and `max_tokens` are JSON
numbers here, not strings. Profiles work in both the user config and the
project `.yolirc.json`; a project profile replaces a same-named user
profile wholesale.

A profile is selected with precedence `--provider` flag >
`YOLI_PROVIDER` env var > `YOLI_PROVIDER`/`default_provider` config key.
When a profile is active, its fields outrank the flat keys but the shell
environment still wins overall. With no selection the flat `YOLI_*` keys
behave exactly as before, so existing configs keep working.

Profiles are edited by hand in the JSON file; `yoli config providers`
lists them (API keys are never printed), and the TUI's `/provider`
command switches between them at runtime.

> **Migration note:** the `openrouter_api_key` key and the
> `OPENROUTER_API_KEY` / `OPENROUTER_MODEL` env vars were retired in favor
> of the provider-neutral `api_key` / `YOLI_API_KEY` / `YOLI_MODEL` above.
> Stale config files trigger a rename hint on stderr.

## Working with config from the CLI

```bash
# Show where the user config lives
yoli config path

# Read the effective value of a key
yoli config get default_provider

# Persist a value to the user config file
yoli config set default_provider openrouter

# Show every key with its value and source
yoli config list

# List provider profiles (never prints api keys)
yoli config providers
```

`yoli config list` annotates each row with one of `env`, `project`, `user`,
or `default`, so you can see at a glance where a value originates.

## File format

Both `~/.config/yoli/config.json` and `./.yolirc.json` are JSON objects
mapping known keys to string values:

```json
{
  "default_provider": "openrouter",
  "default_model": "openai/gpt-4o",
  "api_key": "sk-or-v1-…"
}
```

`yoli config set` writes the file with 2-space indent and a trailing
newline, creating the parent directory if needed.

## How config reaches providers

The provider types in `internal/ai/providers` are pure: they read
`os.Getenv`. The CLI is the only layer aware of config files; before
invoking a provider it calls `ApplyEnvDefaults(LoadConfig(...))`, which
exports each env-bound config value into the process environment *only if
the env var is not already set*. The shell environment always wins over
the config file.
