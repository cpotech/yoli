# Session format

Yoli persists every chat and headless-agent conversation as a single
JSONL file. The first line is a header; every other line is either a
message entry or a leaf marker.

## File layout

Sessions live under `~/.yoli/agent/sessions/<cwd-bucket>/<id>.jsonl`,
where `<cwd-bucket>` is a 12-character SHA-256 prefix of the working
directory that produced the session. Each session is named by its UUID.

## Header

The first line is a JSON object identifying the session:

```json
{"type":"session","version":3,"id":"<uuid>","timestamp":"<rfc3339>","cwd":"<path>","parentSession":"<uuid|empty>"}
```

`parentSession` is present only for sessions created via `--fork`.

## Entries

Every subsequent line is one of:

```json
{"type":"message","id":"<8-hex>","parentId":"<8-hex|empty>","timestamp":"<rfc3339>","message":{...ai.Message}}
{"type":"leaf","id":"<8-hex>","timestamp":"<rfc3339>"}
```

- `message` entries form a tree via `parentId`. The root is the entry
  whose `parentId` is empty.
- `leaf` entries record that the active leaf was moved to a prior entry
  (a Branch). The next message appended will use the new leaf as its
  parent.

## Reconstruction

Reopening a session walks the file once:

1. Parse the header.
2. For each entry in order, append it to the entry list and advance the
   leaf to its ID. A `leaf` entry advances the leaf without appending.

`BuildMessages` then walks from the active leaf back to the root via
`parentId` and reverses the chain, producing the AI-ready conversation.

## Flags that produce sessions

| Flag | Effect |
|---|---|
| _(default)_ | Auto-create a new session for the current cwd. |
| `-c` | Continue the most-recently-modified session for the cwd. |
| `-r` | List sessions and prompt on stdin (TTY) to resume one. |
| `--session <path\|id>` | Resume a specific session by path, full id, or unique prefix. |
| `--fork <path\|id>` | Copy a source session's active branch into a new session whose `parentSession` is the source. |
| `--no-session` | Run without writing a session file. |

The headless `yoli agent` command honours the same flags and also reads
`AGENT_SESSION`, `AGENT_FORK`, and `AGENT_CONTINUE` from the environment.

## Inspection

`yoli session` provides non-interactive inspection helpers:

```
yoli session list [--all] [--cwd <dir>]
yoli session current --session <path|id>
yoli session tree    --session <path|id>
yoli session branch  --session <path|id> --entry <id>
```
