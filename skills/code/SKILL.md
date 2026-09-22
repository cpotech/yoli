---
name: code
description: Autonomously implements code changes, writes tests, and commits locally
trigger: Use when asked to implement, write code, fix bugs, add features, or make code changes.
---

# Code Skill

You autonomously implement code changes: analyze requirements, write code and tests, run tests, and commit changes.

## Process

Follow these steps in order:

1. **Analyze** — Read the task description carefully. Use Glob, Grep, and Read to understand the project structure, relevant files, and existing patterns. Read the project's own docs for build & test commands: `CONTRIBUTING.md`, `README*`, and build manifests (`go.mod`, `package.json` scripts, `Makefile`, etc.). Identify the files you need to create or modify.

2. **Write Tests First (TDD)** — Write tests BEFORE writing production code. Implement test cases as real, runnable tests (not stubs or placeholders). Use the project's existing test framework, assertions, and mocking patterns. Import the modules under test (they may not exist yet — that's expected in TDD). NEVER copy or re-implement production code in test files — always import the real module.

3. **Implement Code Changes** — Write clean, minimal code to make the failing tests pass. Follow existing patterns and conventions in the codebase. Make atomic, focused changes. Remove dead code and unnecessary complexity encountered in the touched scope when safe and relevant.

4. **Add Additional Tests** — Review your implementation for edge cases or scenarios not covered. Add any additional tests needed for comprehensive coverage.

5. **Run Tests Locally** — Run tests to verify all tests pass. If tests fail, fix the code and re-run until green. Do NOT skip this step.

6. **Commit Changes** — Stage and commit changes with conventional commit messages (e.g., `feat:`, `fix:`, `test:`, `refactor:`). Do NOT push to a remote.

7. **Signal Completion** — Provide a summary of what was done and signal completion.

## Guidelines

1. **Be autonomous** — Make decisions yourself. Only ask questions if truly blocked.
2. **Tests first (TDD)** — Always write tests before implementing production code.
3. **Conventional commits** — Use commit messages like `feat:`, `fix:`, `test:`, `refactor:`.
4. **Never skip tests** — Always run tests before finishing.
5. **Keep changes minimal** — Only change what's needed to satisfy the task, including cleanup in the touched scope.
6. **No replicated production code in tests** — Tests must import and exercise real production modules. Never copy, re-implement, or inline production logic in test files.
7. **Simplify responsibly** — Prefer behavior-preserving simplifications and dead-code removal over adding complexity.
