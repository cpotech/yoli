# Providers

Yoli ships two providers, all implementing the `Provider` interface from
`internal/ai`.

## FauxProvider

A deterministic stub used by tests and demos. Replies with canned content
and emits no network traffic. No credentials required.

## OpenRouterProvider

Streams completions from [OpenRouter](https://openrouter.ai) over Server-Sent
Events.

- Required env var: `OPENROUTER_API_KEY`.
- Optional env var: `OPENROUTER_MODEL`. The provider itself sets no
  default; the CLI supplies the model and falls back to `openrouter/free`
  when none is configured.
- Model identifiers follow OpenRouter's convention, e.g. `openai/gpt-4o`.

## Provider selection today

`yoli chat` and `yoli run --role <role>` currently target
`OpenRouterProvider` and read `OPENROUTER_API_KEY` / `OPENROUTER_MODEL`
from the environment. `FauxProvider` is exported from
`internal/ai/providers` and is available to programmatic callers and
tests, but the CLI does not yet expose a `--provider` flag — the
`default_provider` config key is reserved for that work.

## Storing credentials

Provider credentials can be stored via `yoli config set` instead of being
exported in every shell:

```bash
yoli config set openrouter_api_key sk-or-v1-…
```

The CLI calls `ApplyEnvDefaults(LoadConfig(...))` before invoking a
provider, so a stored `openrouter_api_key` is exported as
`OPENROUTER_API_KEY` for the duration of the process — unless the env var
is already set, in which case the shell wins.

See also [configuration.md](configuration.md).
