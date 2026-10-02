# `yoli acp` — Agent Client Protocol server

`yoli acp` runs yoli as an [Agent Client Protocol](https://agentclientprotocol.com)
(ACP) agent: a JSON-RPC 2.0 server on stdin/stdout that an ACP-capable
editor spawns and talks to. In Neovim that means CodeCompanion.nvim,
avante.nvim and agentic.nvim; Zed and JetBrains work the same way. The
editor provides the chat UI: streamed turns, visible tool calls,
follow-along file locations and cancel. yoli needs no editor plugin.

```
yoli acp [--provider <name>] [--no-session]
```

| Flag | Description |
|---|---|
| `--provider <name>` | Provider profile to use. Defaults to `default_provider` (see [configuration.md](configuration.md)). |
| `--no-session` | Keep sessions in memory only. `session/load` is then unavailable and `loadSession` is advertised as `false`. |

The editor starts `yoli acp` itself, so put `--provider` in the editor's
command args to choose a profile. stdout carries only ACP frames; the
`yoli: acp: provider=… model=…` banner, warnings and logs go to stderr.

## What is supported

| ACP method | Support |
|---|---|
| `initialize` | Yes. Always answers `protocolVersion: 1`. |
| `authenticate` | Answers `{}`. `authMethods` is empty, so clients don't call it. |
| `session/new` | Yes. `cwd` must be absolute. |
| `session/load` | Yes, unless `--no-session`. Replays the conversation, then responds. |
| `session/prompt` | Yes. One prompt at a time per session; a second one gets error `-32000`. |
| `session/cancel` | Yes. The pending prompt responds `stopReason: "cancelled"`. |
| `session/update` | Sent for user replay, assistant text, reasoning, tool calls and tool results. |
| `session/set_mode`, `session/request_permission`, `fs/*`, `terminal/*` | Not yet. |

Details:

- **Sessions.** An ACP `sessionId` is the yoli session ID. Sessions are
  saved under `~/.yoli/agent/sessions/` like `chat` and `tui`, so
  `yoli session list` and `yoli tui --session <id>` see them too.
- **Per-session cwd.** Tools (Read, Write, Edit, Bash, …) operate on the
  session's `cwd`, not the directory the editor started yoli in. Project
  skills are loaded from `<cwd>/.yoli/skills/`.
- **Prompt content.** `text`, `resource_link` and embedded `resource`
  blocks are accepted (`embeddedContext: true`). Embedded text is passed
  to the model inline; links and binary resources become a
  `Referenced file: <path>` line. `image` and `audio` blocks are rejected
  with `-32602`.
- **Tool calls.** Each call is reported as a `tool_call` with a short
  title built from its main argument (`Skill plan`, `Read src/a.go`, or
  a Bash command in backticks), an ACP
  `kind` (`read`, `edit`, `search`, `execute`, `fetch` or `other`) and,
  for Read/Write/Edit, the absolute file path in `locations`. Its result
  follows as a `tool_call_update` (`completed` or `failed`), with the
  output echoed to the editor truncated to 4000 bytes. The model still
  sees the full output.
- **Whole turns.** Responses are not token-streamed. Each assistant turn
  arrives as one `agent_message_chunk`, and the UI updates turn by turn
  and tool call by tool call.
- **Stop reasons.** `end_turn`, `cancelled`, or `max_turn_requests` when
  the loop's iteration cap is reached. Rate-limited provider requests
  are retried with backoff for about 30s first. Other provider errors,
  and a rate limit that outlasts the retries, return a JSON-RPC error
  (`-32603`) carrying the message.
- **No permission prompts.** Tools run without asking, as in `chat` and
  `tui`. The Bash policy still blocks the same footguns (see
  [architecture.md](architecture.md)).
- **MCP servers.** yoli has no MCP client. Any `mcpServers` the editor
  sends are logged to stderr and ignored.

Edits go straight to disk, not through the editor's buffers. Editors
should reload changed buffers. CodeCompanion and avante already do this
after a turn; for plain Neovim buffers, set `autoread` and run
`:checktime` on `FocusGained`/`BufEnter`.

## Editor setup

### CodeCompanion.nvim

```lua
require("codecompanion").setup({
  adapters = {
    acp = {
      yoli = function()
        local helpers = require("codecompanion.adapters.acp.helpers")
        return {
          name = "yoli",
          formatted_name = "yoli",
          type = "acp",
          roles = { llm = "assistant", user = "user" },
          commands = {
            default = { "yoli", "acp" },
            -- local_vllm = { "yoli", "acp", "--provider", "runpod" },
          },
          defaults = { mcpServers = {}, timeout = 20000 },
          parameters = {
            protocolVersion = 1,
            clientCapabilities = { fs = { readTextFile = true, writeTextFile = true } },
            clientInfo = { name = "CodeCompanion.nvim", version = "1.0.0" },
          },
          handlers = {
            setup = function() return true end,
            auth = function() return true end,
            form_messages = function(self, messages, capabilities)
              return helpers.form_messages(self, messages, capabilities)
            end,
            on_exit = function() end,
          },
        }
      end,
    },
  },
  interactions = { chat = { adapter = "yoli" } },
})
```

### avante.nvim

```lua
acp_providers = { yoli = { command = "yoli", args = { "acp" } } }
```

### Zed

In `settings.json`:

```json
"agent_servers": {
  "yoli": { "type": "custom", "command": "yoli", "args": ["acp"] }
}
```

## Debugging

Drive the server by hand to see the raw frames:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}' \
  '{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"'"$PWD"'","mcpServers":[]}}' \
  | yoli acp
```

Feed `session/prompt` requests the same way, using the `sessionId` from
the `session/new` response. Closing stdin cancels any running prompt and
exits.
