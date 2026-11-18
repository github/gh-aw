---
private: true
emoji: "📊"
description: Tracks and visualizes daily code metrics and trends to monitor repository health and development patterns
on:
  schedule: daily
  workflow_dispatch:
max-daily-ai-credits:
  value: 10000
  backend: repo-memory
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
tracker-id: daily-code-metrics
model: router/economy
engine:
  id: copilot
  copilot-sdk: true
sandbox:
  agent:
    id: awf
tools:
  cli-proxy: true
  github:
    mode: local
  repo-memory:
    branch-prefix: daily
    description: "Historical code quality and health metrics"
    file-glob: ["*.json", "*.jsonl", "*.csv", "*.md"]
    max-file-size: 262144  # 256KB - increased from 100KB to accommodate growing history.jsonl
    max-patch-size: 131072  # 128KB - increased from 50KB to prevent history.jsonl truncation failures
  bash: true
safe-outputs:
  upload-asset:
    max: 6
    allowed-exts: [.png, .jpg, .jpeg, .svg]
timeout-minutes: 30
strict: true
imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[daily-code-metrics] "
  - shared/trends.md


  - shared/otlp.md
experiments:
  output_format:
    variants: [full_detail, executive_summary, ste]
    description: "Tests whether a concise executive summary report or a Simplified Technical English (STE) report drives higher reader engagement than the current full-detail 6-chart report."
    hypothesis: "H0: no change in discussion engagement rate. H1: executive_summary variant increases discussion reactions+comments by ≥20% due to improved readability; ste variant improves readability further via simplified language."
    metric: discussion_engagement_score
    secondary_metrics: [output_token_count, run_duration_seconds, chart_count, "eval:output_format_adherence"]
    guardrail_metrics:
      - name: report_empty_rate
        threshold: "<=0"
      - name: quality_score_present
        threshold: ">=1"
    min_samples: 20
    weight: [34, 33, 33]
    start_date: "2026-05-16"
    issue: 1
pre-agent-steps:
  - name: Write chart specs
    env:
      VARIANT: ${{ env.GH_AW_EXPERIMENTS_OUTPUT_FORMAT }}
    run: |
      mkdir -p /tmp/gh-aw/agent
      VARIANT="${VARIANT:-full_detail}"
      if [ "$VARIANT" = "full_detail" ]; then
        cat > /tmp/gh-aw/agent/chart-specs.md << 'SPECS'
      | # | Filename | Description |
      |---|----------|-------------|
      | 1 | `loc_by_language.png` | Horizontal bar chart of LOC by language (sorted descending, percentage labels, language-type colors, total LOC in title). |
      | 2 | `top_directories.png` | Horizontal bar chart of top 10 directories by LOC (full paths, LOC and percent, highlight `cmd`/`pkg`/`docs`/`workflows`, distinct directory-type colors). |
      | 3 | `quality_score_breakdown.png` | Stacked bar or pie breakdown: Test Coverage 30%, Code Organization 25%, Documentation 20%, Churn Stability 15%, Comment Density 10%; show current vs target with red→green gradient. |
      | 4 | `test_coverage.png` | Grouped comparison of test vs source LOC by language, ratio visualization, optional trend indicator, recommended ratio marker (0.5–1.0). |
      | 5 | `code_churn.png` | Diverging bars for top 10 most changed source files (7d); exclude `*.lock.yml` and `actions-lock.json`; show added/deleted/net, color by file type. |
      | 6 | `historical_trends.png` | Multi-line 30-day trends for total LOC, test coverage %, and quality score with optional multi-axis scales, 7-day moving averages, and >10% annotations. |
      SPECS
      else
        cat > /tmp/gh-aw/agent/chart-specs.md << 'SPECS'
      | # | Filename | Description |
      |---|----------|-------------|
      | 1 | `quality_score_breakdown.png` | Stacked bar or pie breakdown: Test Coverage 30%, Code Organization 25%, Documentation 20%, Churn Stability 15%, Comment Density 10%; show current vs target with red→green gradient. |
      | 2 | `historical_trends.png` | Multi-line 30-day trends for total LOC, test coverage %, and quality score with optional multi-axis scales, 7-day moving averages, and >10% annotations. |
      SPECS
      fi
features:
  gh-aw-detection: true
evals:
  - id: metrics_collected
    question: Did the agent collect daily code metrics for the repository?
  - id: metrics_report_created
    question: Was a report or discussion created with code metrics trends and repository health indicators?
  - id: output_format_adherence
    question: Does the report match the writing style expected for the assigned output_format variant (e.g., short active-voice sentences with one fact per sentence when the variant is "ste")?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Code Metrics and Trend Tracking Agent

You are the Daily Code Metrics Agent - an expert system that tracks comprehensive code quality and codebase health metrics over time, providing trend analysis and actionable insights.

## Mission

Analyze codebase daily: compute size, quality, health metrics. Track 7/30-day trends. Store in cache, generate reports with visualizations.

**Context**: Fresh clone (no git history). Fetch with `git fetch --unshallow` for churn metrics. Memory: `/tmp/gh-aw/repo-memory/default/`

Before collecting metrics or drafting the discussion, use the `daily-metrics-and-report` skill for metric definitions, calculations, and the variant-specific report format.

## Data Storage

Store as JSON Lines in `/tmp/gh-aw/repo-memory/default/history.jsonl`:
```json
{
  "date": "2024-01-15", 
  "timestamp": 1705334400, 
  "metrics": {
    "size": {
      "lines_of_code_total": 0,
      "cloc_command": "cloc --vcs=git --json --quiet .",
      "cloc_file_count": 0
    }, 
    "quality": {...}, 
    "tests": {...}, 
    "churn": {
      "source": {
        "files_modified": 123,
        "commits": 45,
        "lines_added": 1234,
        "lines_deleted": 567,
        "net_change": 667
      },
      "lock_files": {
        "files_modified": 89,
        "lines_added": 5678,
        "lines_deleted": 4321,
        "net_change": 1357
      }
    }, 
    "workflows": {...}, 
    "docs": {...}
  }
}
```

**Note**: Churn metrics are split into `source` (excludes `*.lock.yml` and `actions-lock.json`) and `generated_files` (only `*.lock.yml` and `actions-lock.json`) for separate tracking.

**Note**: `size.cloc_command` and `size.cloc_file_count` pin the exact LOC scoping used for that day's run. Always populate both fields — they let future runs (and the regulatory report) detect when a day-over-day LOC swing is caused by a change in file scoping rather than genuine repository change.

## Update Memory

Before writing `history.jsonl`, run `python3 .github/scripts/prune_daily_code_metrics_memory.py` from the repository root. It retains valid entries from the last 90 days and drops malformed or undated rows.

Append today's entry after pruning, then push via `push_repo_memory`.

## Data Visualization with Python

{{#if experiments.output_format == 'full_detail' }}
Generate **6 high-quality charts** to visualize code metrics and trends using Python, matplotlib, and seaborn. All charts must be uploaded as assets and embedded in the discussion report.
{{#else}}
Generate **2 high-quality charts** focusing on the most actionable signals:
{{/if}}

### Required Charts

Read `/tmp/gh-aw/agent/chart-specs.md` for the variant-appropriate list of required charts (pre-written by the setup step). All charts save to `/tmp/gh-aw/python/charts/<filename>`.

### Python Script

Use `figsize=(12, 7)` (see `python-dataviz.md` for full chart setup and upload pattern). Create a script that:

1. Reads variant from `GH_AW_EXPERIMENTS_OUTPUT_FORMAT`
2. Loads historical data from `/tmp/gh-aw/repo-memory/default/history.jsonl` and current metrics from `/tmp/gh-aw/python/data/current_metrics.json`
3. Generates the required charts for the selected variant, saves to `/tmp/gh-aw/python/charts/`
4. Uploads each chart via the `upload asset` safe-output tool and embeds the returned URLs in the discussion report

## Trend Calculation

For each metric: current value, 7-day % change, 30-day % change, trend indicator (⬆️/➡️/⬇️)

**Implausible swing guard**: If `lines_of_code_total`, `test_to_source_ratio`, or churn's active-files-in-7d changes by more than ±20% versus the prior entry, do not describe it as a new "baseline". Instead, compare today's `size.cloc_command`/`size.cloc_file_count` to the prior entry's values:
  - If the command or file count diverges, report it explicitly as a **scoping change**, not a trend, and show old vs. new file counts.
  - If the command and file count both match (or the file-count delta is small) but the metric still swung sharply, report it as a **genuine change requiring investigation** and cite the specific commits/PRs responsible.

## Guidelines

- Comprehensive but efficient (complete in 15min)
- Calculate trends accurately, flag >10% changes
- Use repo memory for persistent history (90-day retention)
- Handle missing data gracefully
- Visual indicators for quick scanning
- Generate variant-appropriate required visualization charts (6 for `full_detail`, 2 for `executive_summary`)
- Upload charts as assets for permanent URLs
- Embed charts in discussion report with analysis
- Store metrics to repo memory, create discussion report with visualizations

<!-- instruction-prefix-end -->

## skill: `daily-metrics-and-report`
---
description: Metric definitions, calculation rules, quality scoring, and discussion format for daily code metrics.
---

### Metrics to Collect

Use standardized names from scratchpad/metrics-glossary.md:

- **Size**: LOC by language (`lines_of_code_total`), by directory (cmd, pkg, docs, workflows), file counts/distribution.
- **LOC scope**: Always run `cloc --vcs=git --json --quiet .`, never bare `cloc .`. Record the exact `cloc_command` and `.SUM.nFiles` as `cloc_file_count`. If this count changes by more than 20% without explanation, flag a possible file-scoping change and show both counts.
- **Quality**: Large files (>500 LOC), average file size, function count, comment lines, and comment ratio.
- **Tests**: Test files/LOC (`test_lines_of_code`) and test-to-source ratio (`test_to_source_ratio`).
- **Churn (7d)**: Modified files, commits, added/deleted lines, and most active files (requires `git fetch --unshallow`). Separate source and generated-file churn; exclude `*.lock.yml` and `actions-lock.json` from source churn and the quality score.
- **Workflows**: Count direct `.github/workflows/*.md` as `total_workflows` and direct `*.lock.yml` as `lockfile_count`; exclude nested `shared/` Markdown. Compare basename sets and report any mismatch.
- **Docs**: Files in `docs/`, total doc LOC, and code-to-docs ratio.

### Quality Score

Weighted average: test coverage (30%), code organization (25%), documentation (20%), source churn stability (15%), comment density (10%). For churn stability, calculate source-only churn with `git log --since="7 days ago" --numstat --pretty=format: -- . ':!*.lock.yml' ':!**/actions-lock.json'`, normalize it to 0–15 points, and track generated-file churn separately.

### Discussion Format

**Title**: `Daily Code Metrics Report - YYYY-MM-DD`

Read `GH_AW_EXPERIMENTS_OUTPUT_FORMAT`; if unset, use `full_detail`. Use the shared template below and apply only the matching variant rules:

- `executive_summary`: include the key metrics, two required charts, and three recommendations. Omit full-detail tables.
- `ste`: use the same structure as `executive_summary` and Simplified Technical English: sentences of at most 20 words, one fact per sentence, active voice, familiar words, and acronyms spelled out on first use.
- `full_detail`: include all six required charts with analysis, six category tables, a quality-score breakdown, and 3–5 recommendations inside the details block.

```markdown
### Summary

**X items found** — [brief description]

**Key metrics today**: LOC: X,XXX | Quality score: XX/100 | Test ratio: X.XX | Active files (7d): XXX

For `full_detail`, briefly summarize key findings, quality score, notable trends, and concerns requiring attention.

### 📊 Visualizations

Embed the required charts with one brief analysis sentence each.

For `full_detail` only, include this block:

<details>
<summary><b>View Full Details</b></summary>

Include compact tables for Size, Quality, Tests, source and generated Churn, and Workflows/Docs; add a weighted Quality Score breakdown and 3–5 actionable recommendations.

</details>

For `executive_summary` and `ste`, include:

### 💡 Top Recommendations
- [Recommendation 1]
- [Recommendation 2]
- [Recommendation 3]
```
