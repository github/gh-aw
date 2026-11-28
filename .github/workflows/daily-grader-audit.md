---
private: true
emoji: "📊"
description: Daily audit of workflow runs with deterministic grader results, producing a downloadable 24-hour report artifact
on:
  schedule: daily around 8:00
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  copilot-requests: write
max-ai-credits: 1500
max-daily-ai-credits: 5000
model: router/economy
engine:
  id: claude
  model-provider: github
  mcp:
    tool-timeout: 10m
tools:
  cli-proxy: true
  agentic-workflows:
safe-outputs:
  upload-artifact:
    max-uploads: 1
    retention-days: 1
    skip-archive: true
timeout-minutes: 60
features:
  gh-aw-detection: true
imports:
  - shared/reporting.md
graders: {}
evals:
  - id: grader_runs_audited
    question: Did the agent inspect workflow runs with deterministic grader results from the last 24 hours?
  - id: downloadable_report_uploaded
    question: Did the agent generate and upload a grader audit report artifact retained for 24 hours?
---

# Daily Grader Audit

You are the grader audit reporter for GitHub Agentic Workflows.

## Mission

Inspect workflow runs from the last 24 hours that produced deterministic grader results, summarize grader health, and upload a Markdown report artifact retained for 24 hours. If collection is incomplete, report the available results and clearly identify the coverage gap.

## Context

- Repository: `${{ github.repository }}`
- Run ID: `${{ github.run_id }}`
- Window: last 24 hours ending at workflow start (UTC)
- Report path: `/tmp/gh-aw/agent/grader-audit-report.md`

## Phase 1: Fetch Grader Runs

Use the agentic-workflows MCP `logs` tool with this bounded request:

```json
{
  "workflow_name": "",
  "count": 100,
  "start_date": "-1d",
  "graders": true,
  "artifacts": ["graders", "usage"],
  "timeout": 5
}
```

Read the returned JSON file path, including any partial result. Follow continuations, preserving the returned repository and cursor parameters, for at most eight requests total. Keep each request's timeout at 5 minutes. Then stop collecting and generate the report; do not retry failed requests. Deduplicate runs by run ID across responses.

Record whether collection is complete. If a partial response remains unresolved, a continuation remains, a request fails, or coverage is otherwise unknown, explicitly label the report incomplete and include the reason and any remaining continuation details. Do not claim that the report covers all runs in the window. If no data is returned, report the collection failure rather than claiming no grader results exist.

The logs tool filters to runs with grader results and each `runs[]` entry may include:

- `run_id`
- `workflow_name`
- `conclusion`
- `created_at`
- `url`
- `graders.total`
- `graders.passed`
- `graders.failed`
- `graders.error_count`
- `graders.unavailable_count`
- `graders.results[]`

If no matching runs are found, still write a report. Claim that no grader results were available for the window only when collection was complete; otherwise say none were found in the collected data.

## Phase 2: Analyze Results

For each run with `graders.results[]`, compute:

- Per-run totals: total, passed, failed, errors, unavailable.
- Per-grader totals grouped by `id`: runs observed, pass count, fail count, error count, unavailable count.
- Pass rate per grader: `pass count / runs observed * 100`.
- Notable failures: any grader result where `status` is `fail`, `error`, or `unavailable`.

Do not infer missing graders from workflow files. Only report grader data present in the logs tool output.

## Phase 3: Generate Report

Create `/tmp/gh-aw/agent/grader-audit-report.md` with:

### Summary

One short paragraph covering the window, number of runs, number of workflows, overall grader health, and whether collection was complete.

### Key Metrics

| Metric | Value |
|---|---|
| Runs with grader results | N |
| Workflows with grader results | N |
| Grader result rows | N |
| Passing results | N |
| Failing results | N |
| Error results | N |
| Unavailable results | N |

### Per-Grader Results

| Grader | Runs | Pass | Fail | Error | Unavailable | Pass Rate |
|---|---:|---:|---:|---:|---:|---:|
| grader-id | N | N | N | N | N | X% |

### Per-Run Results

| Run | Workflow | Conclusion | Total | Pass | Fail | Error | Unavailable |
|---|---|---|---:|---:|---:|---:|---:|
| [§123](https://github.com/owner/repo/actions/runs/123) | workflow-name | success | N | N | N | N | N |

<details>
<summary>Notable grader failures</summary>

| Run | Workflow | Grader | Status | Value | Threshold | Message |
|---|---|---|---|---:|---|---|
| [§123](https://github.com/owner/repo/actions/runs/123) | workflow-name | grader-id | fail | 0.2 | >= 0.8 | explanation |

</details>

### Recommendations

List concrete follow-up actions when failed, errored, or unavailable grader results exist. If coverage is incomplete, recommend completing collection before drawing conclusions. Otherwise, when there are no such results, state that no immediate action is needed.

### Context

- Window: last 24 hours ending at workflow start (UTC)
- Coverage: complete or incomplete, with reasons and remaining continuation details when applicable
- Source: `gh aw logs --graders --start-date -1d --artifacts graders,usage`
- Generated by run `${{ github.run_id }}`

## Phase 4: Upload Report

Stage and upload the report:

1. Copy `/tmp/gh-aw/agent/grader-audit-report.md` to `${RUNNER_TEMP}/gh-aw/safeoutputs/upload-artifacts/grader-audit-report.md`.
2. Call the `upload_artifact` safe-output tool with:

```json
{ "path": "grader-audit-report.md" }
```

The workflow safe-output configuration retains the uploaded report for 1 day.
