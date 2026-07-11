# Providers

Yoli ships two providers, all implementing the `Provider` interface from
`internal/ai`.

## FauxProvider

A deterministic stub used by tests and demos. Replies with canned content
and emits no network traffic. No credentials required.

## OpenAICompatProvider

Streams completions from any OpenAI-compatible `/chat/completions`
endpoint over Server-Sent Events. Self-hosted backends such as vLLM work
by pointing `base_url` at them — see [self-hosting.md](self-hosting.md).

- Required env var: `YOLI_API_KEY`, sent as `Authorization: Bearer <key>`.
- Required env var: `YOLI_BASE_URL`, e.g. `https://openrouter.ai/api/v1`
  or your own endpoint.
- Required env var: `YOLI_MODEL`. The model identifier is sent to the
  backend verbatim, e.g. `openai/gpt-4o` for OpenRouter or the exact
  served model name for a self-hosted server.

## Provider selection

`yoli chat`, `yoli tui`, `yoli run --role <role>`, and `yoli agent` all
target `OpenAICompatProvider`. Which endpoint it talks to is decided by
named provider profiles defined under the `providers` key of the config
file (see [configuration.md](configuration.md#provider-profiles)), with
four selection surfaces:

1. `--provider <name>` flag on `chat`, `tui`, `run`, and `agent`.
2. `YOLI_PROVIDER` env var.
3. `YOLI_PROVIDER` / `default_provider` config key.
4. `/provider [name]` inside the TUI — lists profiles or switches the
   endpoint, model, and context limits mid-session.

Precedence is flag > env > config. Explicit selections (flag or env) of
an unknown profile name are an error; a stale `default_provider` only
warns. With no selection at all, the flat `YOLI_API_KEY` /
`YOLI_BASE_URL` / `YOLI_MODEL` values are used directly, as before.
`FauxProvider` is exported from `internal/ai/providers` for programmatic
callers and tests only.

## Storing credentials

Provider credentials can be stored via `yoli config set` instead of being
exported in every shell:

```bash
yoli config set api_key sk-or-v1-…
```

The CLI calls `ApplyEnvDefaults(LoadConfig(...))` before invoking a
provider, so a stored `api_key` is exported as `YOLI_API_KEY` for the
duration of the process — unless the env var is already set, in which
case the shell wins.

See also [configuration.md](configuration.md).
