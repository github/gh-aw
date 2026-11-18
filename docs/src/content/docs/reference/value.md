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

`lifecycle.coverage: "retained_subset"` means counts describe retained records,
not the workflow's complete history. Human acceptance and delivery fallbacks
remain separate. A missing ledger or no matching records produces unavailable
counts, not verified zeros. Adoption observations retain their original
collection clocks; the delivery cohort does not make them historical snapshots
of adoption at the report cutoff.

Without a value function, the command needs no Git history or measurement
runtime. It returns `measurement_status: "unavailable"`,
`reason: "measurement_not_configured"`, and `attributed_value: null`.
Only collect completed runs; no new execution or workflow dispatch is needed.

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
value function. Objectives not observable through either evaluator require a
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

## Shared measurement and attribution

Measurements require a full checkout whose `origin` matches the selected
repository and whose local main-history ref covers the final snapshot. Git and
an authenticated `gh` CLI with read access to that repository are required.
Shallow history, missing objects, stale refs, API failures, malformed producer
evidence, and incomplete search results return errors. The command does not
fetch history, dispatch workflows, run workflow code, or mutate lifecycle data.

| Report layer | Evidence and meaning |
| --- | --- |
| Outcome | Weekly first-parent main snapshots, plus the final snapshot; includes every author's work. |
| Producer-linked outputs | Pull requests with the exact workflow footer and producer run ID, created between the report's start and end instants. Search is not native delivery history. |
| Merge windows | Every cohort pull request merged by the cutoff, measured against its exact first parent, with seven daily samples before and seven after adoption. |
| Attributed value | Workflow-specific improvement at an isolated merge that passes every shared gate below. |

Attribution requires a native delivered `create_pull_request` record matching the
host, repository, workflow, producer run, and exact pull request URL. Its available
adoption observation must record `explicit_human`, `human_verified: true`, and
the exact merge timestamp. Bot resolution and delivery fallbacks do not qualify.

No other producer-linked merge of this workflow may occur within seven days on
either side, including
merges outside the selected delivery cohort. Every file changed by the merge must
remain untouched by all first-parent main commits through the complete seven-day
post-adoption window. A native survival **snapshot** cannot replace that history
check. This conservative rule also excludes benign later edits. All required
measurements must be available. Attribution currently supports human-adopted PR
merges; other output types retain their native lifecycle evidence without
invented longitudinal value.

Windows are `[merge - 7 days, merge)` and `[merge, merge + 7 days)`, sampled daily
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
| `--timeout` | `5m` | Positive Go duration bounding Git and GitHub collection. |
| `--json`, `-j` | `false` | Emit the full report on stdout. |

JSON includes `schema_version`, `lifecycle`, `measurement_status`, and the
`measurement` definition (ID, name, unit, direction, evaluator, digest, host, repository,
and workflow identity), the pinned `ref`, `start`, `end`,
`observed_at`, `weekly`, `cohort`, `outputs`, `windows`, `attributed_value`,
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

See [Delivery and adoption](../delivery-adoption/) for the shared lifecycle
rules and [Measuring Impact](../../guides/measuring-impact/) for interpreting
cost, adoption, and longer-term outcomes together.
