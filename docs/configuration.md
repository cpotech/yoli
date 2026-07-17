# Configuration

Yoli reads all settings from config files — the process environment is
never consulted. Three layered sources, with later sources taking
precedence:

1. **Defaults** — built into the CLI.
2. **User config** — `~/.config/yoli/config.json` (or
   `$XDG_CONFIG_HOME/yoli/config.json` if `XDG_CONFIG_HOME` is set).
3. **Project config** — `.yolirc.json` in the current working directory.

Endpoint settings live exclusively in provider profiles (next section);
only two flat keys exist:

| Key | Purpose |
|---|---|
| `default_provider` | Name of the provider profile used when no `--provider` flag is given. Required unless every invocation passes `--provider`. |
| `BRAVE_API_KEY` | Credential for the `WebSearch` tool (Brave Search API). |

Unknown keys in a config file are ignored with a warning on stderr;
retired flat keys (`YOLI_API_KEY`, `YOLI_BASE_URL`, `YOLI_MODEL`,
`YOLI_CONTEXT_WINDOW`, `YOLI_MAX_TOKENS`, and their older spellings)
warn with a pointer to the provider-profile field that replaced them.

## Provider profiles

The `providers` object maps profile names to OpenAI-compatible endpoint
definitions. A profile is the only way to configure an endpoint —
several backends can be configured at once and selected per invocation:

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

Profile fields:

| Field | Purpose |
|---|---|
| `base_url` | Any OpenAI-compatible endpoint (required). See [self-hosting.md](self-hosting.md). |
| `api_key` | Credential for the endpoint, sent as `Authorization: Bearer <key>` (required). |
| `model` | Model identifier sent to the backend verbatim. |
| `context_window` | Total context window in tokens (input + output) the backend accepts, as a JSON number. Set this to your server's cap (e.g. a vLLM `max_model_len` of 32768) so the loop reserves output headroom before compacting input. Defaults to 180000. |
| `max_tokens` | Per-turn output-token cap, as a JSON number (default 8192). Lower it to leave more of the window for input. |

Profiles work in both the user config and the project `.yolirc.json`; a
project profile replaces a same-named user profile wholesale.

A profile is selected with precedence `--provider` flag >
`default_provider` config key. Failing to resolve a profile — no
profiles defined, nothing selected, or an unknown name — is an error.
Inside the TUI, `/provider [name]` lists profiles or switches the
endpoint, model, and context limits mid-session. `/providers` lists all
profiles without switching. Sub-agents inherit the
parent's active profile and model via `--provider`/`--model` flags on
the spawned `yoli run` process.

Profiles are edited by hand in the JSON file; `yoli provider list` (or
`yoli config providers`) lists them (API keys are never printed).

## Working with config from the CLI

```bash
# Show where the user config lives
yoli config path

# Read the effective value of a flat key
yoli config get default_provider

# Persist a flat key to the user config file
yoli config set default_provider openrouter

# Show every flat key with its value and source
yoli config list

# List provider profiles (never prints api keys)
yoli config providers
```

`yoli config list` annotates each row with one of `project`, `user`, or
`default`, so you can see at a glance where a value originates.
`yoli config set` writes the file with 2-space indent and a trailing
newline, creating the parent directory if needed, and preserves the
`providers` object verbatim.

## How config reaches providers

The provider types in `internal/ai/providers` are pure: they receive
credentials explicitly via `OpenAICompatOptions`. The CLI is the only
layer aware of config files; it resolves the active profile with
`selectProviderProfile` and passes its fields down. Environment
variables carry orchestrator inputs only (`AGENT_*`,
`YOLI_SUBAGENT_DEPTH`), never settings.
