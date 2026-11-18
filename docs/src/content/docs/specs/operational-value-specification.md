---
title: Operational Value Report Specification
description: How to freeze a workflow's measurement before adoption, collect evidence, and interpret a bounded, reproducible value report.
sidebar:
  order: 1371
---

**Version:** 1.0.0 · **Feature status:** Experimental · **Report schema:** 1

`gh aw value WORKFLOW` answers three distinct questions: **What did the workflow deliver? What did a human accept? What independently measured outcome changed?** Only a subset of measured changes qualifies for attribution. A successful run, a delivered comment, a merged PR, and a unit of improvement are not interchangeable. This contract applies in the workflow owner's repository; no upstream registration, CAO service, or choice of reporting mode is required.

## Before adoption: freeze the question, not the answer

Use `/operational-value-designer OWNER/REPO WORKFLOW` once, or author the same declarations directly. **For a prospective claim, the designer must not see the target cohort's results.** Use a clean context containing the workflow's stated intent, constraints, evidence *formats*, and synthetic fixtures. Do not supply target run logs, grades, PR or issue decisions, metric history, favorable witnesses, or earlier conversations and reports containing those results.

| Freeze together | What must be decided without seeing outcomes |
| --- | --- |
| Measurement | One goal, eligible population and stable subject IDs, independent evidence producer, formula/evaluator version, native unit, direction, and what counts as an observed zero, missing input, change of population, or error. |
| Recipe | Repository/workflow, cohort and selection rule, baseline, clocks, sampling cadence, minimum observations, response lag, follow-up, durability and overlap rules. Do not choose the best-looking horizon after the fact. |
| Fixtures and provenance | Synthetic improved, unchanged, worse, unavailable, malformed and changed-population cases with expected results. Review and retain the exact definition, recipe, fixtures and producer/protocol version in the owner's repository. |

Save the versioned JSON definition as `.github/aw/value/WORKFLOW.json`. It contains `version`, `id`, `name`, `unit`, `direction`, `evaluator` and evaluator-specific `config`, **not** executable code or the observations themselves. A separate versioned recipe is passed with `--recipe`; only the definition is auto-discovered. Keep a baseline before eligible work takes effect and arrange for the independent producer to collect follow-up data. The report does not create that data.

**After freeze**, open a separate evaluation context and apply the rules unchanged to the whole selected cohort, including no-ops, regressions, missing observations, and windows still open. If the designer has already seen the cohort's outcomes, call that analysis *retrospective*; a revised contract applies to a new, unseen cohort. An exact digest makes a replay identifiable, not outcome-blind. There is deliberately **no timestamp or context-purity certification** in the CLI.

## Get the report

Use a `gh aw` build with `value`. From the owner's checkout, collect completed runs into one logs directory with read access to Actions and the relevant GitHub objects:

```bash wrap
gh aw logs WORKFLOW --repo OWNER/REPO --adoption \
  --artifacts usage,delivery,graders --output .github/aw/logs
```

The first command is the collector: it can download artifacts, observe **current** object dispositions, and write local evidence. It need not run again to read a retained export. Its default count is bounded; for a frozen cohort, pass its fixed run IDs via `--stdin` and a sufficient `--count`. Do not select only runs that delivered an output. Archive failed collections as failures, not zero-effect runs.

Read the report with `gh aw value WORKFLOW`. The bare command uses default dates and window policy; **to apply the frozen recipe, pass it explicitly**. For Git, also pin the full local main-history SHA:

```bash wrap
gh aw value WORKFLOW --recipe .github/aw/value/WORKFLOW.review.json \
  --ref MAIN_HISTORY_SHA --logs-dir .github/aw/logs --json > value-report.json
```

For `evidence-number-v1`, pass `--evidence observations.json` **instead of** `--ref`. For the terminal report omit `--json`. Without a recipe, `--start` and `--end` select inclusive noon UTC endpoints (default: yesterday and the preceding 30 days). Do not combine explicit dates with `--recipe`. No definition is needed to read lifecycle records or existing run grades. A custom `graders.operational-value.script`, if configured, runs in a separate Actions job. No report command dispatches a workflow, downloads grader artifacts, or executes a grader.

## Data contracts and clocks

| Report layer | Source and join | Interpretation |
| --- | --- | --- |
| Delivery | `--logs-dir/lifecycle.jsonl`: retained native applied effects, scoped to host, repository, exact workflow path and **delivery time**. Native identity includes run/attempt, invocation/evidence IDs, output type and target. | Count effects, not unique runs, people, or PRs. Two different effects on one issue remain two effects. No matching records means unknown, even when a separate run summary records a no-op. |
| Adoption | GitHub object disposition collected by `logs --adoption`, retained on the same effect. Keep **decision time** and **collection time** distinct. | Explicit human acceptance, delivery fallback, pending, rejection and missing evidence differ. Later observations are not a frozen historical journal. |
| Run result | `--logs-dir/summary.json`: completed run matched by host/repo, exact workflow path, canonical run URL, ID, attempt and **run creation time**. Use only its recorded primary `operational-value` grade. | Preserve value, unit, status and error. The report neither regrades nor sums grades. A full session is input to a grader when it runs, not required input to this report. A compact session is not a grade archive. |
| Git outcome | A full, non-shallow checkout with matching `origin`, local ref covering the end, and authenticated `gh` read access for PR search/merge identity. Sample first-parent Git at the declared times. `json-number-v1` reads a committed `{"value":12}` or `{"value":null}`; `schema-example-coverage-v1` reads a schema and example inventory. | All authors contribute to the trend. A supplied number needs an independent producer; the report does not run its scanner. Evaluate comparable populations, not just endpoint totals. |
| Typed outcome | `--evidence`: versioned bundle for the selected host/repo. Match workflow, metric, scope and event time. Retain subject and run ID, event and collection clocks, coverage/reason, source references, population, context and finite raw samples. Git observations require a SHA when available; benchmarks require comparable context and replicates. | Pair at most one baseline and outcome per subject. An available zero is a number; a missing sample is not zero. Imported observations are caller-supplied, not authenticated acceptance, survival, or attributed value. No Git history or PR search is needed for this path. |

`--daily` adds one noon UTC snapshot per day for a **Git** metric. Each row also counts retained verified human decisions for that workflow in `(previous noon, this noon]`, by decision time, including effects delivered earlier. The first row includes only decisions exactly at the start instant. A positive count is a lower bound (`retained_subset`); absent proof is `null` (`unknown`), not a certified zero. An afternoon decision therefore appears at the **next** noon, never against an earlier measurement. Sparse typed inputs are not interpolated. The daily view changes no attribution rule.

## When a change counts

The [independent collection example](/gh-aw/reference/value/#collect-independent-observations)
shows how to supply these inputs for future cohorts without running measurement code in the report.

1. **Identify the effect.** Git PR discovery uses the exact workflow footer, producer run and canonical PR URL. Require one matching native delivered `create_pull_request` record with available, GitHub-verified, explicit-human acceptance at the exact merge time. PR search is not a delivery ledger.
2. **Measure and follow up.** Compare the exact first parent with the outcome on the selected first-parent history. Apply evaluator-specific population rules and sample minima. Wait for the full lag-plus-after, durability and forward-isolation horizons, not just the first positive sample. Use the declared overlap rule across producer-linked merges (including neighbors outside the delivery period); require every changed file to remain untouched through durability. A later aggregate change without subject linkage is descriptive, not credited.
3. **Publish the subtotal and its coverage.** `windows[].reason` explains the decision. `pending` means the observation window is still open; `unavailable` means a gate lacks proof. A numeric zero means an eligible, measured zero, not missing evidence. `attributed_value` is the sum of qualifying individual windows in the **one metric's native unit**, or `null` if none qualify. `complete` means all merge windows in this report qualified, not that all historical work was observed. A partial subtotal is not a workflow total or proof of exclusive causality.

Issues, comments, labels, discussions and other safe outputs remain reportable via their native records. An accepted issue followed by a change in a related PR can have both adoption evidence and an independent outcome, but it does not satisfy the **direct-PR** attribution contract. Never convert grades, delivery fallbacks, descriptive batches or repository-wide trends into credit.

## Replay and scale

Archive the CLI/evaluator version, exact declarations and digests, pinned Git SHA or typed bundle and digest, fixed run IDs/attempts, lifecycle and summary exports, provider observations and collection clocks, and the resulting JSON. The Git path still reads live GitHub PR metadata; it has no offline API-response replay flag. Refreshing `logs` can legitimately change coverage. For identical retained typed inputs, compare all report fields except the report invocation time `observed_at`.

| Resource | Current boundary | Scaling consequence |
| --- | --- | --- |
| Local inputs | Definition, recipe and typed bundle: 512 KiB each; at most 1,024 typed observations. Grade summary: 50 MiB. Lifecycle cache: 16 MiB / 20,000 items; writes prune deliveries older than 365 days. | Archive immutable collection slices before pruning. Use separate directories for independent cohorts; do not concatenate caches and assume completeness. |
| GitHub discovery | Up to 100 results per page, maximum 1,000 matches; incomplete/changing searches fail. Search precedes report-date filtering so neighboring merges can be checked. | A narrower reporting period need not fix the search limit. Do not truncate results or discard neighbors to produce a number. Record API/auth failures separately from empty cohorts. |
| Git sampling | Up to 730 days between endpoints, at most 1,024 pre/post sample slots per merge. `--daily` adds up to 731 noon rows and reuses trend snapshots at matching times. Report timeout defaults to 5 minutes. | Cost grows with trend/daily dates **plus** merges times samples; first-parent walks, evaluator inventory size and changed paths also matter. A 30-day Git report and a small typed replay are not equivalent workloads. |
| Many workflows | One report per workflow; no central registry, global index or cross-workflow result cache. | Reuse frozen inputs; use bounded concurrency and separate collection directories. Retain failures per workflow. Compare or combine only commensurate metric versions, units and populations; never sum grades or overlapping subjects across workflows. |

The fixed-cohort replay checks correctness, **not enterprise throughput**. Before increasing concurrency, measure API requests/rate limits, elapsed time, memory, retained bytes, and coverage separately for Git and typed workloads across representative repositories. Do not infer enterprise capacity from small local replays.

## SWOT analysis

| | Assessment | Practical response |
| --- | --- | --- |
| **Strengths** | One owner-controlled declaration and report works across output types. Named Go evaluators do not execute workflow code. Versioned inputs, native units, stable identities and explicit unknowns make claims inspectable. | Review the metric once, preserve the original evidence, and reproduce each subtotal before using it. |
| **Weaknesses** | A bounded latest-state ledger cannot certify zero acceptances on a day. Direct-PR attribution omits real indirect work; file survival rejects benign edits and is not proof of durable user benefit. Independent outcomes are often harder to collect than activity. | Publish coverage and unavailable reasons next to the subtotal. Never rank workflows by unassessed windows or substitute a convenient proxy without changing the contract prospectively. |
| **Opportunities** | Independent benchmark/issue-state producers can feed the existing typed adapter without another execution runtime. Complete collection manifests or explicit effect-to-outcome links could support verified zeros and more non-PR cases. | Prioritize independent observations for unmet goals; then test coverage and subject linkage on held-out cohorts before extending attribution. These are not capabilities of the current ledger. |
| **Threats** | Post-hoc metric tuning, selective collection, changing denominators or protocols, concurrent changes and self-reported evidence can produce convincing but misleading trends. Expired artifacts, API limits and live disposition changes can alter replays. | Keep design outcome-blind, freeze the protocol, archive sources and clocks, report gaps and negative outcomes. Retain the report under the same access controls as the underlying repository evidence. |

No causal model or distributed-lag regression is fitted. A separate observational analysis would need a continuous comparable series, trustworthy exposure coverage (including verified zeros), enough independent variation and uncertainty estimates that account for serial correlation. It must not be added to `attributed_value`.

## Conformance and evidence

Check improved, zero, worse, missing, malformed and population-change fixtures with the production evaluator. Verify identity isolation, unavailable grades, no-adoption/overlap/survival gates, and stable replays of a fixed cohort without replacing failures. The [CLI reference](/gh-aw/reference/value/) specifies flags and JSON fields; the [grader specification](/gh-aw/specs/graders-specification/) covers the separate execution contract. **After freeze**, see the [ten-workflow robustness results and reproduction command](/gh-aw/reference/value-pilot/#fixed-cohort-robustness-check) for empirical coverage and limitations.
