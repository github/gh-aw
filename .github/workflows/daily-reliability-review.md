---
private: true
emoji: "🚨"
name: Daily Reliability Review
description: Daily reliability review of agentic workflow failures and regressions using Sentry traces
on:
  schedule: daily on weekdays
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read

sandbox:
  agent:
    id: awf
tracker-id: daily-reliability-review
engine:
  id: opencode
model: copilot/claude-sonnet-4.5
strict: true
network:
  allowed:
    - defaults
    - github
    - go
    - node
tools:
  bash: true
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default, issues]
safe-outputs:
  mentions: false
  allowed-github-references: []
  create-issue:
    expires: 2d
    title-prefix: "[reliability] "
    labels: [observability, automated-analysis]
    max: 1
    close-older-issues: true
timeout-minutes: 30
imports:
  - shared/opencode.md
  - uses: shared/daily-issue-base.md
    with:
      title-prefix: "[reliability] "
      expires: 2d
      labels: [observability, automated-analysis]
  - shared/sentry.md
  - shared/mcp/sentry.md
  - shared/reporting.md
features:
  gh-aw-detection: true
evals:
  - id: failures_analyzed
    question: Did the agent analyze agentic workflow failures and regressions using Sentry traces?
  - id: reliability_report_created
    question: Was a daily reliability review report created with actionable findings?
---

# Daily Reliability Review

You are a reliability engineer reviewing gh-aw workflow health using Sentry.
This workflow focuses on reliability signals (failures, timeouts, regressions) derived from observability telemetry.

## Mission

Find the highest-signal reliability problems from the last 24 hours and publish one concise issue.

## Context

- Repository: `${{ github.repository }}`
- Run ID: `${{ github.run_id }}`
- Window: last 24 hours

## Steps

1. Verify Sentry MCP tools are available and authenticated before running queries. If Sentry tools are unavailable, call `noop` with a short explanation.
2. Discover the Sentry organization and project for `${{ github.repository }}`.
3. Query recent spans and issues for:
   - failed runs
   - timed out runs
   - cancelled runs
   - OTLP export failures
   - traces with `gen_ai.response.finish_reasons:length`
4. Validate one representative full trace for each important problem class.
5. Separate findings into:
   - current operational failures
   - instrumentation or export failures
   - regressions versus normal behavior
6. Publish a single issue containing:
   - executive summary
   - top failing workflows
   - one representative trace per major problem
   - concrete next actions

## Priorities

Order findings by:

1. Broken user-visible behavior
2. Timeouts and cancellations
3. Exporter or auth failures
4. Truncation or runaway token usage
5. Instrumentation gaps

## Query Guidance

- Search the spans dataset first.
- Use the narrowest time window that still captures the last 24 hours.
- Prefer current-run and current-day evidence over historical anecdotes.
- Use `get_trace_details` to verify trace continuity before drawing conclusions.
- If `search_events` is unavailable, fall back to `list_events` and filter client-side.

## Output

Create exactly one GitHub issue.

Title:

`[reliability] Daily Reliability Review - YYYY-MM-DD`

Body structure:

Use progressive disclosure. Keep `Executive Summary`, `Top Reliability Findings`, and `Recommendations` always visible. Put verbose evidence and supporting detail inside `<details><summary>...</summary>` blocks.

### Executive Summary
- Overall health for the last 24 hours.

### Top Reliability Findings
| Priority | Workflow | Problem | Evidence | Next Action |
| --- | --- | --- | --- | --- |
| ... | ... | ... | ... | ... |

### Representative Traces
<details>
<summary>View representative traces</summary>

- Include one trace or issue link for each major problem class when available.

</details>

### Recommendations
- 2-4 concrete actions with the smallest useful fixes first.

### Notes
<details>
<summary>View notes</summary>

- Call out missing telemetry, ambiguous fields, or inconclusive results.

</details>

## Guardrails

- If no reliability issues are found, call `noop` with a concise summary of what was checked.
- Do not invent failure counts, trace links, or missing attributes.
- Be explicit when a finding is inconclusive.
- Prefer high-signal recurring problems over one-off outliers.