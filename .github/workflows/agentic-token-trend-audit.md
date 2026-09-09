---
private: true
description: On-demand AIC audit for a user-specified date range
on:
  workflow_dispatch:
    inputs:
      date_range:
        description: "Date range for logs (format: <start>..<end>, where each side is YYYY-MM-DD or a delta like -30d; e.g. 2026-05-01..2026-05-31 or -30d..-0d)"
        required: true
        type: string
        default: "-30d..-0d"

imports:
  - shared/graders.md
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
network:
  allowed:
    - defaults
    - python
tracker-id: agentic-token-trend-audit
safe-outputs:
  create-issue:
    expires: 3d
    title-prefix: "[agentic-token-trend-audit] "
    max: 1
    close-older-issues: true
  upload-asset:
    max: 5
    allowed-exts: [.png, .jpg, .jpeg, .svg]
sandbox:
  agent:
    runtime: cloud-hypervisor
tools:
  agentic-workflows:
  bash:
    - "*"
features:
  gh-aw-detection: true
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
      DATE_RANGE_INPUT: ${{ github.event.inputs.date_range }}
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/token-audit

      DATE_RANGE="$DATE_RANGE_INPUT"
      if [[ "$DATE_RANGE" != *".."* ]]; then
        echo "❌ Invalid date_range input: $DATE_RANGE"
        echo "Expected format: <start>..<end> (for example 2026-05-01..2026-05-31 or -30d..now)"
        exit 1
      fi

      START_DATE="${DATE_RANGE%%..*}"
      END_DATE="${DATE_RANGE##*..}"

      # Download logs for the requested range as JSON.
      # Allow partial results — gh aw logs streams incrementally, so even if
      # it hits an API rate limit partway through, the JSON written so far is
      # still valid and should be processed by the agent.
      LOGS_EXIT=0
      gh aw logs \
        --start-date "$START_DATE" \
        --end-date "$END_DATE" \
        --json \
        -c 500 \
        > /tmp/gh-aw/token-audit/workflow-logs.json || LOGS_EXIT=$?

      if [ -s /tmp/gh-aw/token-audit/workflow-logs.json ]; then
        TOTAL=$(jq '.runs | length' /tmp/gh-aw/token-audit/workflow-logs.json)
        echo "✅ Downloaded $TOTAL agentic workflow runs ($START_DATE to $END_DATE)"
        if [ "$LOGS_EXIT" -ne 0 ]; then
          echo "⚠️ gh aw logs exited with code $LOGS_EXIT (partial results — likely API rate limit)"
        fi
      else
        echo "❌ No log data downloaded (exit code $LOGS_EXIT)"
        echo '{"runs":[],"summary":{}}' > /tmp/gh-aw/token-audit/workflow-logs.json
      fi
timeout-minutes: 25
evals:
  - id: date_range_analyzed
    question: Did the agent analyze AIC usage for the user-specified date range?
  - id: trend_report_created
    question: Was a trend report created with per-workflow AIC data for the requested period?

engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
---

# On-Demand Agentic Workflow AIC Trend Audit

You are the Agentic Workflow AIC Trend Auditor — a workflow that analyzes AI Credit (AIC) consumption across a caller-specified date range.

## Mission

1. Parse the pre-downloaded agentic workflow logs and compute per-workflow AIC metrics.
2. Publish a concise audit issue with AIC findings for the requested range.

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
| `token_usage` | int | Historical raw token total (`omitempty`) |
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
| `working_set` | object or null | Working-Set Rebuild Factor (WSRF) data: `measurement_state`, `rebuild_factor`, `cumulative_input_tokens`, `peak_input_tokens` |

## Phase 1 — Process Logs

Write a Python script to `/tmp/gh-aw/token-audit/process_audit.py` and run it. The script must:

1. Load `/tmp/gh-aw/token-audit/workflow-logs.json` and extract `.runs` for the requested input range `${{ github.event.inputs.date_range }}`.
2. Filter to `status == "completed"` runs only.
3. Use each run's `aic` field as the preferred cost metric.
   - Treat missing/null `aic` as `0`.
   - Keep `effective_tokens` only as a diagnostic legacy field when it is present in the logs.
   - Extract `working_set.rebuild_factor` (WSRF) when `working_set.measurement_state == "measured"`; treat missing or `unavailable`/`partial` states as no data point (do not fabricate a value).
4. Group by `workflow_name` and compute per-workflow aggregates:
   - `run_count`, `total_aic`, `avg_aic`, `total_turns`, `avg_turns`, `total_action_minutes`, `error_count`, `warning_count`, `avg_wsrf` (mean of available `working_set.rebuild_factor` values; `null` when none available)
5. Compute an overall summary: total runs, total AIC, total action minutes.
6. Sort workflows descending by `total_aic`.
7. Save the result to `/tmp/gh-aw/token-audit/audit_snapshot.json` with this shape:

```json
{
  "date": "YYYY-MM-DD",
  "period_range": "${{ github.event.inputs.date_range }}",
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
      "avg_wsrf": F,
      "latest_run_url": "..."
    }
  ]
}
```

Treat any missing numeric AIC field as `0`.

## Phase 2 — Generate Charts

Create chart images in `/tmp/gh-aw/token-audit/charts/` using Python, `matplotlib`, and `seaborn` with `whitegrid` styling:

1. **AIC by workflow** (`token_by_workflow.png`): a horizontal bar chart of the top 15 workflows by total AIC from `audit_snapshot.json`.
2. **Daily AIC trend** (`daily_token_trend.png`, optional): a line chart that aggregates completed-run AIC by UTC day across the requested date range (skip this chart if fewer than 2 daily points exist).

Chart requirements:

- The preinstalled Python packages live in `/tmp/gh-aw/token-audit/site-packages`. Set `PYTHONPATH=/tmp/gh-aw/token-audit/site-packages${PYTHONPATH:+:$PYTHONPATH}` for every Python command you write in Phase 1 or Phase 3 that imports `pandas`, `matplotlib`, or `seaborn`, for example: `PYTHONPATH=/tmp/gh-aw/token-audit/site-packages${PYTHONPATH:+:$PYTHONPATH} python3 /tmp/gh-aw/token-audit/process_audit.py`.
- Use 300 DPI and a white background.
- Add clear axis labels and titles.
- Save only PNG files.
- Always generate `token_by_workflow.png`.
- If there are fewer than 2 daily points for the requested range, skip `daily_token_trend.png` and explain why in the issue.
- After generating each chart, call `upload_asset` with its file path.
- Replace `UPLOAD_URL_WORKFLOW_PLACEHOLDER` only after `token_by_workflow.png` is uploaded.
- Replace `UPLOAD_URL_DAILY_TREND_PLACEHOLDER` only after `daily_token_trend.png` is uploaded.
- If `daily_token_trend.png` is skipped, omit only the `![Daily AIC Trend](...)` image markdown line and its placeholder replacement; keep the Trends section text and explicitly state why the chart was skipped.

## Phase 3 — Publish Audit Issue

Create an issue with these sections:

### Formatting Requirements

- Use `###` for main sections and `####` for subsections inside the issue body.
- Keep the executive summary and final observations visible without collapsible sections.
- Put verbose tables or supporting detail inside `<details><summary>...</summary>` blocks.
- If you cite specific workflow runs, format them as links like `[§12345](https://github.com/${{ github.repository }}/actions/runs/12345)` and include up to 3 under `**References:**`.

### Report Template

```
### 📊 Executive Summary

- **Period**: requested date range (YYYY-MM-DD to YYYY-MM-DD)
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

<!-- Optional: include only if daily_token_trend.png was generated and uploaded; otherwise remove this line -->
![Daily AIC Trend](UPLOAD_URL_DAILY_TREND_PLACEHOLDER)

Summarize daily AIC movement across the requested range (up/down days, spikes, and overall direction) when daily points are available; if the chart was skipped, explicitly state why.

<details>
<summary><b>Full Per-Workflow Breakdown</b></summary>

[Complete table of all workflows sorted by total AIC]

</details>

### 💡 Observations

- Identify any workflow with >30% of total AIC as a "heavy hitter"
- Note workflows with high error/warning counts relative to runs
- Flag any workflow whose avg AIC per run exceeds 10.00
- Flag any workflow whose `avg_wsrf` is notably high relative to peers (context repeatedly rebuilt near peak size rather than growing incrementally turn over turn) when enough measured runs exist

**Data snapshot**: `/tmp/gh-aw/token-audit/audit_snapshot.json`
```

## Important Notes

- Use `// 0` (null coalescing) in jq and `.get(field, 0)` in Python for nullable numeric fields.
- Treat `working_set.rebuild_factor` as unavailable (not `0`) when `measurement_state != "measured"` or the field is absent; average only over runs with a measured value and state the sample size when reporting `avg_wsrf`.
- Distinguish between these two cases in the issue:
  - the raw `.runs` array is empty
  - the raw `.runs` array is non-empty but none of the runs are `status == "completed"`
- Report those cases differently:
  - if `len(runs) == 0` (or `jq '.runs | length' == 0`), say the collection window returned no runs
  - if `len(runs) > 0` and there are zero completed runs, say the collection window had runs but none completed yet
- Do not claim the raw log file was empty unless you verified `len(runs) == 0` (or `jq '.runs | length' == 0`).
- Keep the issue concise — the optimizer workflow will do the deep analysis.

### Output Format

Structure reports as: overview → key metrics/issues → collapsible detail → next actions.
