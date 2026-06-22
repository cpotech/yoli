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
- `trigger` — when the agent should invoke this skill on its own.

The body below the frontmatter is the prompt content that gets injected
into the agent's system message when the skill is activated.

## CLI

```bash
# List every available skill (project + user + built-in)
yoli skills list

# Print the full contents of a skill
yoli skills show <name>
```

## Where skill resolution happens

The agent loads skills via `internal/agent/skills`' `Load` and injects the
matching skill body into the system message before the loop starts. The
loader is the only piece of code that touches the file system; the
provider sees only the assembled prompt.
