---
title: Measuring Impact
description: Learn how to measure the impact of agentic workflows using cost, outcomes, funnel metrics, and system-level trends.
sidebar:
  order: 295
---

Measure impact with **early cost signals** and **later outcome signals**.
Do not collapse them into a single score.

Use this page to choose metrics and read them together. For spend
controls, see [Cost management](/gh-aw/reference/cost-management/).
For downstream result tracking, see [Outcomes](/gh-aw/reference/outcomes/).

## Timing of Cost and Outcomes

Cost estimates are available early, while accurate cost arrives later from Actions minutes, runner duration, inference usage, and artifact retention. Outcomes arrive later still: a comment may wait days for a response, and a proposed change or created issue matters only once it is merged or resolved.

Avoid custom impact formulas. Use the metrics gh-aw already exposes, each read on the timeline where it becomes trustworthy, in layers rather than a synthetic score:

| Layer | Start with | Data source |
| --- | --- | --- |
| Operations | Run count, completion rate, retries, duration | [`gh aw logs`](/gh-aw/setup/cli/#logs) and [`gh aw health`](/gh-aw/setup/cli/#health) |
| Cost efficiency | AIC and AIC per successful run | [`gh aw logs`](/gh-aw/setup/cli/#logs) and [Cost Management](/gh-aw/reference/cost-management/) |
| Outcomes | Accepted, rejected, pending, acceptance rate, time to outcome | [`gh aw outcomes`](/gh-aw/setup/cli/#outcomes) |
| Long-term impact | Backlog, review time, defects, or another repository-specific goal | Repository analytics or organization reporting |

Long-term impact matters but is the hardest to attribute directly. Most teams should start with run volume, execution success, cost, useful output rate, and acceptance over time. Once those are stable, connect them to downstream questions such as whether outputs were accepted, merged, or reduced later work.

## A Practical Measurement Model

For workflows that produce [safe outputs](/gh-aw/reference/safe-outputs/),
use this measurement loop:

1. Choose an observation window, such as the last 30 days, and record the workflow's run count, successful runs, and AIC.
2. Identify the runs that produced safe outputs. No additional `outcomes:` configuration is required; outcome evaluation uses the safe-output artifacts from each run.
3. Allow enough time for repository activity to reveal the result. For example, wait for a proposed pull request to be reviewed or an issue to be resolved.
4. Evaluate each run with `gh aw outcomes RUN_ID`. Run the command again later for outputs that are still `pending`.
5. Compare similar output types over time. Track accepted outcomes and AIC per accepted outcome, then investigate workflows whose acceptance falls or cost rises.

For a quick machine-readable summary:

```bash wrap
gh aw outcomes 1234567890 --json |
  jq '.summary | {total, accepted, rejected, ignored, pending, acceptance_rate}'
```

The run ID is available in the GitHub Actions run URL. The command
downloads the run's safe-output artifacts and checks the current
state of the affected issues, pull requests, comments, and other
supported objects. See [Outcomes](/gh-aw/reference/outcomes/#evaluating-outcomes-in-practice)
for evaluation timing, result interpretation, and telemetry export.

Use built-in telemetry first: `gh aw logs` covers runs and cost, and [Outcomes](/gh-aw/reference/outcomes/) covers downstream acceptance. For repository-wide or organization-wide trends, send the same data to [OpenTelemetry](/gh-aw/reference/open-telemetry/).

This shows where impact breaks down: a workflow that runs reliably but produces little value, produces output that is rarely adopted, or is effective but too expensive.

A workflow can look efficient alone while reducing total system value, for example when two workflows act on the same issue or pull request, overlapping triggers create duplicates, or competing suggestions increase review burden. Measure system-level overlap too: which workflows act on the same event type, how often they duplicate output, and what the cost per unique accepted outcome is.

## System Overlap and Waste

Waste is any cost, time, or reviewer attention that does not
produce proportional value. Common sources include redundant
runs, duplicate outputs, repeated context collection, expensive
model calls for deterministic work, and outputs with consistently
low usage or acceptance.

Use the `object_url` and `repo` fields from `gh aw outcomes --json`
to find multiple workflows acting on the same repository object.
That gives you a concrete starting point for identifying duplicate
outputs before changing triggers or consolidating workflows.

Typical fixes include consolidating overlapping workflows,
sharing intermediate artifacts, caching stable context, and
moving deterministic work out of the agent path.

Do not overreact to single numbers. Trend data is usually more
useful. Look for cost per successful run moving down, useful
output rate and acceptance moving up, retries dropping, and
system overlap decreasing.
