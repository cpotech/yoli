# Skills

A **skill** is a single Markdown file with YAML frontmatter that the agent
loads on demand. Each skill describes a focused capability — "review a
pull request", "run a security audit", "set up a Yolium project" — along
with the prompt body the agent should adopt when the skill is invoked.

## File layout

Skills live in three locations, searched in order of precedence:

1. **Project** — `./.yoli/skills/<name>/SKILL.md`
2. **User** — `~/.yoli/skills/<name>/SKILL.md`
3. **Built-in** — bundled next to the `yoli` CLI entry point (resolved
   relative to the binary's own directory).

A project skill overrides a user skill of the same name, which overrides a
built-in.

The built-in directory resolves relative to the *installed* binary:
`dist/yoli-linux-amd64` sees the repo's `skills/`, a container binary at
`/usr/local/bin/yoli` sees `/usr/local/skills` (the Dockerfile copies the
repo's `skills/` there), and a dev binary at the repo root looks *outside*
the repo — use the dist binary when testing built-ins.

## Built-in skills

- **plan** (`skills/plan/SKILL.md`) — analyze the codebase and produce a
  structured implementation plan (ordered steps, files to modify,
  acceptance criteria, test specifications) without writing code.

## SKILL.md format

```markdown
---
name: review-pr
description: Review a GitHub pull request against the repo's review checklist
trigger: Use when the user asks for a PR review or pastes a PR URL.
---

You are reviewing a pull request. Follow the checklist below…
```

- `name` — unique slug, must match the directory name.
- `description` — one-line summary shown by `yoli skills list`.
- `trigger` — when the agent should invoke this skill on its own; shown
  alongside the description in the injected system-prompt section.

The body below the frontmatter is the prompt content the agent adopts
when it activates the skill via the `Skill` tool.

## CLI

```bash
# List every available skill (project + user + built-in)
yoli skills list

# Print the full contents of a skill
yoli skills show <name>
```

## Where skill resolution happens

`yoli agent`, `yoli chat`, and `yoli tui` load skills once at startup via
`internal/agent/skills`' `Load` and append an "Available Skills" section
(names, descriptions, and triggers only) to the system prompt. Skill
bodies are not inlined; the agent fetches one on demand by calling the
`Skill` tool with the skill's name, which returns the frontmatter-stripped
Markdown body. When no skills are found, neither the section nor the tool
is registered. A failing skills directory degrades to "no skills" with a
stderr warning rather than breaking the run.

Note that project skills (`.yoli/skills/`) feed the system prompt of any
agent run in that repository — the same trust model as checked-in agent
context files like `AGENTS.md`.
