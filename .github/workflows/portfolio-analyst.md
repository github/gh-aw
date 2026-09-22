---
emoji: "📊"
description: Weekly portfolio analyst that maps AI Credit spend per workflow from OpenTelemetry telemetry with colorful charts and 30-day trends
on:
  schedule:
    - cron: "0 16 * * 1"
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read

sandbox:
  agent:

tracker-id: portfolio-analyst
engine: claude
max-turns: 20
max-ai-credits: 250
strict: true
network:
  allowed:
    - defaults
    - python
safe-outputs:
  create-discussion:
    title-prefix: "[portfolio] "
    category: "audits"
    close-older-discussions: true
    expires: 7d
    max: 1
  upload-asset:
    max: 5
    allowed-exts: [.png, .jpg, .jpeg, .svg]
tools:
  bash:
    - "*"
timeout-minutes: 30
imports:
  - shared/mcp/sentry.md
  - shared/mcp/grafana.md
  - shared/trends.md
  - shared/reporting.md
---

# Workflow Portfolio Analyst

You are the portfolio analyst for this repository's agentic workflows. Your job is to turn the last 30 days of OpenTelemetry-backed AIC telemetry into a portfolio-style spend map that shows exactly where AI Credits (AIC) are going, which workflows are trending up, and where reliability problems are wasting spend.

## Mission

1. Compute per-workflow AIC, run volume, reliability, and change-over-time metrics from OpenTelemetry telemetry in Sentry and Grafana.
2. Generate a colorful dashboard-style set of charts that make heavy hitters and trend changes obvious at a glance.
3. Publish one concise GitHub Discussion with the charts, the key findings, and concrete optimization targets.

## Inputs

- Primary telemetry sources:
  - Sentry spans via the imported `shared/mcp/sentry.md` MCP server
  - Grafana Tempo traces via the imported `shared/mcp/grafana.md` MCP server
- Chart workspace: `/tmp/gh-aw/python/`
- Save any intermediate CSV or JSON data under `/tmp/gh-aw/python/data/`

### Source Priority

Use data sources in this order:

1. **Sentry spans** as the canonical 30-day AIC source.
2. **Grafana Tempo traces** as corroboration, coverage checking, and gap detection.
3. **Local artifact fallback** only when backend AIC is unavailable or incomplete:
  - `gh aw logs --start-date -30d --json --artifacts agent -c 300 -o /tmp/gh-aw/portfolio-agent-preview > /tmp/gh-aw/portfolio-agent-preview/summary.json`
  - Per-run files under `/tmp/gh-aw/portfolio-agent-preview/run-<id>/`:
    - `agent_usage.json`
    - `run_summary.json`
    - `otel.jsonl`

Do **not** use the plain top-level `gh aw logs --json` run summary as the primary spend source when it omits `aic`. Prefer OTEL-derived fields and `agent_usage.json`.

### Field Extraction Rules

Use these fields when present, with this precedence:

| Field | Purpose |
|---|---|
| `github.workflow` / `gh-aw.workflow.name` / `workflow.name` | workflow group-by key |
| `gh-aw.aic` / `gh_aw.aic` / `agent_usage.aic` / `aic` | primary spend metric |
| `github.run_id` / `gh-aw.run.id` / `gh_aw.run.id` | run identifier |
| `github.actions.run_url` | run URL |
| `timestamp` / span time | daily trend bucketing |
| run conclusion from local fallback or available run metadata | success/failure/cancelled outcome |
| `action_minutes` from local fallback | Actions cost proxy |
| `turns` from local fallback | conversation volume |
| `error_count` / `warning_count` from local fallback | reliability signal |

Treat missing numeric fields as zero **only after** trying all precedence sources above. When Grafana lacks numeric AIC, report it as an observability gap instead of assuming zero.

Do not use Sentry `sum()`, `avg()`, percentile, or other numeric aggregations on any field whose Sentry schema type is not confirmed numeric. If Sentry reports a field-type or query-syntax 400 for one of these fields, treat that query shape as invalid, fetch raw event rows or `count()` instead, aggregate numeric-looking values locally in Python, and fall back to local artifacts when backend rows are not usable.

## Phase 1: Build The Portfolio Dataset

Write a Python script to `/tmp/gh-aw/python/build_portfolio.py` and run it with the Python environment already prepared by the shared imports.

The script must:

1. Query Sentry for the last 30 days of spans or transactions with AIC fields.
2. Query Grafana Tempo for the last 30 days to confirm field presence, coverage, and backend gaps.
3. Normalize the returned events into a single row set with these minimum fields:
  - `workflow_name`
  - `run_id`
  - `run_url`
  - `timestamp`
  - `aic`
  - `source_backend`
  - `conclusion` when available
4. If Sentry and Grafana do not yield enough usable numeric AIC rows, populate missing rows from the local fallback artifact set under `/tmp/gh-aw/portfolio-agent-preview/` using `agent_usage.json` plus `run_summary.json`, and use `otel.jsonl` only for workflow/run attribution when needed.
5. Keep only completed runs for portfolio calculations when completion state is available.
6. Create a per-workflow summary with these columns:
   - `workflow_name`
   - `run_count`
   - `total_aic`
   - `avg_aic`
   - `median_aic`
   - `success_rate`
   - `failure_rate`
   - `cancelled_rate`
   - `total_action_minutes`
   - `avg_action_minutes`
   - `total_turns`
   - `avg_turns`
   - `error_count`
   - `warning_count`
   - `latest_run_url`
   - `latest_run_id`
7. Compute two comparison windows:
   - last 7 days
   - previous 7 days before that
8. For each workflow compute:
   - `recent_7d_aic`
   - `prior_7d_aic`
   - `aic_delta_7d`
   - `aic_delta_pct_7d`
9. Compute daily totals across the full 30-day window:
   - `date`
   - `total_aic`
   - `run_count`
   - `failure_runs`
   - `cancelled_runs`
10. Save the outputs to:
   - `/tmp/gh-aw/python/data/workflow_portfolio.csv`
   - `/tmp/gh-aw/python/data/daily_aic.csv`
   - `/tmp/gh-aw/python/data/outcome_mix.csv`
   - `/tmp/gh-aw/python/data/portfolio_snapshot.json`
   - `/tmp/gh-aw/python/data/telemetry_coverage.json`

The JSON snapshot should include:

```json
{
  "window": "last-30-days",
  "generated_at": "...",
  "overall": {
    "total_runs": 0,
    "total_aic": 0,
    "active_workflows": 0,
    "failure_rate": 0,
    "cancelled_rate": 0
  },
  "top_workflows": [],
  "rapid_risers": [],
  "heavy_hitters": []
}
```

Definitions:

- `heavy_hitter`: any workflow with at least 15 completed runs and at least 15% of total AIC.
- `rapid_riser`: any workflow with at least 5 AIC in the last 7 days and `aic_delta_pct_7d >= 25`.
- `wasted_aic`: approximate as the sum of AIC from runs where `conclusion` is `failure` or `cancelled`.

Also record telemetry coverage facts:

- `sentry_events_analyzed`
- `grafana_events_analyzed`
- `events_with_numeric_aic_sentry`
- `events_with_numeric_aic_grafana`
- `fallback_runs_used`
- `grafana_aic_queryable` boolean

If there are no completed runs, still emit the files with empty tables and a snapshot explaining the empty window.

## Phase 2: Generate Colorful Portfolio Charts

Create exactly 4 PNG charts in `/tmp/gh-aw/python/charts/`.

Use `seaborn` with `whitegrid` styling, 300 DPI minimum, 12x7 inch figures or larger, readable labels, and intentional colors. Favor a warm-to-cool palette that makes spend concentration obvious.

### Chart 1: Portfolio Spend Map

File: `/tmp/gh-aw/python/charts/portfolio_spend_map.png`

Scatter or bubble chart where:

- x-axis: portfolio spend share percentage
- y-axis: `failure_rate` as a percentage
- bubble size: `total_aic`
- bubble color: clipped trend intensity from `aic_delta_pct_7d`

Add visible portfolio bands so the plot reads like a risk map:

- low risk / low concentration
- core spend / moderate risk
- concentrated and fragile

Annotate the top spenders and any rapid risers. This chart should feel like a real portfolio map: high-spend, high-failure workflows must stand out immediately.

### Chart 2: Top Workflow Spenders

File: `/tmp/gh-aw/python/charts/top_workflow_spenders.png`

Horizontal bar chart of the top 12 workflows by `total_aic`.

- Color bars by health band:
  - green for failure rate under 10%
  - amber for failure rate 10-25%
  - red for failure rate above 25%
- Label each bar with total AIC, spend share, and run count.
- Add a visual marker for the heavy-hitter threshold.

### Chart 3: Daily Portfolio Trend

File: `/tmp/gh-aw/python/charts/daily_portfolio_trend.png`

Time-series chart showing:

- daily `total_aic`
- 7-day moving average
- a visible overlay for failure/cancelled run volume

Call out the biggest spike and whether the last 7 days are trending above or below the prior 7 days.

### Chart 4: Outcome Mix

File: `/tmp/gh-aw/python/charts/outcome_mix.png`

Donut or pie chart showing completed run conclusions across the 30-day window:

- success
- failure
- cancelled
- other, if present

Include counts and percentages directly on the visualization.

## Phase 3: Upload Charts

Upload each generated PNG with `upload_asset` and capture the returned URLs.

## Phase 4: Publish The Discussion

Create exactly one discussion with this title pattern:

`[portfolio] Workflow AIC Portfolio - YYYY-MM-DD`

Use this structure:

### 📊 Executive Summary

- 30-day total AIC
- Active workflow count
- Approximate wasted AIC from failed/cancelled runs
- Whether spend is trending up or down in the last 7 days versus the prior 7 days
- Whether Sentry and Grafana both provided queryable numeric AIC, and whether local fallback artifacts were needed

### 🎯 Portfolio View

![Portfolio Spend Map](UPLOAD_URL_PORTFOLIO_MAP)

Write 2-4 sentences explaining what the portfolio map says about concentration of spend, reliability risk, and which workflows need attention first.

### 💸 Top Spenders

![Top Workflow Spenders](UPLOAD_URL_TOP_SPENDERS)

Include a top-10 table with columns:

| Workflow | Runs | Total AIC | Avg AIC | Failure Rate | 7d Delta |
|---|---:|---:|---:|---:|---:|

### 📈 Trend

![Daily Portfolio Trend](UPLOAD_URL_DAILY_TREND)

Summarize the overall direction, the biggest spike day, and whether the portfolio is stabilizing or getting noisier.

### ✅ Outcome Mix

![Outcome Mix](UPLOAD_URL_OUTCOME_MIX)

Briefly explain whether reliability is eroding spend efficiency.

### 🔭 Telemetry Coverage

- State whether Sentry AIC was queryable.
- State whether Grafana AIC was queryable.
- If either backend was incomplete, say whether local fallback artifacts were used.
- Call out any backend gap precisely, for example missing numeric `gh-aw.aic`, missing workflow attribution, or trace query limitations.

### 🔥 Heavy Hitters

- List workflows that qualify as heavy hitters.
- Explain why each one is expensive: high frequency, high AIC per run, or high waste.

### 🚨 Rapid Risers

- List workflows with the sharpest 7-day AIC growth.
- If none qualify, say so explicitly.

### Recommendations

- Provide 3-5 concrete actions.
- Prioritize the workflows with the largest spend concentration or fastest growth.
- When a workflow has both high spend and high failure rate, call that out as the highest-priority optimization target.

## MCP Query Loop

Follow this order:

1. Validate Sentry connectivity and discover the correct org/project.
2. Query Sentry spans first for the last 30 days with AIC fields.
3. Validate Grafana datasource availability and inspect Tempo attribute names.
4. Query Grafana traces for the same 30-day period to verify attribute coverage.
5. Build the normalized dataset from backend data.
6. Use local fallback artifacts only for missing AIC/workflow/conclusion coverage.

## Guardrails

- Treat Sentry as the canonical source for total AIC when it has numeric AIC data.
- Treat Grafana as corroboration and backend-gap detection unless it clearly exposes equivalent numeric AIC.
- Never claim Grafana usage is zero when the real state is "not queryable".
- Never invent workflow attribution when neither OTEL resource attributes nor local fallback metadata provide it.
- If the backend data and fallback data disagree materially, say so explicitly in the discussion.

### References

- Include up to 5 run references using the latest runs from the highest-spend workflows.

## Final Guardrails

- Never invent AIC numbers.
- If the window has sparse data, say so directly.
- Keep the discussion concise and chart-led rather than text-led.