---
name: plan
description: Analyze the codebase and produce a structured implementation plan with ordered steps, files to modify, acceptance criteria, and test specifications — without writing code
trigger: Use when asked to plan, design, or scope an implementation before coding, or when a separate coding agent will execute the work later.
---

# Plan Skill

You produce a plan, not code. Do not modify source files; your deliverable
is the plan itself. A coding agent (or the user) will execute it later, so
it must be precise enough to follow without further clarification.

## Process

Work through these steps in order:

1. **Analyze** — Read the project's own docs FIRST: `AGENTS.md`,
   `CLAUDE.md`, `README*`, plus build manifests (`Makefile`, `justfile`,
   `package.json` scripts, `go.mod`, `pyproject.toml`). These define the
   toolchain, build/test commands, and testing conventions your plan must
   conform to. Then explore the codebase: find the closest existing
   analogue to the requested change and read it end-to-end so the plan
   follows established patterns. Note behavior-preserving simplification
   and dead-code opportunities in files the work will touch.
2. **Clarify (if needed)** — Ask one question at a time, and only when the
   answer materially affects the plan. If this run has no way to ask,
   state your assumption explicitly in the plan and proceed.
3. **Write the plan** — Follow the Plan Structure below.
4. **Write test specifications** — Follow the Test Specifications rules
   below.
5. **Deliver** — Follow the Delivery rules below.

## Plan Structure

The plan must contain these sections:

- **Context** — The goal in one or two sentences, plus what analysis
  revealed about the relevant code.
- **Approach** — The chosen approach and why it beats the alternatives.
- **Steps** — Ordered steps, each naming the files to modify and the
  specific changes (including in-scope cleanup and dead-code removal).
  Earlier steps must not depend on later ones.
- **Files to Modify** — A table of files and what changes in each.
- **Acceptance Criteria** — Checkboxes, including test requirements and
  any simplification expectations.
- **Test Specifications** — Human-readable form of the specs from step 4.

## Test Specifications

For each test file give the path, what it covers, and the individual test
cases as concrete, implementable descriptions:

- Match the project's existing test framework, file naming, assertion
  style, and mocking conventions exactly — study real test files first.
- Every spec must exercise real production modules imported from their
  real paths. Never specify tests that re-implement production logic.
- Only specify what the project's runner can actually verify. Prefer
  observable behavior over environment-dependent details the test runner
  cannot compute.
- Cover the happy path, edge cases and error conditions, and any cleanup
  or simplification changes in the plan.

## Delivery

Report progress and deliver the finished plan through whatever
progress/completion mechanism your run instructions define (for example,
updating a work item's description and posting the plan as a comment
before signalling completion). If none is defined, the full plan is your
final output.

## Guidelines

1. Cite exact files (and line ranges where useful) for each step.
2. Respect existing conventions; do not introduce new patterns when an
   established one fits.
3. Prefer simpler designs — call out chances to reduce complexity.
4. Constrain cleanup to behavior-preserving changes relevant to the task.
5. One plan per task: do not split the work into new tracking items.
