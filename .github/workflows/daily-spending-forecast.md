---
name: Daily Spending Forecast
description: Forecasts agentic workflow spending, reviews data quality, and publishes a daily report with charts
emoji: "📈"
on:
  schedule: daily around 9:00
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  copilot-requests: write
engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
strict: true
imports:
  - shared/trending-charts-simple.md
tools:
  github:
    mode: local
  agentic-workflows: true
steps:
  - name: Build gh-aw from source
    run: |
      set -euo pipefail
      make build
      "$GITHUB_WORKSPACE/gh-aw" --version
  - name: Prefetch forecast usage artifacts
    continue-on-error: true
    env:
      REPOSITORY: ${{ github.repository }}
      GH_TOKEN: ${{ github.token }}
    run: |
      # Download usage artifacts for the last 30 days in parallel so the main
      # forecast step reads from the local cache and produces output quickly.
      DEBUG='*' "$GITHUB_WORKSPACE/gh-aw" forecast \
        --repo "$REPOSITORY" \
        --days 30 \
        --sample 100 \
        --concurrency 8 \
        --timeout 25 \
        --verbose \
        > /dev/null 2>&1 || true
  - name: Run spending forecast
    id: spending_forecast
    continue-on-error: true
    env:
      REPOSITORY: ${{ github.repository }}
      GH_TOKEN: ${{ github.token }}
    run: |
      set -uo pipefail
      output_dir="/tmp/gh-aw/agent/spending-forecast"
      mkdir -p "$output_dir"

      set +e
      DEBUG='*' "$GITHUB_WORKSPACE/gh-aw" forecast \
        --repo "$REPOSITORY" \
        --days 30 \
        --period month \
        --sample 100 \
        --concurrency 8 \
        --timeout 10 \
        --verbose \
        --json \
        > >(tee "$output_dir/forecast.json")
      exit_code=$?
      wait
      set -e

      {
        printf 'exit_code=%s\n' "$exit_code"
        printf 'repository=%s\n' "$REPOSITORY"
        printf 'generated_at=%s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
      } > "$output_dir/forecast-metadata.txt"
post-steps:
  - name: Upload spending forecast report
    if: always()
    uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
    with:
      name: spending-forecast-${{ github.run_id }}
      path: /tmp/gh-aw/agent/spending-forecast/
      retention-days: 30
      if-no-files-found: warn
safe-outputs:
  create-issue:
    title-prefix: "[spending-forecast] "
    labels: [agentic-workflows]
    close-older-issues: true
    expires: 7d
    max: 1
  mentions: false
  allowed-github-references: []
timeout-minutes: 45
sandbox:
  agent:
    runtime: cloud-hypervisor
evals:
  - id: spending_forecast_analyzed
    question: Did the agent analyze the agentic workflow spending forecast and its data quality?
  - id: forecast_report_created
    question: Did the agent create a report with spending projections and supporting evidence?
features:
  gh-aw-detection: true
---

# Daily Spending Forecast

Analyze the prepared `gh aw forecast` output and publish a daily spending forecast for
`${{ github.repository }}`.

The structured initial command output is in
`/tmp/gh-aw/agent/spending-forecast/`:

- `forecast.json` — machine-readable forecast output
- `forecast-metadata.txt` — command exit code and collection context

## Analysis

1. Validate that `forecast.json` parses and reconcile its workflow totals, sampled run
   counts, history windows, run samples, success rates, and P10/P50/P90 projections.
2. Review the data for inaccuracies. Flag zero or missing AIC values, sparse or stale
   samples, outliers, inconsistent date windows, implausible run frequencies, missing
   workflows, and confidence intervals that are too broad to support a reliable budget.
   Do not invent missing values.
3. If the prepared output is incomplete or suspicious, rerun
   `$GITHUB_WORKSPACE/gh-aw forecast` with targeted arguments (including `--eval` when
   backtesting would clarify accuracy). If sample collection still fails — especially
   when `sampled_runs` is zero for all workflows or artifact downloads fail — you MUST
   use the `agentic-workflows` MCP server to inspect recent runs and usage artifacts
   and derive observed/projected AIC directly from that evidence.
   Preserve any additional structured output in the spending forecast directory so it
   is included in the artifact, but do not persist process stdout or stderr logs. Limit
   follow-up to the evidence needed to resolve or document the discrepancy.
4. Calculate historical spending from `run_samples[].aic` and clearly distinguish
   observed spending from projected spending.

## Terminology

Use these definitions consistently throughout the report. Include the parenthetical
explanation each time a percentile term first appears in a section so readers never
need to guess:

- **P10** (10th percentile — optimistic scenario: 9 out of 10 months will cost *at
  least* this much)
- **P50** (50th percentile — median/expected scenario: equal probability of spending
  more or less than this)
- **P90** (90th percentile — conservative scenario: only 1 out of 10 months is expected
  to exceed this amount)

## Charts

Generate two PNG charts, save them to `/tmp/gh-aw/python/charts/`, upload each with
the `upload_asset` safe-output tool, and embed them in the report using the returned
asset URLs. Run both scripts with `/tmp/gh-aw/python/venv/bin/python` — the `python3` on
`PATH` inside the sandbox is PyPy and cannot import Pandas/Matplotlib.

**Chart 1 — Spending Trend** (`spending_trend.png`):
Line chart showing per-run AIC over the 30-day sample window, one series per
workflow (top 5 by total AIC; group the rest into "Other"). X-axis: date. Y-axis: AIC
per run. Include a 7-day rolling-average overlay. Title: "Spending Trend — Last 30 Days".

```python
#!/tmp/gh-aw/python/venv/bin/python
import json, os, sys
import pandas as pd
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import seaborn as sns
from datetime import datetime, timedelta

data_file = "/tmp/gh-aw/python/data/run_samples.json"
chart_file = "/tmp/gh-aw/python/charts/spending_trend.png"

# Load data written from forecast.json (run_samples list)
with open(data_file) as f:
    samples = json.load(f)

df = pd.DataFrame(samples)
df["run_started_at"] = pd.to_datetime(df["run_started_at"])
df["date"] = df["run_started_at"].dt.date

top5 = df.groupby("workflow_id")["aic"].sum().nlargest(5).index
df["series"] = df["workflow_id"].where(df["workflow_id"].isin(top5), "Other")

sns.set_style("whitegrid")
fig, ax = plt.subplots(figsize=(12, 6), dpi=300)
palette = sns.color_palette("husl", n_colors=len(df["series"].unique()))
for idx, (name, grp) in enumerate(df.groupby("series")):
    daily = grp.groupby("date")["aic"].mean()
    ax.plot(daily.index, daily.values, marker="o", linewidth=1.5,
            label=name, color=palette[idx], alpha=0.8)
    rolling = daily.rolling(7, min_periods=1).mean()
    ax.plot(rolling.index, rolling.values, linewidth=2.5, linestyle="--",
            color=palette[idx], alpha=0.5)

ax.set_title("Spending Trend — Last 30 Days", fontsize=14, fontweight="bold")
ax.set_xlabel("Date")
ax.set_ylabel("AIC per Run")
ax.legend(fontsize=9, loc="upper left")
plt.xticks(rotation=45)
plt.tight_layout()
plt.savefig(chart_file, dpi=300, bbox_inches="tight", facecolor="white")
print(f"Chart saved: {chart_file}")
```

**Chart 2 — Forecast Distribution** (`forecast_distribution.png`):
Horizontal bar chart showing weekly P10 / P50 / P90 (10th percentile — optimistic /
50th percentile — median / 90th percentile — conservative) for the top workflows by
projected spend. Use green for P10, blue for P50, and red for P90. Title: "Weekly
Forecast Distribution (P10 optimistic / P50 median / P90 conservative)".

```python
#!/tmp/gh-aw/python/venv/bin/python
import json, os
import pandas as pd
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import seaborn as sns

forecast_file = "/tmp/gh-aw/python/data/forecast.json"
chart_file = "/tmp/gh-aw/python/charts/forecast_distribution.png"

with open(forecast_file) as f:
    forecast = json.load(f)

rows = []
for wf in forecast.get("workflows", []):
    proj = wf.get("projection", {})
    rows.append({
        "workflow": wf.get("workflow_id", "unknown")[-30:],
        "p10": proj.get("weekly_aic_p10", 0),
        "p50": proj.get("weekly_aic_p50", 0),
        "p90": proj.get("weekly_aic_p90", 0),
    })

df = pd.DataFrame(rows).sort_values("p50", ascending=True).tail(10)

sns.set_style("whitegrid")
fig, ax = plt.subplots(figsize=(12, max(4, len(df) * 0.6)), dpi=300)
y = range(len(df))
ax.barh(y, df["p90"], color="#d73027", alpha=0.6,
        label="P90 (conservative — 90th percentile)")
ax.barh(y, df["p50"], color="#4575b4", alpha=0.8,
        label="P50 (median — 50th percentile)")
ax.barh(y, df["p10"], color="#1a9850", alpha=0.9,
        label="P10 (optimistic — 10th percentile)")
ax.set_yticks(list(y))
ax.set_yticklabels(df["workflow"].tolist(), fontsize=9)
ax.set_xlabel("Projected Weekly AIC")
ax.set_title(
    "Weekly Forecast Distribution\n(P10 optimistic / P50 median / P90 conservative)",
    fontsize=13, fontweight="bold"
)
ax.legend(fontsize=9)
plt.tight_layout()
plt.savefig(chart_file, dpi=300, bbox_inches="tight", facecolor="white")
print(f"Chart saved: {chart_file}")
```

Before running these scripts, write the necessary JSON data files:
- Extract `run_samples` from `forecast.json` and save to `/tmp/gh-aw/python/data/run_samples.json`
- Copy or symlink `forecast.json` to `/tmp/gh-aw/python/data/forecast.json`

After generating charts, upload each with `upload_asset` and store the returned URLs for
embedding in the report.

## Report

Write the final GitHub-flavored markdown report to
`/tmp/gh-aw/agent/spending-forecast/report.md`, then create one issue titled
`Daily spending forecast - YYYY-MM-DD` with the same report body.

Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body. Include:

- a concise executive summary with total observed AIC and weekly/monthly
  P10 (10th percentile — optimistic), P50 (50th percentile — median), and
  P90 (90th percentile — conservative) forecast totals;
- the two rendered chart images embedded inline using the asset URLs from the
  `upload_asset` calls above — include a descriptive alt-text for each;
- a workflow table showing sample count, observed AIC, P50/P90 per-run AIC, projected
  weekly/monthly AIC, success rate, and confidence range;
- a visible data-quality and accuracy section that explains every detected discrepancy,
  the likely forecast impact, and whether follow-up evidence resolved it;
- assumptions, forecast date, 30-day history window, and a link to
  `[§${{ github.run_id }}](https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }})`.

If chart generation fails, fall back to compact ASCII charts in fenced code blocks (each
under 80 columns, spaces not tabs) so the report always contains a visual summary.

Wrap verbose per-workflow evidence in `<details><summary><b>...</b></summary>...</details>`. If the initial
forecast failed and follow-up cannot recover it, still create an operational report from
the captured diagnostics rather than presenting fabricated projections.

Structure reports as: overview → charts → key metrics/issues → collapsible detail → next actions.