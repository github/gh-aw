---
title: Value report
description: Report a workflow's delivered effects, adoption, recorded grades and independently measured outcomes.
sidebar:
  order: 298
---

`gh aw value WORKFLOW` is an experimental, read-only report. Use it in the workflow owner's repository; no central registration or new collection job is required. To collect existing evidence and then read it:

```bash wrap
gh aw logs WORKFLOW --repo OWNER/REPO --adoption --artifacts usage,delivery,graders --output .github/aw/logs
gh aw value WORKFLOW --repo OWNER/REPO --json
```

`logs --adoption` includes delivery. To select a fixed cohort, supply completed IDs to `logs --stdin` and set `--count` accordingly. `value` reads the retained export; it does not refresh GitHub object dispositions, fetch Git history, dispatch workflows, or regrade sessions.

| Report section | Meaning |
| --- | --- |
| `lifecycle` | Native effects delivered during the period and their latest retained adoption observations. Each effect is counted separately. Missing records mean unknown, not zero. |
| `run_grades` | Recorded primary `operational-value` results, scoped by completed run creation time. |
| `weekly`, `cohort` | First-parent Git measurements at the recipe cadence plus the final date; all authors contribute. |
| `outputs`, `windows` | Producer-linked PRs, individual merge comparisons, samples and reasons for pending or unavailable attribution. `inspected_windows` is an optional presentation selected by a recipe. |
| `evidence` | Independently supplied observations and comparable baseline/outcome pairs. They do not authenticate adoption or survival. |
| `attributed_value` | Subtotal in the measurement's native unit for qualifying PR windows only; `null` if none qualify. Consult `attribution_coverage` before interpreting a number. |

### Recorded grader results

The report reads `summary.json` in `--logs-dir` and matches host, repository, exact workflow path, canonical run URL, run ID and attempt. It preserves primary value (including zero), unit and status. Both text and JSON omit free-form grader messages and errors; these remain in the original summary. No definition, Node.js or Git history is needed for this section. Missing grades remain unavailable; unrelated built-in grades are never substituted. Even a successful export represents a retained subset, not complete history.

### Supply a measurement

Run `/operational-value-designer OWNER/REPO WORKFLOW` in a clean design context, or write the declarations directly. **Freeze the metric, eligible population, windows and fixtures before seeing the cohort's outcomes.** Evaluate them afterward. See the [Operational Value Report Specification](/gh-aw/specs/operational-value-specification/).

The CLI discovers `.github/aw/value/WORKFLOW.json`, or accepts `--value-function PATH`. The definition selects a named Go evaluator; it contains no executable code. For example:

```json title=".github/aw/value/dependency-maintenance.json"
{
  "version": 1,
  "id": "outdated-dependencies-v1",
  "name": "Outdated dependencies",
  "unit": "dependencies",
  "direction": "lower_is_better",
  "evaluator": "json-number-v1",
  "config": {"path": "metrics/dependencies.json"}
}
```

This reads an existing Git blob such as `{"value":12}`. `0` is measured zero; `null` means unavailable. Optional `population` contains unique subject IDs. Population changes block comparison. `schema-example-coverage-v1` instead measures textual examples of top-level schema fields; its `config` names a repository-relative `schema` and literal `examples` directories, with unit `fields` and direction `higher_is_better`.

With the same schema population, change is the **signed difference** in covered fields. Newly covered fields remain listed in `closed`; lost coverage reduces the change and can make it negative.

For observations that are not committed numbers, select `evidence-number-v1` and pass `--evidence PATH`. Its `config` specifies `scope` (`git_snapshot`, `run`, `benchmark`, `object_state`), `aggregation` (`scalar`, `median`, `excess_lines`), `minimum_samples` and `require_population`. Benchmarks require median aggregation and a comparable `context`; `excess_lines` requires `git_snapshot`, population and a nonnegative `maximum_lines`.

The version-1 JSON bundle has `repository: "HOST/OWNER/REPO"` and an `observations` array. Each observation has a unique `id`, `workflow`, definition ID in `metric`, `scope`, stable `subject`, positive `run_id`, event `at`, collection `observed_at`, `phase` (`baseline`, `outcome`, `sample`), `coverage` and source `references`. Available rows require finite `values` (not null elements); available Git snapshots require a full `sha`. Partial or unavailable rows carry a `reason` and no values. Pair at most one baseline and outcome per subject. Supply the actual eligible `population` when required. An unsupported or unobservable goal stays unavailable.

### Apply a recipe

A version-1 recipe supplies an `id`, matching `repository` and `workflow`, `start` and `end`. **Pass `--recipe PATH` explicitly** to apply its cohort and window policy. It does not combine with `--start` or `--end`. For Git replay, a complete local checkout, matching `origin`, a ref covering the cutoff, and authenticated `gh` read access for PR metadata are required. For typed evidence, use `--evidence` instead of `--ref`; Git history and PR discovery are not used.

The optional `window` sets `baseline` (`exact_parent` for Git, `paired_observation` for typed input), `before_days`, `after_days`, `outcome_lag_days`, `durability_days`, `sample_every_hours`, `trend_every_days`, `minimum_before_samples`, `minimum_after_samples`, `isolation_before_days`, `isolation_after_days` and `overlap` (`workflow` or `changed_files`). The default seven-day policy reproduces the slides; it is not a universal causal horizon. Git recipes can also pin `author` with `author_login`, `early_batches_through`, and individual PR `witnesses`. Batches never receive attribution.

### Reproduce the schema example

From a full `github/gh-aw` checkout, collect the completed witness run and replay the checked-in definition and slides recipe against the pinned main history:

```bash wrap
printf '%s\n' 36389625388 | gh aw logs --stdin --repo github/gh-aw --adoption --output .github/aw/logs
gh aw value schema-feature-coverage --recipe .github/aw/value/schema-feature-coverage.slides.json \
  --ref 30aaa011200b522ca1ffce0c8aed019048aac096 --json
```

Check `weekly` for repository-wide coverage, `windows` for individual attribution, and `inspected_windows` for selected slide rows. Retain the original logs and JSON; a new API observation can change coverage. A repository trend is not the attributed subtotal.

### Options

| Option | Default / effect |
| --- | --- |
| `--repo`, `-r` | Current repository; accepts `[HOST/]OWNER/REPO`. |
| `--start`, `--end` | Inclusive noon UTC dates; default end yesterday, start 30 days earlier. Maximum span: 730 days. |
| `--logs-dir` | `.github/aw/logs`, containing `lifecycle.jsonl` and `summary.json`. |
| `--value-function`, `--recipe`, `--evidence` | Explicit definition, report policy and typed input respectively. |
| `--ref` | Local `origin/main`; supply a full commit SHA to pin Git replay. |
| `--daily` | Add one noon UTC row per day for a Git metric, with retained human acceptances by decision time. Does not change the subtotal. |
| `--timeout` | Positive Go duration, default `5m`. |
| `--json`, `-j` | Emit the full JSON report instead of the terminal summary. |
