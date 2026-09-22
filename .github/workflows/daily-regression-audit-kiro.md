---
private: true
emoji: "🧭"
description: Daily analysis of recent CI test failures and regression patterns
features:
  gh-aw-detection: true
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
sandbox:
  agent:
    id: awf
tracker-id: daily-regression-audit-kiro
engine:
  id: copilot
  copilot-sdk: true
strict: true
network:
  allowed:
    - defaults
    - github
tools:
  github:
    mode: local
    toolsets: [repos, issues, actions]
  bash:
    - cat
    - grep
    - wc
    - head
    - tail
    - sort
    - uniq
    - date
    - echo
    - printf
    - jq
    - python3
    - ls
    - pwd
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[regression] "
    labels: [automation, testing, reliability]
    max: 1
    close-older-issues: true
    close-older-key: daily-regression-audit-kiro
  missing-tool:
timeout-minutes: 25
imports:
  - shared/otlp.md
  - shared/reporting.md
---

# Daily Regression Audit

Analyze recent CI workflow runs to surface patterns in test failures and regressions.

## Step 1 — Fetch recent workflow runs

Use the GitHub MCP `list_workflow_runs` tool on `${{ github.repository }}` with:
- `workflow_id: ci.yml` (or the main CI workflow)
- `per_page: 20`
- `status: completed`

Record the last 20 run conclusions (success / failure / cancelled).

## Step 2 — Identify failure rate

Calculate:
- Total runs in the set
- Failure count and percentage
- Consecutive failure streak (if any)

If failure rate is below 10% and no streak exists, skip to Step 4 with a "healthy" conclusion.

## Step 3 — Inspect failed runs

For the most recent 3 failed runs, use `list_workflow_jobs` to get the jobs and then
`get_job_logs` to retrieve failing job output (tail 200 lines). Look for:
- Repeated error messages across runs (regression patterns)
- New failures that appear only in recent runs (potential regressions introduced recently)
- Flaky tests vs. consistent failures

Summarize the top 2–3 failure patterns found.

When parsing JSON from MCP tool output or local files, use `jq` (not inline `python3 -c`/heredoc parsing).
If a tool/command is denied for this step, do not retry near-identical variants: after 2 denied attempts for the
same intent, stop and call `missing-tool` with the denied command and required capability.

## Step 4 — Check for open regression issues

Use `list_issues` on `${{ github.repository }}` with `labels: ["bug","regression"]` and
`state: open`, `perPage: 5`. Record issue numbers and titles.

## Step 5 — Report

Use the `reporting` skill to format the report body. Use `###` (or lower) headers only — never
`#` or `##`. Keep the failure rate and recommended action visible at the top; wrap the full
failure pattern details and raw job log excerpts in a
`<details><summary><b>Failure Details</b></summary>` block.

Use the `create_issue` safe-output tool to post the daily audit:

- **Title**: `[regression] Daily CI Regression Audit — ${{ github.run_id }}`
- **Body**:
  - `### Summary` with CI failure rate (last 20 runs) and recommended action (investigate /
    monitor / no action)
  - `<details><summary><b>Failure Details</b></summary>` wrapping top failure patterns (or "No
    regressions detected") and open regression/bug issues

If CI is fully healthy (failure rate < 10%, no streak, no new patterns), call `noop` with
`"CI health is good — no regressions detected."`.