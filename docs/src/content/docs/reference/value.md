---
title: Value report
description: Report shared delivery and adoption for any workflow, with workflow-specific value functions and conservative longitudinal attribution.
sidebar:
  order: 298
---

`gh aw value WORKFLOW` reports delivery and adoption for **any
workflow** using the same native records and rules. Only the value function
varies by workflow. Without a function, lifecycle evidence remains visible and
value is explicitly unavailable. Workflows do not need to be part of
`github/gh-aw`: run the installed CLI in the workflow owner's checkout and keep
the measurement definition and recipe in that repository.

The shared pipeline handles historical sampling, lifecycle joins, merge-window
isolation, and full-window file survival. Run success, merge counts, delivery
fallbacks, and run-scoped grader scores are not longitudinal attributed value.

## Get the report

Collect or refresh native evidence, then read the report:

```bash wrap
gh aw logs weekly-research --repo owner/repo --adoption --output .github/aw/logs
gh aw value weekly-research --repo owner/repo --json
```

The report reads existing `.github/aw/logs/lifecycle.jsonl` evidence without
refreshing or changing it. `--logs-dir` selects another collection directory.
The lifecycle section includes all retained safe-output types for the selected
workflow and delivery period, not just pull requests.

The [ten-workflow pilot](../value-pilot/) records real applicability results,
candidate measurements, and evidence gaps. It distinguishes working lifecycle
reporting from an independently verified outcome or attributed value.

`lifecycle.coverage: "retained_subset"` means counts describe retained records,
not the workflow's complete history. Human acceptance and delivery fallbacks
remain separate. A missing ledger or no matching records produces unavailable
counts, not verified zeros. Adoption observations retain their original
collection clocks; the delivery cohort does not make them historical snapshots
of adoption at the report cutoff.

Without a value function, the command needs no Git history or measurement
runtime. It returns `measurement_status: "unavailable"`,
`reason: "measurement_not_configured"`, and `attributed_value: null`.
Recorded run grades, when collected, remain available separately in `run_grades`.
Only collect completed runs; no new execution or workflow dispatch is needed.

## Daily evidence

Add `--daily` to a historical Git report to see one measurement per day, independently of PR windows:

```bash wrap
gh aw value schema-feature-coverage --daily \
  --recipe .github/aw/value/schema-feature-coverage.slides.json \
  --logs-dir .github/aw/logs --json
```

Each `daily[]` row contains the noon UTC `snapshot` (commit, value, and population when available).
`retained_human_acceptances` counts distinct retained delivery records for this workflow with
a verified human acceptance, by **decision time**, even if delivery happened before the report.
Each interval ends at the row's noon, inclusive, and excludes the preceding noon.
The first row is the baseline and includes only decisions exactly at the start instant.

The lifecycle ledger retains the latest observation per effect, not a complete decision history.
Positive counts have `acceptance_coverage: "retained_subset"`; without a matching decision,
the count is `null` and coverage is `unknown`, **not** a verified zero. Delivery fallbacks are
not counted. `--daily` neither estimates a temporal association nor changes attributed value.
Sparse typed evidence and session grades are not interpolated into daily measurements.

## Recorded grader results

The command also reads primary `operational-value` grades already exported by
`gh aw logs` into `summary.json` under `--logs-dir`. To collect those artifacts:

```bash wrap
gh aw logs weekly-research --repo owner/repo --artifacts graders \
  --output .github/aw/logs
gh aw value weekly-research --repo owner/repo --json
```

`run_grades.runs` contains the run ID, attempt, creation time, and the existing
grader result, including its primary value, unit, status, and message/error.
Only completed runs matching the selected host, repository, workflow path, and
creation-time period are included. Run URLs must agree with the recorded
identity. Grades are collected observations, not reconstructed historical
grades as of the period cutoff.

Observed zero remains zero. A missing `operational-value` result is reported as
`unavailable`; execution errors remain `error`. Other built-in grades are not
substituted for operational value. Missing summaries or no matching completed
runs produce unavailable coverage. Matching rows have `retained_subset`
coverage, not a claim of complete run history. Malformed summaries, conflicting
identities, duplicate results, and inconsistent statuses/values fail explicitly.
The summary input is limited to 50 MiB.

This is a read-only view of the existing logs export: no code execution,
regrading, artifact fetching, aggregation, or automatic attribution. The
recorded grades themselves need no measurement definition, Git history, or
Node.js. If a Git/typed-evidence measurement is configured, its existing input
requirements still apply. Recollect logs to refresh the exported grades.
Custom `--summary-file` locations are not discovered automatically.

## Define the workflow-specific measurement

Use the
[operational-value-designer skill](https://github.com/github/gh-aw/blob/main/.github/skills/operational-value-designer/SKILL.md)
to derive, validate, and freeze a measurement and report recipe from workflow intent:

```text
/operational-value-designer owner/repo WORKFLOW longitudinal
```

The skill authors declarations and fixtures in the workflow owner's repository;
it does not require that repository to contain gh-aw's source or Go tooling.
The command does not invoke an
agent or invent a metric. Measurement definitions select named Go evaluators.
The report starts no JavaScript or query runtime. Run-scoped session graders
remain separate.

Save a reviewed definition in `.github/aw/value/WORKFLOW.json`, or
select it with `--value-function PATH`. Paths inside evaluator configuration
are relative to that checkout's Git root. No upstream workflow registration is
required.

For an existing, independently measured numeric outcome committed in repository
history, use the generic native evaluator:

```json title=".github/aw/value/dependency-maintenance.json"
{
  "version": 1,
  "id": "critical-vulnerabilities-v1",
  "name": "Critical vulnerabilities",
  "unit": "vulnerabilities",
  "direction": "lower_is_better",
  "evaluator": "json-number-v1",
  "config": {"path": "metrics/vulnerabilities.json"}
}
```

The evidence file contains `{"value":12}`; an explicit `{"value":null}` means
unavailable, not zero. Missing or malformed files return errors. An optional
`population` array contains unique nonempty subject IDs; a change in its
presence or membership makes the merge's improvement unavailable. Values retain
their native scale. Improvement is `after - before` for `higher_is_better` and
`before - after` for `lower_is_better`; deterioration is negative.

This evaluator transports existing measurements; it does not run a scanner,
grader, or workflow. Review the evidence producer and retain its provenance.
An agent's claimed score is not independent outcome evidence. All shared
attribution gates still apply, including untouched changed files.

```bash wrap
gh aw value dependency-maintenance --repo owner/repo --json
```

The specialized schema-feature example uses a different evaluator:

```json title=".github/aw/value/schema-feature-coverage.json"
{
  "version": 1,
  "id": "schema-textual-coverage-v1",
  "name": "Schema fields with an example",
  "unit": "fields",
  "direction": "higher_is_better",
  "evaluator": "schema-example-coverage-v1",
  "config": {
    "schema": "pkg/parser/schemas/main_workflow_schema.json",
    "examples": [".github/workflows", "schema-demos"]
  }
}
```

`schema-example-coverage-v1` requires
unit `fields`, direction `higher_is_better`, a literal repository-relative
schema path, and 1–32 literal example directories. It can measure another
workflow with the same objective and different paths; it is not a universal
value function. Objectives not observable through these evaluators or the typed
evidence adapter below require a
reviewed native evaluator or evidence projection before they can be measured.

Definitions and recipes are strict JSON objects, limited to 512 KiB each.
Unknown versions, fields, evaluator IDs, and legacy `script` declarations fail
explicitly. There is no JavaScript fallback and no Node.js dependency for
`value`. Existing run-scoped `graders.operational-value.script` declarations
are unaffected.

The evaluator retains field identities, not just totals, so population churn
cannot masquerade as an unchanged-population improvement. An observed inventory
with no covered fields is zero; empty schemas, absent inventories, malformed
inputs, and missing history return errors. Definition SHA-256 digests identify
the exact declaration bytes; the versioned evaluator ID identifies its semantics.
Review intent, configuration, and fixtures together. Corrections are prospective.

## Typed evidence inputs

`evidence-number-v1` evaluates independently collected observations without
requiring them to be committed as numeric files. It does not collect or
authenticate the supplied evidence, execute workflow code, or validate the
underlying goal's correctness.

```json title=".github/aw/value/performance.json"
{
  "version": 1,
  "id": "compile-time-v1",
  "name": "Controlled compilation benchmark",
  "unit": "ns/op",
  "direction": "lower_is_better",
  "evaluator": "evidence-number-v1",
  "config": {
    "scope": "benchmark",
    "aggregation": "median",
    "minimum_samples": 5,
    "require_population": true
  }
}
```

```bash wrap
gh aw value performance --repo owner/repo \
  --value-function .github/aw/value/performance.json \
  --evidence observations.json --start 2026-09-01 --end 2026-10-31 --json
```

The bundle is strict JSON with `version: 1`, `repository: "HOST/OWNER/REPO"`,
and `observations`. It is limited to 512 KiB and 1024 observations.
Each observation contains:

| Field | Contract |
| --- | --- |
| `id`, `workflow`, `metric`, `scope`, `subject`, `run_id` | Unique observation ID, declared workflow/definition/scope, stable subject or experiment identity, positive producer/source run ID. |
| `at`, `observed_at` | Event and collection timestamps; event ≤ collection ≤ report collection time. |
| `phase` | `baseline`, `outcome`, or descriptive `sample`. |
| `coverage`, `reason` | `available`, `partial`, or `unavailable`; the latter two require a reason and no numeric values. |
| `references` | Nonempty source references. These retain provenance, not authentication. |
| `sha` | Full commit SHA required for available `git_snapshot` observations. |
| `population` | Optional unique subject IDs; required when configured. |
| `context` | Comparable measurement protocol/runner/toolchain/fixture identity; required for benchmarks. |
| `values` | Finite raw measurements; missing evidence is not an observed zero. |

Supported scopes are `git_snapshot`, `run`, `benchmark`, and `object_state`.
`scalar` requires exactly one independently measured number.
`median` aggregates repeated samples without treating them as proof of
statistical significance. `excess_lines` requires Git scope, population, and
nonnegative `maximum_lines`; it sums `max(0, count - maximum_lines)` across one
nonnegative integer newline count per file. A zero for an absent file requires
independently verified absence. `minimum_samples` is 1–10000; scalar requires 1,
and benchmark scope requires median aggregation.

Only observations matching the selected workflow, definition ID, scope, and
event-time report bounds reach the evaluator. There may be at most one baseline
and outcome per subject; duplicate endpoints fail rather than selecting a
favorable result. Changed population or context prevents change calculation.
Raw inputs and their clocks remain in `evidence.observations`.

This mode works without full Git history, but rejects `--ref` and PR
author/batch/witness recipe selectors. It records
`trust: "caller_supplied_not_native_adoption"` and **never assigns attributed
value**. `evidence.coverage: "available"` describes supplied measurements, not
verified human acceptance or full-window evidence. `window_mature` records only
elapsed time; temporal `sampling_coverage` remains `not_evaluated`.
The [reevaluated pilot](../value-pilot/#typed-evidence-reevaluation) includes an
executable bundle with real observations and explicit missing-evidence cases.

## Define a report recipe

A recipe belongs to the workflow owner's repository and selects its own cohort:

```json title=".github/aw/value/dependency-maintenance.review.json"
{
  "version": 1,
  "id": "dependency-maintenance-review-v1",
  "repository": "github.com/owner/repo",
  "workflow": "dependency-maintenance",
  "start": "2026-09-01",
  "end": "2026-10-31"
}
```

```bash wrap
gh aw value dependency-maintenance --repo owner/repo \
  --recipe .github/aw/value/dependency-maintenance.review.json --json
```

`repository` includes the host and must match the selected repository; `workflow`
must match the selected workflow. Optional `author` and `author_login` must be
provided together: the former filters search, and the latter verifies returned
PR authors. Optional `early_batches_through` groups early merges by UTC date;
`witnesses` lists individual PR numbers to inspect. Witnesses must exist in the
cohort and must not overlap those batches. Without these selections, every
individual merge remains available in `windows`.

### Window policy

The optional `window` object fixes a versioned metric-specific policy:

| Field | Default / bounds |
| --- | --- |
| `version`, `baseline` | `1`; `exact_parent` for Git replay, `paired_observation` for typed evidence. |
| `before_days`, `after_days` | 7, 7; each 1–365. |
| `outcome_lag_days`, `durability_days` | 0, 7; lag 0–365, durability 1–365 and at least the lag. |
| `sample_every_hours`, `trend_every_days` | 24, 7; hours divide 24; trend 1–365 days. |
| `minimum_before_samples`, `minimum_after_samples` | 7, 7; positive and attainable under the chosen cadence. |
| `isolation_before_days`, `isolation_after_days` | 7, 7; each 0–365. |
| `overlap` | `workflow`; alternatively `changed_files`, requiring exact changed-file evidence. |

At most 1024 combined pre/post sample slots are permitted. Partial overrides
retain other defaults, including sample minima; shorter windows may therefore
require explicit smaller minima.

**Seven days preserves the slides' original policy; it is not an empirically
validated universal window.** Choose periods from expected response delay,
workflow cadence, reversibility, and available independent measurements.
Benchmark replicates are separate from calendar sampling. Freeze policies
before future collection; describe policies chosen after observing a cohort as
retrospective candidates, not preregistered or validated choices.

In Git replay, pre samples cover `[merge - before, merge)`; post samples cover
`[merge + lag, merge + lag + after)`. Merge `after` remains the exact merge
snapshot; `outcome` records the lagged snapshot. Survival runs independently
through `merge + durability`. Attribution waits for the entire outcome,
durability, and forward-isolation horizons, not merely the minimum sample count.
Delayed aggregate changes remain descriptive: attribution requires subject
linkage not currently implemented for that path.

`workflow` overlap excludes neighboring producer-linked merges within the
declared isolation interval; `changed_files` excludes intersecting changed-file
sets instead. Missing file evidence stays unavailable. Longer outcome horizons
do not automatically widen global workflow exclusions. Typed evidence replay
records these policies but does not execute Git temporal sampling, overlap, or
survival checks.

## Shared measurement and attribution

Historical Git measurements require a full checkout whose `origin` matches the selected
repository and whose local main-history ref covers the final snapshot. Git and
an authenticated `gh` CLI with read access to that repository are required.
Shallow history, missing objects, stale refs, API failures, malformed producer
evidence, and incomplete search results return errors. The command does not
fetch history, dispatch workflows, run workflow code, or mutate lifecycle data.

| Report layer | Evidence and meaning |
| --- | --- |
| Outcome | Recipe-cadence first-parent main snapshots, plus the final snapshot; includes every author's work. |
| Producer-linked outputs | Pull requests with the exact workflow footer and producer run ID, created between the report's start and end instants. Search is not native delivery history. |
| Merge windows | Every cohort pull request merged by the cutoff, with exact-parent comparison and recipe-defined pre/post samples. |
| Attributed value | Workflow-specific improvement at an isolated merge that passes every shared gate below. |

Attribution requires a native delivered `create_pull_request` record matching the
host, repository, workflow, producer run, and exact pull request URL. Its available
adoption observation must record `explicit_human`, `human_verified: true`, and
the exact merge timestamp. Bot resolution and delivery fallbacks do not qualify.

Under the default seven-day policy, no other producer-linked merge of this
workflow may occur within seven days on either side, including
merges outside the selected delivery cohort. Every file changed by the merge must
remain untouched by all first-parent main commits through the complete seven-day
post-adoption window. A native survival **snapshot** cannot replace that history
check. This conservative rule also excludes benign later edits. All required
measurements must be available. Attribution currently supports human-adopted PR
merges; other output types retain their native lifecycle evidence without
invented longitudinal value.

Default windows are `[merge - 7 days, merge)` and `[merge, merge + 7 days)`, sampled daily
at the merge time. Day zero uses the exact merge commit. The end measurement and
file-survival check include `merge + 7 days`. Evidence after the report cutoff
is not used to complete a window; future samples and incomplete windows remain
`null` and `pending`.

Each window reports `attributed_value` as a number only when every gate passes.
An observed, eligible merge with no measured improvement returns `0`.
Missing proof, shared windows, or touched files return `null` with an explicit
reason, not zero. Top-level `attributed_value` is the **verified subtotal** in
`measurement.unit`, or `null` when no window qualifies. `attribution_coverage` distinguishes
`complete`, `partial`, and `unavailable`; a partial subtotal is not the workflow's
total value.

## Read the slides' example

The checked-in Go-native definition and recipe reproduce the slides' measurement
and selected-window policy:

```bash wrap
printf '%s\n' 36389625388 | gh aw logs --stdin --repo github/gh-aw --adoption --output .github/aw/logs
gh aw value schema-feature-coverage \
  --value-function .github/aw/value/schema-feature-coverage.json \
  --recipe .github/aw/value/schema-feature-coverage.slides.json --json
```

The recipe fixes the dates, original `app/github-actions` search author and
verified bot login, early same-day batches through 30 June, and three named
witnesses. `windows` retains every individual merge; `inspected_windows`
provides the six selected slide rows. Each row has seven daily pre samples
anchored at the first merge and seven post samples anchored at the last merge,
plus exact-parent before and full-window end observations. Closed/retired field
sets are deduplicated across batch members. Batches remain descriptive, never
attributed; individual witnesses pass the same shared attribution gates.
Missing witnesses fail rather than silently dropping a row.

It reads top-level `.properties` keys from
`pkg/parser/schemas/main_workflow_schema.json`. A field is covered when a
Markdown file below `.github/workflows/` or `schema-demos/`, recursively, has a
column-zero `<field>:` line. Code blocks count; examples are not compiled.
The stable-cohort section tracks fields present in every weekly snapshot.
This function additionally requires an unchanged schema population at merge
and counts previously missing fields covered by that merge.

The 17 November 2026 replay showed 44 producer-linked pull requests, 19 merges,
and weekly all-author coverage increasing from 32/45 fields to 75/75. Those
counts alone do not attribute that improvement to the workflow.

The 28 September witness changed coverage from 63/68 fields at its exact first
parent to 68/68 at merge. Its five changed demo files stayed untouched through
5 October despite 257 subsequent main commits. With the matching native human
adoption record and no neighboring workflow merge, it qualifies for **five
attributed fields**.

The recipe produces 36 weekly observations and six selected rows with 84 daily
samples for the original baseline. The report also inspects every cohort merge
individually. Additional native records can change which windows have enough
evidence, so the verified subtotal is not a hard-coded slide number.

The [18 November full-history follow-up](../value-pilot/#full-history-follow-up)
reproduced these counts and the five-field attribution witness through the CLI,
using complete `main` history and freshly collected native adoption evidence.

Retain the JSON report and source lifecycle records. The report records the
resolved Git SHA, evaluator ID, definition and recipe digests, and collected
observations. `--ref FULL_SHA` pins Git history, but live API evidence can still
change. Dates alone do not freeze provider evidence; offline API-evidence replay
is not currently implemented.

> [!IMPORTANT]
> This is a retrospective, event-aligned replay, not a prospectively adopted
> measurement contract or proof of exclusive causality. Textual presence does
> not establish working examples, fewer defects, or saved hours. Isolation is
> checked against discovered workflow merges, not every possible confounder.

## Command reference

```text
gh aw value <workflow> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--start` | 30 days before end | First snapshot date, `YYYY-MM-DD` at 12:00 UTC. |
| `--end` | Yesterday | Final snapshot date at 12:00 UTC; must be in the past and at or after start. Maximum range: 730 days. |
| `--ref` | `origin/main` | Local first-parent main-history ref, resolved once to a commit SHA. |
| `--logs-dir` | `.github/aw/logs` | Directory containing retained native `lifecycle.jsonl` evidence. |
| `--repo`, `-r` | Current repository | Source repository in `[HOST/]OWNER/REPO` format. |
| `--value-function` | Auto-detected | Definition path; otherwise `.github/aw/value/WORKFLOW.json`, then the schema-coverage built-in when applicable. |
| `--recipe` | None | JSON recipe selecting dates, optional author filter, early batches, and witnesses. Cannot be combined with `--start` or `--end`. |
| `--evidence` | None | Typed observation bundle for `evidence-number-v1` only; does not replace native lifecycle evidence. |
| `--timeout` | `5m` | Positive Go duration bounding Git and GitHub collection. |
| `--json`, `-j` | `false` | Emit the full report on stdout. |

JSON includes `schema_version`, `lifecycle`, `measurement_status`, and the
`measurement` definition (ID, name, unit, direction, evaluator, digest, host, repository,
and workflow identity), the pinned `ref`, `start`, `end`,
`observed_at`, resolved `window_policy`, `weekly`, `cohort`, `outputs`, `windows`, `attributed_value`,
`attribution_coverage`, and `limitations`. Snapshots include their commit SHA
and native-scale `value`; the adapter's `coverage` evidence includes field
population, missing fields, and covered count. Windows include ordered daily
`samples`, first-parent `before` and merge `after`, `end`, `change`, native
`adoption`, full-window `survival`, `status`, and `reason`.
`change.coverage` contains the `closed`, `added`, and `removed` field sets.
When a recipe is selected, `recipe` retains its definition/digest and
`inspected_windows` contains the selected batch and witness rows. Built-in
defaults have no external definition digest; pass the checked-in definition
explicitly when retaining replay provenance.
Git and PR event clocks are distinct from collection time; adoption records are
observations collected by the separate native lifecycle collector.
The historical `weekly`/`samples` field names remain stable even when the
recipe changes cadence. Typed replay adds `evidence` instead of Git snapshots.

See [Delivery and adoption](../delivery-adoption/) for the shared lifecycle
rules and [Measuring Impact](../../guides/measuring-impact/) for interpreting
cost, adoption, and longer-term outcomes together.
