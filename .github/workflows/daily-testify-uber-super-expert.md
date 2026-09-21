---
on:
  schedule: daily
  workflow_dispatch: null
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
imports:
- uses: shared/skip-if-issue-open.md
  with:
    title-prefix: "[testify-expert]"
- uses: shared/daily-issue-base.md
  with:
    expires: 2d
    labels:
    - testing
    - code-quality
    - automated-analysis
    - cookie
    title-prefix: "[testify-expert] "
- shared/go-source-analysis.md
- shared/safe-output-app.md
- shared/otlp.md
description: Daily expert that analyzes one test file and creates an issue with testify-based improvements
emoji: 🧪
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
name: Daily Testify Uber Super Expert
strict: true
timeout-minutes: 20
tools:
  bash:
  - "*"
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets:
    - default
  repo-memory:
    branch-name: memory/testify-expert
    description: Tracks processed test files to avoid duplicates
    file-glob:
    - "*.json"
    - "*.txt"
    max-file-size: 51200
tracker-id: daily-testify-uber-super-expert
features:
  gh-aw-detection: true
sandbox:
  agent:
    id: awf
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Testify Uber Super Expert 🧪✨

Analyze one Go `*_test.go` file per run and open a focused improvement issue using testify best practices.

## Current Context

- **Repository**: ${{ github.repository }}
- **Analysis Date**: $(date +%Y-%m-%d)
- **Workspace**: ${{ github.workspace }}
- **Cache Location**: `/tmp/gh-aw/repo-memory/default/memory/testify-expert/`

## Required Execution Flow

### 1) Load cache and choose one target file

- Read `/tmp/gh-aw/repo-memory/default/memory/testify-expert/processed_files.txt` when present (`path|YYYY-MM-DD`).
- Build candidate list from `find . -name '*_test.go' -type f`.
- Prefer files never processed or processed more than 30 days ago.
- If all files were processed recently, stop and return the “all analyzed” completion message.

### 2) Analyze with Serena + code inspection

For the selected test file:

- Read file structure and identify its paired source file (`*_test.go` → `.go` when it exists).
- Assess testify usage (`assert.*` vs `require.*`), table-driven patterns, naming clarity, isolation, and edge-case coverage.
- Compare exported source functions vs test functions to identify likely coverage gaps.
- Produce concrete, code-level recommendations only (no generic advice).

### 3) Create one actionable issue

Create an issue titled `Improve Test Quality: <FILE_PATH>` containing:

- Current state summary (file path, source pair if present, test count, LOC).
- Strengths (brief).
- Prioritized improvements in this order:
  1. missing/high-value tests,
  2. testify assertion upgrades,
  3. table-driven refactors,
  4. organization/readability.
- Short before/after examples where they materially improve clarity.
- Acceptance checklist with test validation (`make test-unit`).

Use `###`/`####` headers only and wrap long sections in `<details>` blocks.

### 4) Update cache

After successful issue creation:

- Append `<TARGET_FILE>|<TODAY>` to `processed_files.txt`.
- Deduplicate by file path, keeping the newest date.

## Output Requirements

### If no eligible file exists

Return a success message saying all test files were analyzed in the last 30 days and include cache location.

### If analysis was completed

Return a compact summary with selected file, counts, created issue number/title, and cache update confirmation.

## Guardrails

- One file per run.
- Prefer precise recommendations over long prose.
- Follow patterns in `scratchpad/testing.md` and nearby `pkg/**/_test.go` files.
- Always update cache after successful analysis.