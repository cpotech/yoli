# Configuration

Yoli reads configuration from four layered sources, with later sources
taking precedence:

1. **Defaults** — built into the CLI.
2. **User config** — `~/.config/yoli/config.json` (or
   `$XDG_CONFIG_HOME/yoli/config.json` if `XDG_CONFIG_HOME` is set).
3. **Project config** — `.yolirc.json` in the current working directory.
4. **Environment** — `OPENROUTER_API_KEY`, `OPENROUTER_MODEL`, etc.

The effective value of every key, plus the source it came from, is visible
through `yoli config list`.

## Recognised keys

| Key | Env var | Purpose |
|---|---|---|
| `default_provider` | — | Reserved: one of `openrouter`, `faux`. Not yet read by `chat`/`run`. |
| `default_model` | `OPENROUTER_MODEL` | Model identifier passed to OpenRouter. Exported into the env by `ApplyEnvDefaults`. |
| `default_role` | — | Reserved: default role prompt for `yoli run`. Not yet read by `run`. |
| `openrouter_api_key` | `OPENROUTER_API_KEY` | Credential for OpenRouter. Exported into the env by `ApplyEnvDefaults`. |
| `brave_api_key` | `BRAVE_API_KEY` | Credential for the `WebSearch` tool (Brave Search API). Exported into the env by `ApplyEnvDefaults`. |
| `subagent_max_depth` | — | Reserved: maximum nesting depth for the `Agent` tool. |

Unknown keys in a config file are ignored with a warning on stderr.

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
```

`yoli config list` annotates each row with one of `env`, `project`, `user`,
or `default`, so you can see at a glance where a value originates.

## File format

Both `~/.config/yoli/config.json` and `./.yolirc.json` are JSON objects
mapping known keys to string values:

```json
{
  "default_provider": "openrouter",
  "default_model": "openrouter:openai/gpt-4o",
  "openrouter_api_key": "sk-or-v1-…"
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
