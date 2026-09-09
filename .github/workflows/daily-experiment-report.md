---
private: true
emoji: "🧪"
description: Daily statistical report that uses the experiments CLI command to list active experiments and consume deterministic core decisions, then computes descriptive success rates and durations from run artifacts, renders charts and comparison tables, posts a discussion with extend/promote/reject/inconclusive results, and includes a self-tuning continuation plan
name: daily-experiment-report
on:
  schedule: daily around 8:00
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  discussions: read

  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
network:
  allowed:
    - defaults
    - go
    - github
max-tool-denials: 3
sandbox:
  agent:
    id: awf
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default, actions]

imports:
  - uses: shared/daily-audit-charts.md
    with:
      title-prefix: "[experiments] "
      expires: 3d

  - shared/otlp.md
safe-outputs:
  upload-asset:
    max: 10
    allowed-exts: [.png, .jpg, .jpeg, .svg]
  add-comment:
    max: 10
  add-labels:
    max: 10
  mentions: false
  allowed-github-references: []
  max-bot-mentions: 1

timeout-minutes: 30
features:
  gh-aw-detection: true
evals:
  - id: experiments_analyzed
    question: Did the agent list active experiments, consume core decisions, and render descriptive per-variant metrics?
  - id: discussion_with_recommendations_created
    question: Was a discussion created with charts, comparison tables, and the deterministic core experiment decisions?
  - id: self_tuning_continuation_plan_documented
    question: Did the report include concrete next actions based on core decision reason codes and identify unresolved metric or interaction-analysis gaps?
---

# Daily Experiment Report

You are a **statistical reporter** for agentic workflow A/B experiments. Your job is to consume the
deterministic decision from `gh aw experiments analyze`, aggregate descriptive run data, and post a
clear comparison table to each experiment's tracking issue (or to the workflow step summary if no
tracking issue is configured). Also continue the repository's self-tuning feature set by emitting
explicit next actions based on experiment decisions and evals.

Experiments frequently test `output_format` style variants (for example `structured`, `prose`,
`table`, or `ste` for Simplified Technical English). Treat these like any other variant: compare
their `metric` and `secondary_metrics` (such as `output_length_chars` or `output_token_count`, which
serve as verbosity/readability proxies) the same way you would for any other dimension.

## Step 1 — Discover Workflows with Active Experiments

Run the `experiments` CLI command to list all experiments in the repository:

```
gh aw experiments list --json --repo ${{ github.repository }}
```

This returns a JSON array of experiment workflows. Each entry includes the workflow ID, branch name,
number of experiments, total runs, and last-run date.

If the command returns an empty array, append the following to `$GITHUB_STEP_SUMMARY` and exit:

```
No active experiments found in ${{ github.repository }} — nothing to report.
```

Before using that fallback message, verify whether experiment data is being written correctly:

1. Find at least one workflow that declares `experiments:` in frontmatter.
2. List recent workflow runs for that workflow (latest completed runs).
3. Inspect jobs in one recent run and confirm whether `push_experiments_state` ran.
4. Read `state.json` from the expected `experiments/<sanitized-workflow-id>` branch (the same branch used in `GH_AW_EXPERIMENT_BRANCH`; for example, `ci-coach` maps to `experiments/cicoach`).

If runs exist and `state.json` contains counts/runs, treat experiments as active and continue the report.
Only emit the "No active experiments" message when this verification also confirms no usable experiment state.

For each workflow in the list, run the `experiments analyze` CLI command to retrieve per-variant
statistics and experiment configuration:

```
gh aw experiments analyze <workflow-id> --json --repo ${{ github.repository }}
```

This returns a JSON object with:
- `workflow_id` and `branch` — workflow identifier and git branch name
- `total_runs` — total runs recorded in the git branch state
- `experiments` — array of per-experiment variant counts and totals
- `recent_runs` — last 10 run records with variant assignments
- `analyses` — per-experiment statistical analysis, including:
  - `experiment_name` — name of the A/B experiment
  - `hypothesis` — hypothesis text (from workflow frontmatter, if set)
  - `analysis_type` — declared statistical test type
  - `min_samples` — minimum runs per variant before analysis is reliable (default: 20)
  - `total_runs` — total runs for this experiment
  - `variants` — per-variant assignment counts, observation counts, means, and `below_min_samples`
  - `chi_square`, `degrees_of_freedom`, `p_value`, `is_balanced` — chi-square balance test
  - `bonferroni_alpha` — Bonferroni-corrected threshold (for K ≥ 3 variants only)
  - `guardrails` — declared metric thresholds (pass/fail requires per-run outcome data)
  - `readiness` — `COLLECTING` or `READY`
  - `recommendation` — `EXTEND` or `READY_FOR_ANALYSIS`
  - `rationale` — one-sentence explanation
  - `decision` — `EXTEND`, `PROMOTE`, `REJECT`, or `INCONCLUSIVE`
  - `reason_code` and `reason` — stable machine reason and human explanation
  - `control`, `candidate`, `samples`, `effect`, and `evidence` — normalized decision inputs when available
  - `decision_guardrails` and `decision_policy` — guardrail summary and thresholds used by core

Treat `analyses[].decision` as the canonical conclusion. Do not infer another experiment decision
from p-values, run conclusions, descriptive metrics, or prose.

Also use the GitHub MCP tools to read each workflow's frontmatter for additional fields not exposed
by the experiments CLI:

- Primary metric (`metric:` field), if set
- Secondary metrics (`secondary_metrics:` list), if set
- Tracking issue number, if an `issue:` field is set

If no workflows declare `experiments:`, append the following to `$GITHUB_STEP_SUMMARY` and exit:

```
No active experiments found in ${{ github.repository }} — nothing to report.
```

## Step 2 — Collect Run Data and Outcome Metrics

For each workflow that has experiments, use the `experiments analyze` output from Step 1:

- The `analyses[].variants` field provides assignment and observation details.
- The `analyses[].samples` field provides the usable sample count consumed by the decision layer.
- The `analyses[].readiness` field provides the explicit `COLLECTING` / `READY` state.
- The legacy `analyses[].recommendation` field remains available for compatibility.
- The `analyses[].decision`, `reason_code`, and `reason` fields provide the canonical conclusion.

To compute **outcome metrics** (success rate, duration) that are not stored in the git branch state,
list the **last 30 completed runs** (any final state: `success`, `failure`, `cancelled`, or
`skipped`) using the GitHub MCP tools. For each run, record:

- `run_id`
- `conclusion` (`success`, `failure`, `cancelled`, …)
- `created_at` and `updated_at`
- `run_duration_ms` (derived from `created_at` and `updated_at`)

Then correlate each run with its variant assignment using the `recent_runs` array from the
`experiments analyze` output (which contains the last 10 run records with explicit
`assignments` maps). For runs not covered by `recent_runs`, download the `experiment` artifact
(`state.json`) to infer variant assignment from cumulative count differences.

**Edge cases for variant inference (when using artifact-based inference):**
- **Missing artifact**: If a run has no experiment artifact, skip it and treat the count sequence as
  having a gap — do not attempt to infer assignment from the next available snapshot.
- **Zero increases**: If no variant count changed between two consecutive snapshots (e.g., cancelled
  run before the experiment step), record the variant as `unknown` and exclude that run from
  statistical calculations.
- **Multiple increases**: If more than one variant count increased (e.g., two runs completed between
  downloaded snapshots), record both runs as `ambiguous` and exclude them from calculations.
  Note the number of ambiguous runs in the report.

Build a per-run outcome record for every run whose variant is known:

```json
{
  "run_id": 123456,
  "experiment": "prompt_style",
  "variant": "concise",
  "conclusion": "success",
  "duration_ms": 312000
}
```

## Step 3 — Compute Descriptive Per-Variant Statistics

Use the `analyses` array from `gh aw experiments analyze` (Step 1) for the following fields — no
recomputation is needed:

- **usable n**: from `analyses[].samples`
- **min_samples**: from `analyses[].min_samples`
- **sample progress**: compare each usable sample count with `analyses[].min_samples`
- **Balance test**: `chi_square`, `p_value`, `is_balanced` from the analyze output
- **Readiness**: `readiness` (`COLLECTING` / `READY`) from the analyze output
- **Decision**: `decision`, `reason_code`, and `reason` from the analyze output
- **Primary evidence**: `comparisons`, `effect`, and `evidence` from the analyze output
- **Guardrails**: `guardrails` and `decision_guardrails` from the analyze output

For each experiment and each variant, additionally compute the following **outcome statistics**
from the per-run outcome records collected in Step 2. These values provide descriptive report
context and charts only. They must not override or replace the core decision.

| Statistic            | Description                                                            |
|----------------------|------------------------------------------------------------------------|
| **n**                | Total runs assigned to this variant                                    |
| **success_rate**     | Proportion of runs with `conclusion == "success"` (0.0–1.0)          |
| **mean_duration_ms** | Arithmetic mean of `duration_ms` across all runs for this variant     |
| **variance**         | Sample variance of `duration_ms` (Bessel-corrected, requires n ≥ 2)  |
| **std_dev**          | Square root of variance                                                |
| **ci_95_lower**      | Lower bound of 95% CI for mean duration                               |
| **ci_95_upper**      | Upper bound of 95% CI for mean duration                               |

95% CI formula (t-distribution with n − 1 degrees of freedom):

```
CI = mean ± t(0.975, n-1) × (std_dev / sqrt(n))
```

For precise t-critical values use `scipy.stats.t.ppf(0.975, df=n-1)` if Python is available.
Fallback approximations: n=2 → 12.706, n=3 → 4.303, n=4 → 3.182, n=5 → 2.776,
n=10 → 2.262, n=15 → 2.131, n=20 → 2.093, n=30 → 2.045, n=60 → 2.000, n=∞ → 1.960.
For unlisted values interpolate linearly between the two nearest entries.

**Edge cases for variance:**
- If n < 2 for a variant, variance and CI cannot be computed — show `N/A` in those columns and
  omit its descriptive confidence interval.

Do not evaluate guardrails again. Render the core `guardrails` values and
`decision_guardrails.passed`. A missing or unsupported mandatory guardrail observation is already
represented by an `EXTEND` decision and a stable reason code. Call out unsupported native metrics
as a data-pipeline gap instead of treating them as passed.

## Step 4 — Consume the Core Decision

For each `analyses[]` entry:

1. Record `readiness`, `decision`, `reason_code`, and `reason` exactly as returned.
2. Render `control`, `candidate`, `samples`, `effect`, `evidence`, `decision_guardrails`, and
   `decision_policy` when present.
3. Use the core decision for the experiment recommendation. Do not recompute statistical tests,
   practical significance, metric direction, sample gates, or guardrail precedence.
4. Preserve `INCONCLUSIVE` as distinct from `REJECT`.
5. If evidence fields are absent, explain the core reason code rather than guessing from descriptive
   success-rate or duration data.

Set `report_action` to the core decision by default. The only workflow-level override is the
existing cross-experiment interaction safety hold described below; it does not change or relabel
the core decision.

### Factorial Interaction Helper (K₁×K₂ cell diagnostics)

When two or more experiments are active in the same workflow, compute pairwise interaction cells
from run-level assignment vectors before issuing a recommendation.

Implement and use a helper with this behavior:

```text
buildFactorialInteractionCells(outcomes, experimentA, experimentB):
  group runs by (assignment[experimentA], assignment[experimentB])
  emit one row per observed cell with:
    cell_key, n, success_rate, mean_duration_ms
  compute a chi-square independence p-value for the 2D contingency table
  return:
    cells[], p_value, sparse_cells (cells where n < min_samples), is_sparse
```

Required report output for each experiment pair:

- A compact K₁×K₂ cell table (`n`, success rate) in the discussion body
- `interaction_p_value` from the contingency test
- `interaction_risk_status` set to `SPARSE_CELL_RISK` when any cell has `n < min_samples`

If `interaction_risk_status = SPARSE_CELL_RISK` and the core decision is `PROMOTE`, do not recommend
promotion. Keep `core_decision: PROMOTE`, set `report_action: EXTEND`, and use
`report_reason_code: interaction_underpowered`. Explain that this is a reporting safety hold until
interaction evidence becomes a normalized core analysis signal. Never replace the core decision
field itself.

## Step 5 — Generate Bar Charts

For each experiment, generate two bar charts using Python (libraries and directories are already set
up by the imported `shared/trending-charts-simple.md` environment):

### Chart A — Success Rate by Variant

```python
#!/usr/bin/env python3
import json, os
import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
import numpy as np

# Load per-run data written in step 2 (replace with actual data)
# variants: list of variant names
# success_rates: matching list of 0.0-1.0 success rates
# ns: matching list of sample sizes

fig, ax = plt.subplots(figsize=(10, 6), dpi=150)
colors = plt.cm.Set2(np.linspace(0, 1, len(variants)))
bars = ax.bar(variants, [r * 100 for r in success_rates], color=colors, edgecolor='white', linewidth=1.5)

# Annotate each bar with n and percentage
for bar, n, rate in zip(bars, ns, success_rates):
    ax.text(bar.get_x() + bar.get_width() / 2, bar.get_height() + 0.8,
            f'{rate*100:.1f}%\n(n={n})', ha='center', va='bottom', fontsize=11, fontweight='bold')

ax.axhline(y=success_rates[0] * 100, color='grey', linestyle='--', linewidth=1.2, label='Control baseline')
ax.set_ylim(0, 115)
ax.set_xlabel('Variant', fontsize=13)
ax.set_ylabel('Success Rate (%)', fontsize=13)
ax.set_title(f'Experiment: {experiment_name} — Success Rate by Variant', fontsize=14, fontweight='bold')
ax.legend(fontsize=11)
ax.grid(axis='y', alpha=0.4)
plt.tight_layout()
plt.savefig(f'/tmp/gh-aw/python/charts/{experiment_name}_success_rate.png',
            dpi=150, bbox_inches='tight', facecolor='white')
plt.close()
```

### Chart B — Mean Duration by Variant (with 95% CI error bars)

```python
fig, ax = plt.subplots(figsize=(10, 6), dpi=150)
# ci_lower, ci_upper: lists of CI bounds in seconds
yerr_lower = [mean - lo for mean, lo in zip(mean_durations_s, ci_lower_s)]
yerr_upper = [hi - mean for mean, hi in zip(mean_durations_s, ci_upper_s)]
colors = plt.cm.Set2(np.linspace(0, 1, len(variants)))
bars = ax.bar(variants, mean_durations_s, yerr=[yerr_lower, yerr_upper],
              color=colors, edgecolor='white', linewidth=1.5,
              capsize=8, error_kw={'linewidth': 2, 'ecolor': 'dimgray'})

for bar, mean, n in zip(bars, mean_durations_s, ns):
    ax.text(bar.get_x() + bar.get_width() / 2, bar.get_height() + max(yerr_upper) * 0.05,
            f'{mean:.0f}s\n(n={n})', ha='center', va='bottom', fontsize=11, fontweight='bold')

ax.axhline(y=mean_durations_s[0], color='grey', linestyle='--', linewidth=1.2, label='Control baseline')
ax.set_xlabel('Variant', fontsize=13)
ax.set_ylabel('Mean Duration (s)', fontsize=13)
ax.set_title(f'Experiment: {experiment_name} — Mean Duration by Variant (95% CI)', fontsize=14, fontweight='bold')
ax.legend(fontsize=11)
ax.grid(axis='y', alpha=0.4)
plt.tight_layout()
plt.savefig(f'/tmp/gh-aw/python/charts/{experiment_name}_duration.png',
            dpi=150, bbox_inches='tight', facecolor='white')
plt.close()
```

After saving each chart, upload it using the `upload_asset` safe-output tool and store the returned
asset URLs — they will be embedded in the discussion body.

## Step 5.5 — Build `min_samples` Progress Bars

Add a helper to render per-variant progress toward `min_samples` using fixed-width Unicode bars:

```python
def render_progress_bar(current, target, width=10):
    if target <= 0:
        return "░" * width + f" {current}/{target} (N/A)"
    ratio = max(0.0, min(1.0, current / target))
    filled = int(round(ratio * width))
    bar = "█" * filled + "░" * (width - filled)
    return f"{bar} {current}/{target} ({ratio*100:.0f}%)"
```

Use this helper in the per-experiment sample-size table:

```
███████░░░ 15/20 (75%)
██████████ 20/20 (100%)
██░░░░░░░░ 5/20 (25%)
```

## Step 6 — Render ASCII Comparison Table

For each experiment, produce an ASCII table inside a fenced code block:

```
Experiment : <experiment_name>
Workflow   : <workflow_file_name>
Hypothesis : <hypothesis text if declared, else "(not specified)">
Window     : last 30 runs  |  Analysed: <count> runs with artifacts
min_samples: <min_samples> per variant

+------------------+------+----------+----------------+--------------------+---------------+
| Variant          |  n   | Succ %   | Mean dur (s)   | 95% CI (s)         | min_samples   |
+------------------+------+----------+----------------+--------------------+---------------+
| <control>        |  ##  |  ##.#%   |    ###.#       | [###.# , ###.#]    | ##/## (##%)   |
| <variant_B>      |  ##  |  ##.#%   |    ###.#       | [###.# , ###.#]    | ##/## (##%)   |
+------------------+------+----------+----------------+--------------------+---------------+

Evidence: <analysis_type and p_value or probability_superiority from core evidence>

Guardrails:
  success_rate >=0.95 : PASS (control=0.97, variant_B=0.96)
  empty_output_rate ==0 : FAIL (variant_B=0.02) ← ABORT
  
For multi-variant experiments show pass/fail per variant per guardrail:
  success_rate >=0.95 : control=PASS(0.97), variant_B=FAIL(0.92), variant_C=PASS(0.96)

Core decision : <PROMOTE | EXTEND | REJECT | INCONCLUSIVE>
Reason code   : <reason_code from analyze>
Rationale     : <reason from analyze>
Report action : <same as core decision, or EXTEND for an interaction safety hold>
```

The decision and rationale above must come from `gh aw experiments analyze`. Report prose may
explain the result, but it must not translate `INCONCLUSIVE` into `REJECT`, select a different
candidate, or apply a second significance threshold.

## Step 7 — Post Discussion

Create a single GitHub Discussion containing all experiments using the `create-discussion`
safe output. The `shared/daily-audit-charts.md` import configures the discussion with
title-prefix `[experiments]`, category `audits`, and automatic cleanup of older discussions.

**Discussion title**: `[experiments] Daily Experiment Report — YYYY-MM-DD`

### Discussion body structure

Use h3 (`###`) or lower for all headers in your report. Never use h1 (`#`) or h2 (`##`) inside issue/comment bodies — these are reserved for the issue title.

Wrap long sections in `<details><summary><b>Section Name</b></summary>` tags to improve readability and reduce scrolling. Keep critical summaries and key metrics always visible.

Use visual cues consistently:
- Use emojis strategically (for example: `📊` charts, `✅` success, `⚠️` warnings, `❌` failures)
- Use status badges for readiness (`🟢 READY`, `🟡 COLLECTING`)
- Bold final decisions and wrap variant names in inline code

Suggested structure:
- Brief summary (always visible)
- Key metrics or highlights (always visible)
- Detailed analysis (in `<details>` tags)
- Recommendations (always visible)

```markdown
### 🧪 Daily Experiment Report — YYYY-MM-DD

[1–2 sentence executive summary: N experiments analysed across M workflows,
 K have decision-quality evidence, list core decisions and interaction safety holds at a glance.]

### ⚡ Quick Stats

| Metric | Value |
|--------|-------|
| Active experiments | N |
| Ready for analysis | R |
| Decisions with sufficient evidence | K |
| Core decisions | ✅ PROMOTE: P · 🟡 EXTEND: E · ❌ REJECT: R · ⚪ INCONCLUSIVE: I |
| Interaction safety holds | H |

---

#### `<experiment_name>` · `<workflow_basename>`

> **Readiness**: 🟢 READY / 🟡 COLLECTING
> **Variants**: `<v1>` vs `<v2>` · **Window**: last 30 runs · **Analysed**: N runs with artifacts
> **min_samples**: <min_samples> per variant · **Evidence**: <core evidence summary>

<hypothesis if declared>

<details>
<summary><b>📈 View Detailed Statistics</b></summary>

**Sample Sizes & Progress**
| Variant | Runs | Progress |
|---------|------|----------|
| `<control>` | n | ██████░░░░ n/<min_samples> (##%) |
| `<variant_B>` | n | ███░░░░░░░ n/<min_samples> (##%) |

![📊 Success Rate Chart](<ASSET_URL_success_rate>)

![⏱️ Duration Chart](<ASSET_URL_duration>)

<ASCII comparison table from Step 6 inside a ``` code block>

</details>

**Core decision: PROMOTE / EXTEND / REJECT / INCONCLUSIVE** (`<reason_code>`) — <core reason>

<If an interaction safety hold applies: **Report action: EXTEND** (`interaction_underpowered`)>

---

[Repeat the section above for each experiment]

### 📊 Summary

<details>
<summary><b>View Full Experiments Table</b></summary>

| Experiment | Workflow | Control | Candidate | Evidence | Guardrails | Core decision | Report action |
|-----------|---------|---------|-----------|----------|------------|---------------|---------------|
| ... | ... | ... | ... | ... | PASS/FAIL | ... | ... |

</details>

> Descriptive window: last 30 runs per workflow · Decision thresholds: from `decision_policy`
> Run: [${{ github.run_id }}](https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }})
```

If no workflows declare `experiments:`, create the discussion with a brief notice:

```markdown
### 🧪 No Active Experiments — YYYY-MM-DD

No workflows in `${{ github.repository }}` currently declare an `experiments:` section.

Run the `ab-testing-advisor` workflow to generate experiment campaign ideas.
```

After the discussion is created, also write a one-line summary to `$GITHUB_STEP_SUMMARY`:

```
Daily experiment report: N experiments analysed, M have decision-quality evidence, H interaction holds. Discussion: <url>
```

## Step 8 — Notify Tracking Issues

For each experiment that has a `issue:` field set, post a comment to that tracking issue when any
of the following conditions are met **for the first time today**:

**Condition A — Core readiness is `READY`:**
Post a comment:
```
🧪 **Experiment `<name>` is ready for analysis!**

All variants have reached the minimum sample size of `<min_samples>` runs:
<variant>: <n>/<min_samples>
...

View the latest statistics in the [Daily Experiment Report](<discussion_url>).
```

**Condition B — Core decision is `PROMOTE` and no interaction safety hold applies:**
Post a comment:
```
📊 **Experiment `<name>` has a deterministic promotion decision**

Decision: **PROMOTE `<candidate>`**
Reason: `<reason_code>` — <reason>

View the full report: [Daily Experiment Report](<discussion_url>)
```

**Condition C — Core decision is `REJECT` with reason code `guardrail_failed`:**
Post a comment:
```
⚠️ **Guardrail violation in experiment `<name>`**

The following guardrail metric failed:
- `<metric_name>` expected `<threshold>`, got `<actual_value>` for variant `<variant>`

Decision: **<decision>** (`<reason_code>`) — <reason>
```

Use the `add-comment` safe-output tool to post comments. Skip experiments with no
`issue:` field. Do not post duplicate comments if the same condition was already reported in a
previous run today.

## Step 9 — Update Experiment Lifecycle Labels

For each experiment with a tracking `issue:` field, apply the following GitHub labels on the
tracking issue when the corresponding condition is met. Create the label first if it does not
already exist (use a neutral gray color). Labels are **additive only** — once applied they are
not removed automatically; the person concluding the experiment can remove them manually.

| Label                           | Apply when                                                                   |
|--------------------------------|------------------------------------------------------------------------------|
| `experiment:active`            | `start_date <= today <= end_date` (or no dates declared)                    |
| `experiment:ready-for-analysis`| Core `readiness` is `READY`                                                   |
| `experiment:concluded`         | Core `decision` is `REJECT`, or core `decision` and `report_action` are both `PROMOTE` |

Use the `add-labels` safe-output tool to apply labels to the tracking issue.
If a label does not exist in the repository, create it with `create_label` GitHub MCP tool
before applying it, using a neutral gray color (e.g. `#808080`) and a short description.

## Step 10 — Self-Tuning Continuation Plan (Experiments + Evals, Grader-Ready)

After generating the daily report, append a short **Self-Tuning Continuation Plan** section to the
discussion body with:

1. **Top 3 experiment actions** derived from core `decision` and `reason_code`.
2. **Top 3 eval actions** (which low-scoring eval questions to tighten, and which workflow prompts they map to).
3. **Decision-pipeline gaps** for the next PR:
   - identify unresolved native metrics or guardrails that produced `insufficient_observations` or `guardrail_unsupported`,
   - identify interaction diagnostics that should become normalized core analysis signals.

Keep this section implementation-oriented and concise. The goal is continuous convergence toward a
self-tuning AW system that uses experiments for assignment, evals and graders for observations,
core analysis for deterministic decisions, and reporting for explanation.

### Output Format

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks to reduce scrolling.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.