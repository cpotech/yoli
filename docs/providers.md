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

All settings come from the active provider profile in the config file
(see [configuration.md](configuration.md#provider-profiles)); the
environment is never read:

- Required field: `api_key`, sent as `Authorization: Bearer <key>`.
- Required field: `base_url`, e.g. `https://openrouter.ai/api/v1`
  or your own endpoint.
- `model`. The model identifier is sent to the backend verbatim, e.g.
  `openai/gpt-4o` for OpenRouter or the exact served model name for a
  self-hosted server.

## Provider selection

`yoli chat`, `yoli tui`, `yoli run --role <role>`, and `yoli agent` all
target `OpenAICompatProvider`. Which endpoint it talks to is decided by
named provider profiles defined under the `providers` key of the config
file, with three selection surfaces:

1. `--provider <name>` flag on `chat`, `tui`, `run`, and `agent`.
2. `default_provider` config key.
3. `/provider [name]` inside the TUI — lists profiles or switches the
   endpoint, model, and context limits mid-session.

The flag wins over the config key. Failing to resolve a profile — no
profiles defined, nothing selected, or an unknown name — is an error.
`FauxProvider` is exported from `internal/ai/providers` for programmatic
callers and tests only.

## Storing credentials

Credentials live in the `api_key` field of a provider profile, edited by
hand in the JSON config file. `yoli config providers` lists profiles
without printing keys. Shell environment variables are ignored.

See also [configuration.md](configuration.md).
