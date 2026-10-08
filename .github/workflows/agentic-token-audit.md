---
description: Daily audit of AI Credit (AIC) usage across all agentic workflows with historical trend tracking
on:
  schedule:
    - cron: "daily around 12:00 on weekdays"
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
network:
  allowed:
    - defaults
    - python
tracker-id: agentic-token-audit
safe-outputs:
  create-issue:
    expires: 3d
    title-prefix: "[agentic-token-audit] "
    max: 1
    close-older-issues: true
  upload-asset:
    max: 5
    allowed-exts: [.png, .jpg, .jpeg, .svg]
tools:
  agentic-workflows:
  bash:
    - "*"
  repo-memory:
    branch-name: "memory/token-audit"
    description: "Historical daily workflow AIC snapshots (shared with agentic-token-optimizer)"
    file-glob: ["*.json", "*.jsonl", "*.csv", "*.md"]
    max-file-size: 102400
    max-patch-size: 51200
steps:
  - name: Setup Python
    uses: actions/setup-python@v7.0.0
    with:
      python-version: "3.12"
  - name: Setup local chart workspace
    run: |
      mkdir -p /tmp/gh-aw/token-audit/charts /tmp/gh-aw/token-audit/site-packages
  - name: Install Python chart dependencies
    run: |
      python3 -m pip install --quiet --target /tmp/gh-aw/token-audit/site-packages pandas matplotlib seaborn
  - name: Download agentic workflow logs
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/token-audit

      # Download last 24 hours of agentic workflow logs as JSON
      # Allow partial results — gh aw logs streams incrementally, so even if
      # it hits an API rate limit partway through, the JSON written so far is
      # still valid and should be processed by the agent.
      LOGS_EXIT=0
      gh aw logs \
        --start-date -1d \
        --json \
        -c 100 \
        > /tmp/gh-aw/token-audit/workflow-logs.json || LOGS_EXIT=$?

      if [ -s /tmp/gh-aw/token-audit/workflow-logs.json ]; then
        TOTAL=$(jq '.runs | length' /tmp/gh-aw/token-audit/workflow-logs.json)
        echo "✅ Downloaded $TOTAL agentic workflow runs (last 24 hours)"
        if [ "$LOGS_EXIT" -ne 0 ]; then
          echo "⚠️ gh aw logs exited with code $LOGS_EXIT (partial results — likely API rate limit)"
        fi
      else
        echo "❌ No log data downloaded (exit code $LOGS_EXIT)"
        echo '{"runs":[],"summary":{}}' > /tmp/gh-aw/token-audit/workflow-logs.json
      fi
timeout-minutes: 25
source: githubnext/agentic-ops@c611242a76866fb51d4f7d660c80badc504dd473
evals:
  - id: aic_usage_analyzed
    question: Did the agent audit AI Credit (AIC) usage across agentic workflows for the target period?
  - id: report_or_discussion_created
    question: Was a report or discussion created with historical trend data and per-workflow AIC breakdowns?
---

# Daily Agentic Workflow AIC Usage Audit

You are the Agentic Workflow AIC Auditor — a workflow that tracks daily AI Credit (AIC) consumption across all agentic workflows in this repository and maintains a historical record for trend analysis.

## Mission

1. Parse the pre-downloaded agentic workflow logs and compute per-workflow AIC usage metrics.
2. Persist today's snapshot to repo-memory so the optimizer (and future runs of this audit) can read historical data.
3. Publish a concise audit issue summarizing today's usage and trend highlights.

## Data Sources

### Pre-downloaded logs

The workflow logs are at `/tmp/gh-aw/token-audit/workflow-logs.json`. The file is the raw JSON output of `gh aw logs --json` with this top-level shape:

```json
{
  "summary": { "total_runs": N, ... },
  "runs": [ ... ],
  "tool_usage": [ ... ],
  "mcp_tool_usage": { ... },
  ...
}
```

Each element of `.runs` is a `RunData` object with (among others):

| Field | Type | Notes |
|---|---|---|
| `workflow_name` | string | Human-readable name |
| `workflow_path` | string | `.github/workflows/....lock.yml` |
| `aic` | float | AI Credits (AIC); preferred cost metric |
| `token_usage` | int | Raw token total (`omitempty` — treat missing/null as 0) |
| `effective_tokens` | int | Legacy metric retained for compatibility (prefer AI Credits) |
| `action_minutes` | float | Billable GitHub Actions minutes |
| `turns` | int | Number of agent turns |
| `duration` | string | Human-readable duration |
| `created_at` | ISO 8601 | Run creation time |
| `run_id` | int64 | Unique run ID |
| `url` | string | Link to the run |
| `status` | string | `completed`, `in_progress`, etc. |
| `conclusion` | string | `success`, `failure`, etc. |
| `error_count` | int | Errors encountered |
| `warning_count` | int | Warnings encountered |
| `token_usage_summary` | object or null | Firewall-level breakdown by model |

### Repo-memory (historical snapshots)

Previous snapshots live at `/tmp/gh-aw/repo-memory/default/`. Each daily snapshot is stored as a JSON file named `YYYY-MM-DD.json` with the schema below.

## Phase 1 — Process Logs

Write a Python script to `/tmp/gh-aw/token-audit/process_audit.py` and run it. The script must:

1. Load `/tmp/gh-aw/token-audit/workflow-logs.json` and extract `.runs`.
2. Filter to `status == "completed"` runs only.
3. Group by `workflow_name` and compute per-workflow aggregates:
   - `run_count`, `total_aic`, `avg_aic`, `total_turns`, `avg_turns`, `total_action_minutes`, `error_count`, `warning_count`
4. Compute an overall summary: total runs, total AIC, total action minutes.
5. Sort workflows descending by `total_aic`.
6. Save the result to `/tmp/gh-aw/token-audit/audit_snapshot.json` with this shape:

```json
{
  "date": "YYYY-MM-DD",
  "period_days": 30,
  "overall": {
    "total_runs": N,
    "total_aic": N,
    "total_action_minutes": F
  },
  "workflows": [
    {
      "workflow_name": "...",
      "run_count": N,
      "total_aic": N,
      "avg_aic": N,
      "total_turns": N,
      "avg_turns": F,
      "total_action_minutes": F,
      "error_count": N,
      "warning_count": N,
      "latest_run_url": "..."
    }
  ]
}
```

Use `aic` as the primary cost metric and treat null/missing `aic` as `0`.

## Phase 2 — Persist Snapshot to Repo-Memory

1. Read the snapshot from `/tmp/gh-aw/token-audit/audit_snapshot.json`.
2. Copy it to `/tmp/gh-aw/repo-memory/default/YYYY-MM-DD.json` (today's UTC date).
3. This file is what the optimizer workflow reads to identify high-usage workflows.

Also maintain a rolling summary file at `/tmp/gh-aw/repo-memory/default/rolling-summary.json` that contains an array of daily overall totals (`date`, `total_aic`, `total_runs`, `total_action_minutes`) for the last 90 entries. Load the existing file, append today's entry, trim to 90, and save.

Do not append a synthetic zero-valued entry to `rolling-summary.json` when either of these conditions is true:

- the raw `.runs` array is empty
- the raw `.runs` array is non-empty but there are zero completed runs in the current window

Report those two cases differently in the issue as described below so the empty-window diagnosis stays precise while the historical trend remains unchanged.

## Phase 3 — Generate Charts

Create up to two chart images in `/tmp/gh-aw/token-audit/charts/` using Python, `matplotlib`, and `seaborn` with `whitegrid` styling:

1. **AIC by workflow** (`token_by_workflow.png`): a horizontal bar chart of the top 15 workflows by total AIC from `audit_snapshot.json`.
2. **Historical AIC trend** (`token_trend.png`): a line chart from `rolling-summary.json`.

Chart requirements:

- The preinstalled Python packages live in `/tmp/gh-aw/token-audit/site-packages`. Set `PYTHONPATH=/tmp/gh-aw/token-audit/site-packages${PYTHONPATH:+:$PYTHONPATH}` for every Python command that imports `pandas`, `matplotlib`, or `seaborn`, for example: `PYTHONPATH=/tmp/gh-aw/token-audit/site-packages${PYTHONPATH:+:$PYTHONPATH} python3 /tmp/gh-aw/token-audit/process_audit.py`.
- Use 300 DPI and a white background.
- Add clear axis labels and titles.
- Save only PNG files.
- If there are fewer than 2 rolling-summary points, skip the trend chart and explain why in the issue.
- After generating each chart, call `upload_asset` with its file path.
- In the issue template below, replace `UPLOAD_URL_WORKFLOW_PLACEHOLDER` with the URL returned for `token_by_workflow.png`.
- In the issue template below, replace `UPLOAD_URL_TREND_PLACEHOLDER` with the URL returned for `token_trend.png`.
- If a chart is skipped, omit that image markdown line entirely instead of leaving a placeholder behind.

## Phase 4 — Publish Audit Issue

Create an issue with these sections:

### Formatting Requirements

- Use `###` for main sections and `####` for subsections inside the issue body.
- Keep the executive summary and final observations visible without collapsible sections.
- Put verbose tables or supporting detail inside `<details><summary>...</summary>` blocks.
- If you cite specific workflow runs, format them as links like `[§12345](https://github.com/${{ github.repository }}/actions/runs/12345)` and include up to 3 under `**References:**`.

### Report Template

```
### 📊 Executive Summary

- **Period**: last 24 hours (YYYY-MM-DD to YYYY-MM-DD)
- **Total runs**: N
- **Total AIC**: N.NN
- **Total Actions minutes**: X.X min
- **Active workflows**: N

### 🏆 Top 5 Workflows by AIC Usage

| Workflow | Runs | Total AIC | Avg AIC |
|---|---|---|---|
| ... | ... | ... | ... |

### 📈 Trends

Embed chart images using uploaded asset URLs when available:

![AIC by Workflow](UPLOAD_URL_WORKFLOW_PLACEHOLDER)

![Historical AIC Trend](UPLOAD_URL_TREND_PLACEHOLDER)

Summarize AIC changes from `rolling-summary.json` when historical data is available.

<details>
<summary><b>Full Per-Workflow Breakdown</b></summary>

[Complete table of all workflows sorted by total AIC]

</details>

### 💡 Observations

- Identify any workflow with >30% of total AIC as a "heavy hitter"
- Note workflows with high error/warning counts relative to runs
- Flag any workflow whose avg AIC per run exceeds 10.00

**Data snapshot**: `memory/token-audit/YYYY-MM-DD.json`
```

## Important Notes

- Use `// 0` (null coalescing) in jq and `.get(field, 0)` in Python for nullable numeric fields.
- Distinguish between these two cases in the issue:
  - the raw `.runs` array is empty
  - the raw `.runs` array is non-empty but none of the runs are `status == "completed"`
- Report those cases differently:
  - if `len(runs) == 0` (or `jq '.runs | length' == 0`), say the collection window returned no runs
  - if `len(runs) > 0` and there are zero completed runs, say the collection window had runs but none completed yet
- Do not claim the raw log file was empty unless you verified `len(runs) == 0` (or `jq '.runs | length' == 0`).
- Keep the issue concise — the optimizer workflow will do the deep analysis.

## Experiment OTEL Span Attributes

If the file `/tmp/gh-aw/experiments/assignments.json` exists, read it and emit one OTEL span
attribute per experiment using the `otlp.cjs` `logSpan` helper:

```javascript
const fs = require('fs');
const assignmentsFile = '/tmp/gh-aw/experiments/assignments.json';
if (fs.existsSync(assignmentsFile)) {
  const assignments = JSON.parse(fs.readFileSync(assignmentsFile, 'utf8'));
  const names = Object.keys(assignments).sort();
  if (names.length > 0) {
    const attrs = { 'gh_aw.experiment.names': names.join(',') };
    for (const name of names) {
      attrs[`gh_aw.experiment.${name}`] = assignments[name];
    }
    const otlp = require('/tmp/gh-aw/actions/otlp.cjs');
    await otlp.logSpan('experiment', attrs);
  }
}
```

This enables filtering workflow runs by experiment variant in Datadog, Honeycomb, or any
OTLP-compatible backend. Attribute keys follow the pattern `gh_aw.experiment.<name>` with the
assigned variant as the value, plus `gh_aw.experiment.names` as a comma-separated index.
