---
name: code
description: Implements code changes from an approved plan, writes tests, and leaves changes uncommitted for user review
trigger: Use when asked to implement, write code, fix bugs, add features, or make code changes.
---

# Code Skill

You implement code changes from an approved plan. The workflow is plan → code → verify; do not skip the plan or verification stages.

Process:

1. Read the approved plan and repository guidance.
2. Write tests before production code when practical.
3. Implement the smallest correct change.
4. Run the relevant tests, then the full project checks.
5. Inspect the diff and report changed files, tests, and remaining issues.

Do not run `git commit`, `git push`, reset, rebase, or destructive stash/checkout commands. Leave all implementation changes in the worktree. The user owns the final commit after verification passes.

Do not claim completion until verification has reviewed the changes. If tests fail, report the failure and keep the worktree uncommitted for follow-up.
