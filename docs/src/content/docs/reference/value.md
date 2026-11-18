---
title: Value report
description: Report shared delivery and adoption for any workflow, with workflow-specific value functions and conservative longitudinal attribution.
sidebar:
  order: 298
---

`gh aw value WORKFLOW` reports delivery and adoption for **any
workflow** using the same native records and rules. Only the value function
varies by workflow. Without a function, lifecycle evidence remains visible and
value is explicitly unavailable.

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

## Define the workflow-specific value function

Use the
[operational-value-designer skill](https://github.com/github/gh-aw/blob/main/.github/skills/operational-value-designer/SKILL.md)
to derive, validate, and freeze a function from workflow intent:

```text
/operational-value-designer owner/repo WORKFLOW longitudinal
```

The skill authors the function; the reporting command does not invoke an agent
or invent a metric. The same skill also supports run-scoped graders, whose
results remain separate from this report.

Save a reviewed definition in `.github/aw/value/WORKFLOW.json`, or
select it with `--value-function PATH`. For a workflow whose independently
maintained outcome inventory measures documented public APIs:

```json title=".github/aw/value/api-docs.json"
{
  "id": "documented-apis-v1",
  "name": "Documented public APIs",
  "unit": "APIs",
  "direction": "higher_is_better",
  "paths": ["metrics/api-coverage.json"],
  "script": "const text = snapshot.files['metrics/api-coverage.json']; if (text === undefined) return null; const data = JSON.parse(text); return Number.isFinite(data.covered) ? data.covered : null;"
}
```

The synchronous JavaScript body receives immutable `snapshot.sha`,
`snapshot.files` (names to exact UTF-8 contents), and optional `parameters`
from the definition. Return one finite native-scale number or `null`.
Missing or inapplicable evidence is `null`, not zero. Invalid results and script
failures return errors. The existing restricted grader worker provides the
isolation; this is a snapshot measurement, not a replayed session grader.

`paths` selects literal tracked files or directories, recursively. Limits are
32 paths, 512 regular files, and 8 MiB per snapshot; symlinks and submodules are
unsupported. Definitions are limited to 512 KiB and scripts to 256 KiB. No
network, process, environment, clock, random, direct filesystem access, or
input mutation is available. Node.js 20+ with permission support is required.

Use `parameters` to freeze the eligible population when denominator churn could
mimic improvement. Higher-is-better improvement is `after - before`;
lower-is-better improvement is `before - after`. Regressions remain negative.
Definition SHA-256 digests distinguish implementations. Review intent,
parameters, script, and fixtures together; corrections are prospective, not
rewrites of previous reports.

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

The built-in `schema-textual-coverage-v1` function reproduces the slides without
a custom definition:

```bash wrap
printf '%s\n' 36389625388 | gh aw logs --stdin --repo github/gh-aw --adoption --output .github/aw/logs
gh aw value schema-feature-coverage --start 2026-03-22 --end 2026-11-17 --json
```

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

The CLI inspects every cohort merge individually; the slides illustrated a
subset of windows and grouped some same-day merges. Additional native records
can change which windows have enough evidence, so the verified subtotal is not
a hard-coded slide number.

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
| `--timeout` | `5m` | Positive Go duration bounding Git and GitHub collection. |
| `--json`, `-j` | `false` | Emit the full report on stdout. |

JSON includes `schema_version`, `lifecycle`, `measurement_status`, and the
`measurement` definition (ID, name, unit, direction, digest, host, repository,
and workflow identity), the pinned `ref`, `start`, `end`,
`observed_at`, `weekly`, `cohort`, `outputs`, `windows`, `attributed_value`,
`attribution_coverage`, and `limitations`. Snapshots include their commit SHA
and native-scale `value`; the adapter's `coverage` evidence includes field
population, missing fields, and covered count. Windows include ordered daily
`samples`, first-parent `before` and merge `after`, `end`, `change`, native
`adoption`, full-window `survival`, `status`, and `reason`.
`change.coverage` contains the `closed`, `added`, and `removed` field sets.
Git and PR event clocks are distinct from collection time; adoption records are
observations collected by the separate native lifecycle collector.

See [Delivery and adoption](../delivery-adoption/) for the shared lifecycle
rules and [Measuring Impact](../../guides/measuring-impact/) for interpreting
cost, adoption, and longer-term outcomes together.
