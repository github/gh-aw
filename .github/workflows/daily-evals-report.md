---
private: true
emoji: "🧪"
description: Daily data science report analyzing evals feature adoption, per-question pass rates, and quality trends across agentic workflows
on:
  schedule: daily around 8:00
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
max-daily-ai-credits: 3000
model: copilot/gpt-5.3-codex
engine:
  id: codex
tracker-id: daily-evals-report
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
    memory: 4g
features:
  gh-aw-detection: true
timeout-minutes: 45
tools:
  github:
    mode: local

imports:
  - uses: shared/meta-analysis-base.md
    with:
      toolsets: [default, actions]
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[evals] "
      expires: 7d
safe-outputs:
  mentions: false
  allowed-github-references: []
  max-bot-mentions: 1
  create-issue:
    title-prefix: "[evals] "
    labels: [evals, automated-analysis]
    close-older-issues: true
    expires: 7d
    max: 1
evals:
  - id: evals_data_analyzed
    question: Did the agent analyze evals feature adoption and per-question pass rates across agentic workflows?
  - id: report_created
    question: Was a data science report produced with quality trends and adoption metrics?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Evals Feature Report

You are a data scientist analyzing the `evals` feature in GitHub Agentic Workflows.
The `evals` feature lets workflow authors declare BinEval-style binary YES/NO questions
that are automatically evaluated after each run. Results are stored as `evals.jsonl`
artifacts per run and persisted to dedicated `evals/<workflow-id>` git branches.

## Mission

Analyze the last 7 days of workflow runs that use the `evals` feature. Compute per-question
YES/NO pass rates, identify workflows with failing evals, and produce a data science summary.
When the evals feature is degraded or broken, produce an actionable issue to guide investigation.

## Context

- Repository: `${{ github.repository }}`
- Run ID: `${{ github.run_id }}`
- Window: last 7 full days ending at workflow start (UTC)

## Phase 1: Fetch Evals Runs

Call the `logs` MCP tool once to download runs with evals artifacts from the past 7 days.

**Tool**: `logs`
**Parameters**:
```json
{
  "workflow_name": "",
  "count": 100,
  "start_date": "-7d",
  "artifacts": ["evals", "usage"]
}
```

Logs are saved to `/tmp/gh-aw/aw-mcp/logs/run-<id>/`.

Do **not** enumerate every workflow first. One broad fetch is the right starting point.

## Phase 2: Discover Evals Data

For each downloaded run directory at `/tmp/gh-aw/aw-mcp/logs/run-<id>/`:

1. Check for `evals/evals.jsonl` (or `evals.jsonl` at root if flattened).
2. Read `aw_info.json` (or `activation/aw_info.json`) to get:
   - `workflow_name` (or `workflow.name`)
   - `conclusion` (success / failure / cancelled)
   - Whether evals were declared (check for an `evals` key or the presence of the evals job in the run)
3. Detect **evals job failures** by scanning job conclusions:
   - runs where the workflow declares evals but `evals.jsonl` is absent
   - runs where job `push_evals_state` exists and has a failed conclusion (`failure`, `timed_out`, or `cancelled`)
   - if both conditions happen in the same run, count it once

Cap analysis at **40 runs total**. Apply prioritization before capping: first include all runs that produced `evals.jsonl` (up to 40), then fill any remaining capacity with evals-failure runs. If there are more than 40 runs with evals results, use the most recent 40 and note in the report that analysis was capped. Evals failures are still recorded even if the 40-run cap is reached from results-only runs — count them but do not analyze their artifacts.

## Phase 3: Parse Evals JSONL

For each `evals.jsonl` found, read every JSONL line. Each record contains:
- `id` — question ID
- `question` — question text
- `answer` — `"YES"` or `"NO"`
- Optionally: `confidence`, `rationale`, `model`

Extract per-run:
- Workflow name and run ID
- Per-question answer list
- Overall pass/fail: **pass** if every question is YES, **fail** if any question is NO

## Phase 4: Aggregate Statistics

### Per-Workflow

For each unique workflow:
- `runs_with_evals_results` — runs that produced `evals.jsonl`
- `runs_evals_job_failed` — runs that either declared evals but produced no `evals.jsonl`, or had a failed `push_evals_state` job
- `evals_job_success_rate` = `runs_with_evals_results / (runs_with_evals_results + runs_evals_job_failed) * 100`
- `run_pass_rate` = runs where all questions are YES / `runs_with_evals_results * 100`
- Per-question: `yes_rate` = YES count / `runs_with_evals_results * 100`
- `lowest_scoring_question` — question ID + text with the lowest `yes_rate`

### Cross-Workflow Summary

- `total_workflows_with_evals` — unique workflow count
- `total_runs_analyzed` — total runs checked
- `total_evals_results` — runs with `evals.jsonl`
- `total_evals_failures` — runs with evals job failure conditions (missing results and/or failed `push_evals_state`)
- `overall_evals_job_success_rate` = `total_evals_results / (total_evals_results + total_evals_failures) * 100`
- `overall_yes_rate` — YES answers / total answers across all runs
- `most_failing_questions` — top 3 questions (by NO rate) across all workflows

## Phase 5: Classify Feature Health

| Status | Condition |
|---|---|
| **HEALTHY** | `overall_evals_job_success_rate >= 80%` AND `overall_yes_rate >= 60%` |
| **DEGRADED** | `overall_evals_job_success_rate` between 50–80%, OR `overall_yes_rate` between 30–60% |
| **BROKEN** | `overall_evals_job_success_rate < 50%`, OR zero evals runs found, OR evals job not producing any results |

## Phase 6: Generate Output

### HEALTHY or DEGRADED — Create Issue with Data Science Summary

Create one GitHub issue.

**Title**: `[evals] Daily Evals Feature Report - YYYY-MM-DD` (date in UTC, ISO 8601 format, e.g. `2026-07-15`)

**Body structure** (use `###` or lower for all headers):

```markdown
### Executive Summary

[2–3 sentences: total workflows, runs analyzed, overall health status, headline finding]

> [!NOTE]
> Status: HEALTHY / DEGRADED / BROKEN — [one-line reason]

### Key Metrics

| Metric | Value |
|---|---|
| Workflows with evals | N |
| Runs analyzed | N |
| Runs with evals results | N |
| Evals job success rate | X% |
| Overall YES rate | X% |

### Per-Workflow Pass Rates

| Workflow | Runs | Evals Job Success | Run Pass Rate | Lowest-Scoring Question |
|---|---|---|---|---|
| workflow-name | N | X% | X% | "question text" (X% YES) |

<details>
<summary>Per-Question Breakdown per Workflow</summary>

For each workflow, show a table:

| Question ID | Question | YES | NO | YES Rate |
|---|---|---|---|---|
| builds | Does the generated code compile? | 8 | 2 | 80% |

</details>

### Quality Signals

[Top 3 failing questions with YES rate and affected workflows — these are regressions or areas needing prompt improvement]

### Recommendations

[2–3 concrete actions, for example:
- Which workflow prompts to improve based on low YES rates
- Whether the evals model is appropriate for the question complexity
- Any evals job failures to investigate]

### References

[Up to 3 run URLs as `[§<run-id>](<url>)` links]
```

### BROKEN — Create Actionable Issue

When the evals feature is BROKEN (job success rate < 50% or zero results), the issue body
must include actionable investigation steps instead of the data science summary.

> [!CAUTION]
> Status: BROKEN — the evals job is not producing results.

#### Investigation Checklist

1. Run `gh aw audit <run-id>` on the most recent evals-declaring workflow run to inspect the evals job steps.
2. Check if the `evals` artifact is present: `gh run download <run-id> -n evals`.
3. Look for errors in the evals engine execution step: `Parse BinEval results`.
4. Verify that the `evals` frontmatter config is valid (each question must have `id` and `question`).
5. Check if the agentic engine has sufficient credit budget: compare `max-ai-credits` vs actual usage.
6. Check whether the evals branch `evals/<workflow-id>` exists and contains recent commits.

#### Affected Runs

List up to 5 run IDs where evals were declared but produced no results, with direct links.

### No Data — Noop

If no runs with the evals artifact are found in the 7-day window and no workflows declare evals:
```
noop("No evals-enabled workflow runs found in the last 7 days. The evals feature may not yet be widely adopted in this repository.")
```

If workflows declared evals but zero results were produced:
```
noop("Evals declared in N workflows but no evals.jsonl artifacts found. Use [evals-alert] issue.")
```
(Skip the noop and create the BROKEN issue instead in this case.)

## Token Budget Guidelines

- **One broad `logs` call** — do not enumerate workflows individually before calling logs.
- **Cap at 40 runs** — stop analyzing once the cap is reached.
- **Summarize, do not transcribe** — report statistics, not raw JSONL content.
- **Stop after `create_issue` or `noop`** — no extra tool calls after publishing.

Begin your analysis now. Download the logs, analyze evals results, and publish the report.