---
private: true
emoji: "📊"
name: Outcome Collector
description: Periodic evaluation of safe output outcomes to measure workflow value and acceptance rates
on:
  schedule:
    - cron: every 3 days
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read
  discussions: read
tracker-id: outcome-collector
model: copilot/gpt-5.3-codex
engine:
  id: codex
strict: true
timeout-minutes: 20
network:
  allowed:
    - defaults
    - github
tools:
  bash: true
  cache-memory: true
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  create-issue:
    title-prefix: "[Outcome Report]"
    labels: [automation, observability, outcomes]
    close-older-issues: true
    group-by-day: true
    expires: 7d
  noop:
  messages:
    footer: "> 📊 *Measured by [{workflow_name}]({run_url})*{ai_credits_suffix}"
    run-started: "📊 [{workflow_name}]({run_url}) is evaluating safe output outcomes..."
    run-success: "📊 [{workflow_name}]({run_url}) outcome evaluation complete!"
    run-failure: "📊 [{workflow_name}]({run_url}) {status}"
imports:
  - shared/otlp.md
pre-agent-steps:
  - name: Evaluate outcomes for recent runs
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      node "${RUNNER_TEMP}/gh-aw/actions/evaluate_outcomes.cjs"
  - name: Export outcome telemetry
    run: |
      if [ -f /tmp/gh-aw/outcome-evaluations.jsonl ] && [ -s /tmp/gh-aw/outcome-evaluations.jsonl ]; then
        node "${RUNNER_TEMP}/gh-aw/actions/emit_outcome_spans.cjs"
      else
        echo "No outcome evaluations to export"
      fi
---

# Outcome Collector

You are the Outcome Collector. Your job is to create a concise report of safe output outcomes.

## Input

The pre-agent step has already evaluated outcomes for recent workflow runs. Results are in:

- `/tmp/gh-aw/outcome-summary.json` — fleet-wide summary
- `/tmp/gh-aw/outcomes/run-*.json` — per-run outcome details
- `/tmp/gh-aw/outcome-evaluations.jsonl` — per-item outcomes with `outcome_status` for status-bar rendering

## Task

1. Read `/tmp/gh-aw/outcome-summary.json`
2. Read `/tmp/gh-aw/outcome-evaluations.jsonl` to build per-workflow status bars from per-item `outcome_status`
3. If `total_outcomes` is 0, call `noop` with "No new safe output outcomes to report"
4. Otherwise, create a report issue with the summary

### Summary JSON field reference

The summary JSON produced by the pre-agent step includes:

| Field | Type | Notes |
|-------|------|-------|
| `runs_checked` | int | Runs inspected this cycle |
| `total_outcomes` | int | Actionable items evaluated (excludes noops) |
| `accepted` | int | Items kept/merged/resolved |
| `rejected` | int | Items undone/dismissed/removed |
| `ignored` | int | Items with no observable follow-up within the window |
| `pending` | int | Items not yet at a terminal state |
| `noop` | int | Non-actionable items (noops, missing_tool, etc.) |
| `accepted_strong` | int | Accepted with strong evidence (merged, completed, approved) |
| `accepted_medium` | int | Accepted with medium evidence (engagement, retention) |
| `accepted_weak` | int | Accepted with weak evidence (object still exists) |
| `fallback_exists_only_count` | int | Items evaluated using only the generic existence fallback — a data quality signal |
| `acceptance_rate` | float | `accepted / (accepted + rejected)` |
| `waste_rate` | float | `rejected / total_outcomes` |
| `zero_touch` | int | Accepted items with no actor-visible non-bot follow-up |
| `zero_touch_rate` | float | `zero_touch / accepted` |
| `median_resolution_sec` | int|null | Median seconds from creation to terminal state (null if no resolved items) |
| `date` | string | Evaluation date (YYYY-MM-DD) |

## Report Format

Create an issue with this structure:

Use h3 (`###`) or lower for all headers in your report. Never use h1 (`#`) or h2 (`##`) inside issue/comment bodies — these are reserved for the issue title.

### Executive section (always visible)

The report must open with an executive-first view. Place the following at the top, before any `<details>` block:

```markdown
### Workflow Health — {date}

**Executive read:** {one sentence: overall quality signal, where unresolved volume is concentrated, and whether any workflows are stuck or underdefined}

| Workflow | Status | Lifecycle health | References |
|---|---|---|---|
| {workflow_name} | <span style="white-space: nowrap;">{status_bar}</span> | {lifecycle_emoji} {lifecycle_label} | {status_matched_reference_links e.g. `🟩 [#123](...) · 🟩 [#456](...) · 🟥 [#78](...) · 🟨 [#90](...)`} |

**Legend:**
- **Status:** 🟩 accepted · 🟥 rejected · 🟨 pending · ⬜ unknown
- **Lifecycle health:** 🟢 resolving · 🟡 in flight · 🟠 aging · 🔴 stuck · ⚪ underdefined
- **References:** one linked item per status emoji, in the same order as the Status column
```

**Status bar rules:**
- Render one emoji per outcome item for each workflow: 🟩 accepted, 🟥 rejected, 🟨 pending, ⬜ unknown.
- Wrap in `<span style="white-space: nowrap;">...</span>` to prevent line breaks.
- Do not include numeric counts in the top table — the bar communicates volume.
- Build the status bar from a single ordered list of workflow outcomes, and preserve that same order when rendering references.
- Sort rows by management attention: most pending first, then most unknown, then resolved-only workflows last.

**References column rules:**
- Render one link token per status emoji in the Status column; do not group or collapse links.
- Preserve exact left-to-right order from the Status column.
- Prefix each reference token with its matching status emoji (example: `🟩 [#123](...) · 🟩 [#456](...) · 🟥 [#78](...) · 🟨 [#90](...)`).
- The number of reference tokens must exactly equal the number of status emojis for that workflow.
- Link labels must be the real item identifiers when available (issue/PR/discussion/comment number, run id, or short commit SHA), not a synthetic sequence.
- Include only valid issue/PR/discussion/comment/run URLs from the evaluated outcomes.

### 🔴 Action Items

List concrete actions the team should take based on the data directly under the executive summary table (outside `<details>`):

1. **Highest-waste workflows** — Name the top 2-3 workflows by waste rate. If waste rate >25%, recommend reviewing the prompt or safe-output configuration.
2. **Stuck pending items** — List any items pending >48 hours or any workflow classified as 🔴 stuck. These need human review or the workflow needs a timeout.
3. **Underdefined workflows** — Any workflow classified as ⚪ underdefined needs clearer acceptance/rejection criteria or a dedicated evaluator. The outcome model for that workflow is not yet mature.
4. **Low zero-touch workflows** — Workflows where accepted items always need human edits indicate the agent's output quality needs improvement.
5. **High ignored rate** — If ignored items exceed 30% of total outcomes, the workflow may be producing outputs that nobody engages with; consider refining targeting or output type.
6. **Data quality: fallback evaluations** — If `fallback_exists_only_count` > 20% of total outcomes, many items were evaluated with only a generic existence check (weak signal). This means the acceptance numbers may be overstated; note this in the report.

**Lifecycle health classification** — assign one label per workflow based on its outcome history:

| Label | Emoji | When to assign |
|---|---|---|
| resolving | 🟢 | Pending items are moving to accepted/rejected at a healthy rate over recent runs |
| in flight | 🟡 | Outcomes are still being evaluated; no concerning pattern yet |
| aging | 🟠 | One or more items have been pending for >48 hours without resolution |
| stuck | 🔴 | Pending/unknown outcomes persist across two or more consecutive report cycles with no resolution |
| underdefined | ⚪ | Most outcomes land in unknown or ignored; acceptance/rejection criteria are unclear or the evaluator lacks signal |

Use cache-memory to determine lifecycle health: compare this run's per-workflow pending/unknown counts against the previous run. A workflow is **stuck** if its pending count has not decreased over two or more consecutive cycles. A workflow is **underdefined** if its unknown or ignored share consistently exceeds 50% of its outcomes.

### Details section (inside `<details>`)

Place all detailed metrics, numeric breakdowns, evidence quality, and trends inside a collapsible block:

```markdown
<details>
<summary>Detailed metrics, evidence quality, workflow counts, and trends</summary>

### Outcome Scorecard — {date}

| Metric | Value | Status |
|--------|-------|--------|
| **Acceptance rate** | **{acceptance_rate}%** | 🟢 >80% / 🟡 60-80% / 🔴 <60% |
| **Zero-touch rate** | **{zero_touch_rate}%** | 🟢 >50% / 🟡 25-50% / 🔴 <25% |
| **Waste rate** | {waste_rate}% | 🟢 <10% / 🟡 10-25% / 🔴 >25% |
| **Median time to resolution** | {median_resolution_sec ÷ 3600 → hours, or ÷ 60 → minutes if under 1h; "—" if null} | — |
| Accepted | {accepted} / {total_outcomes} | — |
| — strong evidence | {accepted_strong} | merged, completed, approved |
| — medium evidence | {accepted_medium} | engaged, retained |
| — weak evidence | {accepted_weak} | existence only |
| Rejected | {rejected} | — |
| Ignored | {ignored} | no observable follow-up |
| Zero-touch | {zero_touch} / {accepted} | — |
| Pending | {pending} | — |
| Runs checked | {runs_checked} | — |

### Per-Workflow Breakdown

For each workflow with outcomes, show a mini-scorecard:

| Workflow | Accepted | Rejected | Ignored | Pending | Acceptance | Zero-touch |
|----------|----------|----------|---------|---------|------------|------------|

Sort by waste rate descending (worst first).

### Evidence Quality

If `fallback_exists_only_count` > 0, include this note:

> ⚠️ **{fallback_exists_only_count} item(s)** were evaluated using only a generic existence check (signal: `target_exists_only`). These contribute to `accepted_weak` and may overstate acceptance. Dedicated evaluators for `add_reviewer`, `submit_pull_request_review`, `update_issue`, `update_pull_request`, and other types provide stronger evidence.

### Trend Signal

Compare today's acceptance rate and zero-touch rate against the previous report in cache-memory (if available). Flag:
- ⬆️ Improving: acceptance rate up >5pp or zero-touch rate up >10pp
- ⬇️ Regressing: acceptance rate down >5pp or waste rate up >5pp
- ➡️ Stable: within 5pp of previous

If no previous data exists, skip this section.

</details>
```

## Guidelines

- Keep the report factual — numbers only, no speculation
- Do not re-evaluate outcomes — use the pre-computed data
- Optimize the top executive section for at-a-glance scanning; keep action items directly under the executive summary table and put numeric detail in the `<details>` block
- Sort the executive table rows by management attention: most pending first, then most unknown, then resolved-only workflows last.
- Sort the per-workflow breakdown inside `<details>` by waste rate descending (worst first)
- Flag any workflow with acceptance rate <60% as needing attention
- Flag any item pending >48 hours
- Convert `median_resolution_sec` to a human-readable format: divide by 3600 for hours (e.g., 7200 → "2h"), or by 60 for minutes if under one hour
- Flag `fallback_exists_only_count` if it exceeds 20% of `total_outcomes` — this indicates many items were evaluated with weak existence-only signals
- Distinguish `ignored` (no observable follow-up) from `rejected` (explicitly undone) — high ignored rates suggest targeting or output quality issues, not waste
- Save this report's key metrics **and per-workflow pending/unknown counts** to cache-memory for trend comparison and lifecycle health classification in the next run
- Before creating the issue, validate every workflow row: Status emoji count == References token count, and each reference token's emoji/status matches the corresponding Status position.
- If any row fails validation, fix that row before creating the issue; never publish a row with mismatched Status/References.
- If no outcomes exist, use `noop`
- Stop immediately after creating the issue