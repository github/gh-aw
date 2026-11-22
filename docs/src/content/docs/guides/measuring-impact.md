---
title: Measuring Impact
description: Learn how to measure the impact of agentic workflows using cost, adopted outputs, funnel metrics, and system-level trends.
sidebar:
  order: 295
---

Measure impact with **early cost signals** and **later adoption signals**.
Do not collapse them into a single score.

Use this page to choose metrics and read them together. For spend
controls, see [Cost management](/gh-aw/reference/cost-management/).
For downstream result tracking, see [Delivery and adoption](/gh-aw/reference/delivery-adoption/).
For the slides' concrete schema-coverage replay, see the
[Value report](/gh-aw/reference/value/).

## Timing of cost, delivery, and adoption

Execution evidence establishes delivery when an effect is applied. Cost comes
from recorded usage. Adoption follows a type-specific rule, potentially days
later; an explicitly permitted delivery fallback is not human approval.
Attributed value additionally requires a declared measurement, attribution
evidence, and survival through the complete post-adoption window.

Avoid custom impact formulas. Read the metrics gh-aw already exposes in layers, each on the timeline where it becomes trustworthy:

| Layer | Start with | Data source |
| --- | --- | --- |
| Operations | Run count, completion rate, retries, duration | [`gh aw logs`](/gh-aw/setup/cli/#logs) and [`gh aw health`](/gh-aw/setup/cli/#health) |
| Cost efficiency | AIC and AIC per successful run | [`gh aw logs`](/gh-aw/setup/cli/#logs) and [Cost Management](/gh-aw/reference/cost-management/) |
| Delivery | Applied effects and execution-evidence coverage | `gh aw logs --delivery` |
| Adoption | Human/automation/fallback acceptance, rejection, pending decisions, time to adoption | `gh aw logs --adoption` |
| Attributed value | Declared measurement, attribution evidence, complete-window survival | Recorded measurement and lifecycle evidence; unavailable if any proof is missing |
| Long-term impact | Backlog, review time, defects, or another repository-specific goal | Repository analytics or organization reporting |

Long-term impact is the hardest to attribute. Start with run volume, success, cost, and acceptance over time; once stable, connect them to whether outputs reduced later work.

## A Practical Measurement Model

For workflows that produce [safe outputs](/gh-aw/reference/safe-outputs/),
use this measurement loop:

1. Choose an observation window, such as the last 30 days, and record the workflow's run count, successful runs, and AIC.
2. Identify delivered effects with `gh aw logs WORKFLOW --delivery`. No lifecycle configuration is required for built-in safe-output rules.
3. Allow enough time for repository activity to reveal the result. For example, wait for a proposed pull request to be reviewed or an issue to be resolved.
4. Evaluate selected runs with `gh aw logs WORKFLOW --adoption`. Run the command again later for outputs that are still `pending`.
5. Compare similar output types over time. Separate human adoption from automation decisions and delivery fallbacks, and track AIC per adopted output, without treating that ratio as attributed value.

For a quick machine-readable summary:

```bash wrap
gh aw logs weekly-research --adoption --json |
  jq '.adoption[] | {run_id, summary}'
```

For a specific run, pipe its ID to `gh aw logs --stdin --repo owner/repo --adoption`.
The command downloads safe-output manifests and checks the current state of the affected objects. See [Delivery and adoption](/gh-aw/reference/delivery-adoption/)
for evaluation timing, result interpretation, and telemetry export.

Use built-in records first. Repeated collection preserves later observations
without rewriting original sessions or historical grader results. A current
survival snapshot does not establish survival through an entire measurement window.

This shows where impact breaks down: a workflow that runs reliably but produces little value, whose output is rarely adopted, or that is effective but too expensive.

A workflow can look efficient alone while reducing total system value, for example when overlapping triggers create duplicates or competing suggestions increase review burden. Also measure which workflows act on the same event type, how often they duplicate output, and the cost per unique accepted adoption.

## System Overlap and Waste

For a five-role portfolio with shared coding capacity and human-reviewed,
evidence-driven tuning, use the [Repo Factory recipe](repo-factory.md).

Waste is any cost, time, or reviewer attention that does not produce proportional value: redundant runs, duplicate outputs, repeated context collection, expensive model calls for deterministic work, and outputs with consistently low acceptance.

Use the `object_url` and `repo` fields in `.adoption[].items[]` from `gh aw logs --adoption --json` to find multiple workflows acting on the same object before changing triggers or consolidating workflows.

Typical fixes: consolidate overlapping workflows, share intermediate artifacts, cache stable context, and move deterministic work out of the agent path.

Prefer trends to single numbers: cost per successful run falling, acceptance rising, retries dropping, and overlap decreasing.
