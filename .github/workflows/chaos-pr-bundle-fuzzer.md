---
private: true
name: "Chaos PR Bundle Fuzzer"
description: Stress-tests safe-output create-pull-request git patch/bundle handling with randomized small-change personas
on:
  schedule: "every 7 days"
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  pull-requests: read
  issues: read
model: openai/gpt-5.3-codex
engine:
  id: codex
  model-provider: openai
strict: true
sandbox:
  agent:
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  github:
    mode: local
  cache-memory: true
  bash: true
safe-outputs:
  create-pull-request:
    title-prefix: "[chaos-test] "
    preserve-branch-name: true
    recreate-ref: true
    labels: [test-in-progress]
    draft: true
    max: 5
    expires: 4h
    if-no-changes: "ignore"
    close-older-pull-requests: true
    allowed-files:
      - "tmp/chaos/**"
      - "scratchpad/chaos/**"
      - "tests/chaos/**"
    excluded-files:
      - ".github/workflows/**"
    protected-files: blocked
  noop:
timeout-minutes: 30
imports:
  - shared/otlp.md
  - shared/reporting.md
  - shared/graders.md
features:
  gh-aw-detection: true

---

# Chaos PR Bundle Fuzzer

You are a chaos-testing agent focused on safe-output `create_pull_request` robustness for git patch/bundle packaging.

## Goal

Generate randomized "agent personas" that each perform a small change scenario, run git operations, and create test PRs.

## Hard Requirements

1. Create exactly **5 PRs per run**.
2. Every PR branch name must start with `chaos/`.
3. Every PR body must include this exact sentence (plain text, no markdown formatting):
   This pull request is an automated chaos test for safe-output create-pull-request bundling.
4. Never modify `.github/workflows/**`.
5. Never modify protected/sensitive files.
6. Keep changes intentionally small (1-3 tiny edits per PR). Large changes are out of scope.

## Randomized Persona Loop

Use cache-memory to keep a rolling strategy ledger across runs at `/tmp/gh-aw/cache-memory/chaos-pr-bundle-fuzzer.json`.

For each run:

1. Load previous ledger if present.
2. Build a randomized plan:
   - Always generate 5 PR scenarios.
   - Random personas (examples: cautious maintainer, rushed intern, refactor zealot, docs tidy bot, flaky fixer).
   - Random strategy mix (single commit, two commits, amend, staged subset, minor rename, line-ending variant, multi-parent merge commit, octopus merge, diverged history reconciliation).
3. Prefer strategies that were under-tested in previous runs while balancing simple and complex strategy categories across runs.

## Per-PR Scenario Steps

For each selected persona:

1. Create a scenario-specific branch that starts with `chaos/`.
2. Apply only small file edits under `tmp/chaos/**`, `scratchpad/chaos/**`, or `tests/chaos/**`.
3. Execute git operations to exercise bundling behavior (for example: branch create, add, commit, optional amend or second commit).
4. Verify changed files are still within allowed scope.
5. Create the pull request via safe-output `create_pull_request`.
6. In title/body, clearly mark persona name, scenario type, and that this is a test.

## Output Discipline

- If at least one PR is created, finish after recording summary stats in cache-memory.
- If no safe PR can be produced, call `noop` with a concise reason.
- Keep logs concise and action-oriented.
