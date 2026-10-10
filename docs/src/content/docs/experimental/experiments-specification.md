---
title: A/B Experiments Specification
description: Formal W3C-style specification for the GitHub Agentic Workflows A/B experiment system — frontmatter schema, variant selection, state persistence, expression integration, audit CLI, and statistical reporting.
sidebar:
  order: 220
---

# A/B Experiments Specification

**Version**: 1.0.0  
**Status**: Draft  
**Latest Version**: [experiments-specification](/gh-aw/experimental/experiments-specification/)  
**Editors**: gh-aw maintainers

---

## Abstract

This specification defines the A/B experiment system for GitHub Agentic Workflows.
It covers the `experiments:` frontmatter schema, variant selection algorithms, state persistence
backends, expression and template integration, activation job structure, audit CLI integration,
and statistical analysis requirements. Conforming implementations provide operators with a
zero-infrastructure mechanism to conduct controlled experiments on agentic workflow behavior
using only workflow frontmatter declarations, without any external service dependency.

This document consolidates and supersedes the normative sections of ADR-29534,
ADR-29618, ADR-29628, ADR-29985, and ADR-29996. It also incorporates corrective requirements
identified during an expert review of the implementation in May 2026.

---

## Status of This Document

This is a **Draft** specification. It may be updated, replaced, or made obsolete at any time.
A future revision will promote this document to Candidate Recommendation once the reference
implementation (gh-aw v1.x) satisfies all conformance requirements below.

Promotion from **Draft** to **Candidate Recommendation** requires all of the following:

1. **Reference implementation completeness**: 100% of normative requirements in §§4–12 are
   implemented in `gh-aw` and mapped to concrete implementation files (**tracking issue**:
   [#31983](https://github.com/github/gh-aw/issues/31983)).
2. **Compliance coverage**: At least 95% of normative requirements have automated tests, and
   all MUST/MUST NOT requirements have at least one passing automated test (**tracking issue**:
   [#31983](https://github.com/github/gh-aw/issues/31983)).
3. **CI stability window**: The experiments-related test suite passes on the default branch for
   30 consecutive days with no unresolved regression in variant selection, persistence, or
   reporting behavior (**tracking issue**: [#31983](https://github.com/github/gh-aw/issues/31983)).
4. **Interoperability evidence**: At least two production workflows using `experiments:` run for
   a minimum of 500 total assignments each with valid assignment artifacts and reproducible
   audit output (**tracking issue**: [#31983](https://github.com/github/gh-aw/issues/31983)).
5. **Review sign-off**: Written approval from at least two gh-aw maintainers that Sections 10–14
   are complete, internally consistent, and suitable for Candidate Recommendation publication
   (**tracking issue**: [#31983](https://github.com/github/gh-aw/issues/31983)).

### Sync

- **Who reviews**: The experiments specification editors (`gh-aw maintainers`) perform the
  primary review; one release owner for the current minor version performs final sign-off.
- **When**: Review occurs on the first business day of each month and during every minor-release
  cut.
- **What triggers an immediate sync update**:
  1. Any change to `experiments:` schema fields or validation behavior (§4)
  2. Any change to variant selection, gating, or persistence logic (§§5–7)
  3. Any change to audit/reporting output contracts (§§10–11)
  4. Any incident postmortem that identifies spec/implementation drift

When a trigger occurs, spec updates **SHOULD** be merged in the same PR as the implementation
change or in a linked follow-up PR within 3 business days.

Feedback should be filed as GitHub issues against the `github/gh-aw` repository with the
`experiments` label.

---

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Definitions](#3-definitions)
4. [Frontmatter Schema](#4-frontmatter-schema)
5. [Variant Selection Algorithms](#5-variant-selection-algorithms)
6. [Date-Range Gating](#6-date-range-gating)
7. [State Persistence](#7-state-persistence)
8. [Expression and Template Integration](#8-expression-and-template-integration)
9. [Activation Job Structure](#9-activation-job-structure)
10. [Audit CLI Integration](#10-audit-cli-integration)
11. [Statistical Analysis and Reporting](#11-statistical-analysis-and-reporting)
12. [Simultaneous Experiments and Interaction Effects](#12-simultaneous-experiments-and-interaction-effects)
13. [Security Considerations](#13-security-considerations)
14. [Compliance Testing](#14-compliance-testing)
15. [References](#15-references)
16. [Appendices](#appendices)
17. [Change Log](#change-log)

---

## 1. Introduction

### 1.1 Purpose

Agentic workflows compiled by gh-aw use a frontmatter-driven configuration model.
Teams running these workflows need a first-class mechanism to test different prompt variants
(tone, verbosity, persona, feature flags embedded in the prompt) across successive workflow runs.
Without such a mechanism, variant testing is ad-hoc, untracked, and statistically unbalanced.

This specification defines a self-contained A/B experiment system that requires no external
service, no manual coordination, and no changes outside the workflow frontmatter.

### 1.2 Scope

This specification covers:

- The `experiments:` frontmatter schema and its two syntactic forms (bare-array, rich-object).
- Variant selection algorithms: balanced round-robin (least-used), weighted random, and date-gated fallback.
- State persistence backends: git-branch (`repo`) and GitHub Actions cache (`cache`).
- Expression and Handlebars template integration in the compiled workflow prompt.
- The activation job structure generated by the compiler.
- The `gh aw audit` CLI filtering interface for experiment-annotated runs.
- Requirements for statistical analysis and reporting workflows that consume experiment artifacts.

This specification does **not** cover:

- The internal compiler architecture beyond what is observable at the compiled YAML boundary.
- External analytics dashboards or third-party experiment platforms.
- Multi-armed bandit or adaptive allocation algorithms (considered future work).

### 1.3 Design Goals

1. **Zero external dependencies** — all state is stored within the repository or GitHub Actions infrastructure.
2. **Declarative** — the complete experiment configuration lives in the workflow frontmatter.
3. **Backward compatible** — adding `experiments:` to an existing workflow MUST NOT break any existing compiled output; removing it MUST restore the original output exactly.
4. **Statistically sound** — the default selection algorithm guarantees approximate variant balance in the minimum number of runs.
5. **Observable** — every run produces a durable artifact recording the variant assignment, and OTEL attributes propagate assignments to distributed-tracing backends automatically.

---

## 2. Conformance

### 2.1 Requirements Notation

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**,
**SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and **OPTIONAL** in this document
are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### 2.2 Conformance Classes

This specification defines three conformance classes:

| Class | Requirements |
|---|---|
| **Level 1 — Basic** | Satisfies all MUST/MUST NOT requirements in §4, §5, §8, and §9 |
| **Level 2 — Standard** | Level 1 plus §6 (date gating), §7 (state persistence), §10 (audit CLI) |
| **Level 3 — Complete** | Level 2 plus §11 (statistical analysis and reporting) and §12 (simultaneous experiments) |

An implementation is considered **non-conformant** if it fails any MUST or MUST NOT requirement
at the level it claims to implement.

### 2.3 Normative vs. Informative Content

Sections containing numbered requirements (e.g., "R-SCHEMA-001") are **normative**.
Notes, rationale blocks, and appendices are **informative** and carry no conformance weight.

---

## 3. Definitions

| Term | Definition |
|---|---|
| **Experiment** | A named A/B test declared in workflow frontmatter, associating an identifier with two or more variant strings. |
| **Variant** | A named string value representing one treatment arm in an experiment. |
| **Control variant** | The first variant in the declared `variants` array; used as baseline and as fallback during date gating. |
| **Invocation counter** | A per-experiment, per-variant integer stored in `state.json` that records the cumulative number of times a variant has been selected. |
| **state.json** | The JSON file that stores invocation counters and per-run assignment history for all experiments in a single workflow. |
| **Run record** | An entry in the `state.json` `runs` array recording the run ID, timestamp, and variant assignments for one workflow run. |
| **Sanitized workflow ID** | The workflow basename (without `.md`) with hyphens removed and lowercased, used as a cache/branch key component. |
| **Activation job** | The `activation` GitHub Actions job generated by the compiler that picks variants and exposes them to downstream jobs. |
| **Experiment artifact** | A GitHub Actions artifact named `{sanitizedID}-experiment` uploaded by the activation job and containing `state.json` and `assignments.json`. |
| **assignments.json** | A file in the experiment artifact containing only the current run's variant assignments as a flat JSON object. |

---

## 4. Frontmatter Schema

### 4.1 Field Declaration

**R-SCHEMA-001**: Workflow frontmatter **MAY** include an `experiments` field. Its absence
**MUST** produce no change in compiled output.

**R-SCHEMA-002**: The value of `experiments` **MUST** be a YAML map. Non-map values
**MUST** be rejected at compile time with a descriptive error.

**R-SCHEMA-003**: Every key in the `experiments` map, except the reserved `storage` key
(§7.1) and `identity` key (§4.1.1), **MUST** be an experiment name that matches the regular expression
`^[a-zA-Z_][a-zA-Z0-9_]*$`. Keys that do not match **MUST** be silently skipped with a
compile-time warning emitted to stderr.

#### 4.1.1 Experiment History Identity

The reserved `identity` key **MUST** accept one of `full`, `frontmatter`, or
`workflow`. If omitted or unrecognized, implementations **MUST** use
`frontmatter`.

| Value | Fingerprint |
|---|---|
| `full` | Compiler freshness hash and complete workflow body hash |
| `frontmatter` | SHA-256 of normalized workflow frontmatter text, excluding the markdown body |
| `workflow` | Workflow ID derived from the workflow filename |

The default `frontmatter` mode **MUST** preserve history across body-only edits,
including workflows that inline imports or use body template expressions.
`full` **MUST** create a new identity when the workflow body changes.
`workflow` **MUST** preserve identity across frontmatter and body edits to the
same workflow ID. Identity selection **MUST NOT** change the immutable source
revision used to route work to a worker.

> **Note (informative)**: The identifier pattern ensures experiment names can be used as
> GitHub Actions step output names and embedded in `${{ experiments.<name> }}` expressions
> without bracket notation.

### 4.2 Bare-Array Form

**R-SCHEMA-004**: Each experiment value **MAY** be declared as a YAML sequence of two or
more strings:

```yaml
experiments:
  prompt_style: [concise, detailed]
```

**R-SCHEMA-005**: A bare-array value with fewer than two entries **MUST NOT** be accepted;
the compiler **MUST** emit a compile-time error.

### 4.3 Rich Object Form

**R-SCHEMA-006**: Each experiment value **MAY** alternatively be declared as a YAML object
with a required `variants` field and optional metadata fields. The two forms **MUST** be
accepted in the same `experiments` map without conflict.

**R-SCHEMA-007**: The `variants` field **MUST** be an array of two or more non-empty strings.
The same minimum-two-variants constraint from R-SCHEMA-005 applies.

**R-SCHEMA-008**: The following optional fields are defined for the object form:

| Field | Type | Description |
|---|---|---|
| `description` | string | Human-readable explanation of what the experiment tests. |
| `hypothesis` | string | Null and alternative hypothesis statements. |
| `metric` | string | Primary metric name to observe (e.g., `aic`), or an eval reference (`eval:<id>` / `evals.<id>`) when using `evals:`, or a grader reference (`grader:<id>` / `graders.<id>`) when using `graders:`. |
| `secondary_metrics` | string[] | Additional metrics to collect. |
| `guardrail_metrics` | object[] | Thresholds that must not degrade. Each object has `name` (string), `threshold` (string or number), and optional `direction` (`"min"`\|`"max"`) (see §4.4). |
| `min_samples` | integer ≥ 1 | Minimum runs per variant before analysis is reliable. Defaults to 20. |
| `weight` | integer[] | Per-variant probability weights (see §5.2). |
| `issue` | integer ≥ 1 | GitHub issue number tracking this experiment. |
| `start_date` | string (YYYY-MM-DD) | Experiment is inactive before this date (see §6). |
| `end_date` | string (YYYY-MM-DD) | Experiment is inactive after this date (see §6). |
| `analysis_type` | string enum | Statistical test for automated reporting (see §11.2). |
| `decision` | object | Deterministic interpretation policy (see §11.6). |
| `tags` | string[] | Free-form labels for dashboard filtering. |
| `notify` | object | Significance-alert destination (see §4.5). |

**R-SCHEMA-009**: The `weight`, `issue`, `min_samples`, `start_date`, `end_date`, `analysis_type`,
`tags`, and `notify` fields carry no effect on variant assignment outside their documented
subsections. `description`, `hypothesis`, `metric`, `secondary_metrics`, and `tags` are
purely informative at runtime.

**R-SCHEMA-010**: Implementations **MUST NOT** introduce additional properties in the object
form without a corresponding schema update; the compiler **MUST** reject unknown keys under
strict mode.

### 4.4 Guardrail Metrics

**R-SCHEMA-011**: Each entry in `guardrail_metrics` **MUST** be an object with the required
fields `name` and `threshold`, and the optional field `direction`. The fields are defined as
follows:

- `name` (string, required) — the metric identifier to guard (e.g. `"success_rate"`, `"empty_output_rate"`).
- `threshold` (string **or** number, required) — either a comparison expression matching
  `^(>=|<=|==|>|<)-?\d+(\.\d+)?$` (e.g. `">=0.95"`, `"==0"`, `"<=0.05"`) **or** a bare
  numeric value (e.g. `0.0`) when paired with a `direction` field.
- `direction` (`"min"` | `"max"`, optional) — the optimization direction for the metric.
  Use `"min"` when lower values are better (e.g. error rates, latency) and `"max"` when
  higher values are better (e.g. success rates, quality scores).

When `direction` is present with a bare numeric `threshold`, reporting tooling (§11.5) **MUST**
interpret the threshold as the acceptable boundary: for `direction: min` the observed value
**MUST** be ≤ threshold; for `direction: max` the observed value **MUST** be ≥ threshold.
When `threshold` is a comparison string (e.g. `">=0.95"`), `direction` is informative only
and the explicit comparison operator governs evaluation.

**R-SCHEMA-012**: Guardrail evaluation is **INFORMATIVE** at the schema level — the compiler
does not enforce guardrails at compile time. Reporting tooling (§11) **MUST** evaluate each
guardrail and include pass/fail status in its output.

### 4.5 Notify Object

**R-SCHEMA-013**: The `notify` object **MUST** contain only the keys `discussion` and/or
`issue`, each of which **MUST** be a positive integer (minimum 1). Unknown keys in `notify`
**MUST** be rejected by schema validation.

**R-SCHEMA-014**: When `notify.issue` is set and the reporting workflow posts a comment to
that issue, the compiled workflow **MUST** declare `permissions: issues: write`. Implementations
that generate reporting workflows **MUST** automatically add this permission when `notify.issue`
is present in any experiment configuration within the scope of that workflow.

> **Note (informative)**: Failure to include `issues: write` causes comment posting to
> silently fail with a 403 response. This was identified as a defect in the
> `daily-experiment-report` workflow (May 2026 review).

---

## 5. Variant Selection Algorithms

### 5.1 Balanced Round-Robin (Least-Used)

**R-SELECT-001**: When `weight` is absent or invalid (§5.2), implementations **MUST** select
the variant with the lowest cumulative invocation count stored in `state.json`.

**R-SELECT-002**: When two or more variants share the lowest count — including the initial
state where all counts are zero — implementations **MUST** break ties by selecting uniformly
at random from the tied variants. No variant **MUST** be systematically favoured by position.

**R-SELECT-003**: After selecting a variant via round-robin, implementations **MUST** increment
the invocation counter for that variant in `state.json` before persisting state.

> **Note (informative)**: Round-robin guarantees that over K×N runs each variant appears
> approximately N times, achieving balance in far fewer runs than random selection. The
> random tie-breaking on the first run ensures no variant is systematically advantaged.

### 5.2 Weighted Random Selection

**R-SELECT-004**: When `weight` is provided and its length equals the length of `variants`,
implementations **MUST** use weighted random selection: each variant is chosen with probability
proportional to its weight value.

**R-SELECT-005**: When all weight values are zero, implementations **MUST** return the control
variant (first entry in `variants`) without erroring.

**R-SELECT-006**: Weighted random selection **MUST** increment the invocation counter for the
selected variant before persisting state.

> **Note (normative correction)**: ADR-29618 Rule 9 incorrectly stated that weighted selection
> "MUST NOT increment any variant counter." This rule is hereby superseded. Counter increments
> for weighted selection are required to enable `min_samples` progress tracking and accurate
> per-run history. The reference implementation (`pick_experiment.cjs`) already implements
> this correct behavior by calling `recordVariant` unconditionally after both selection paths.

**R-SELECT-007**: When `weight` is provided but its length does not equal the length of
`variants`, implementations **MUST** treat `weight` as absent and fall back to round-robin
selection (R-SELECT-001).

> **Note (statistical, informative)**: Standard power calculations assume balanced allocations.
> When weights are non-uniform (e.g., `[70, 30]`), the effective sample size is reduced.
> The `min_samples` target should be interpreted as the minimum required for the **smaller
> group**. For a 70/30 split, experimenters should set `min_samples` to the desired count for
> the 30% arm and expect the 70% arm to accumulate proportionally more observations.

### 5.3 Variant Exposure

**R-SELECT-008**: Implementations **MUST** expose each selected variant as a named step output
`steps.pick-experiment.outputs.<experiment-name>` and **MUST** also set a combined JSON step
output `steps.pick-experiment.outputs.experiments` containing all variant assignments as a
serialized JSON object.

**R-SELECT-009**: Experiment names **MUST** be sorted alphabetically when building the
`experiments` JSON output to produce deterministic, reproducible output across runs with
identical state.

---

## 6. Date-Range Gating

**R-DATE-001**: When `start_date` is provided and the current date (UTC, `YYYY-MM-DD` format)
is strictly before `start_date`, implementations **MUST** return the control variant without
incrementing any counter.

**R-DATE-002**: When `end_date` is provided and the current date (UTC, `YYYY-MM-DD` format)
is strictly after `end_date`, implementations **MUST** return the control variant without
incrementing any counter.

**R-DATE-003**: Date comparison **MUST** use UTC date. Local timezone offsets **MUST NOT**
affect the result.

**R-DATE-004**: When both `start_date` and `end_date` are provided and the current UTC date is
within the inclusive range `[start_date, end_date]`, the experiment is active and normal
variant selection (§5) applies.

**R-DATE-005**: If `start_date` or `end_date` do not match the `YYYY-MM-DD` pattern,
implementations **SHOULD** treat them as absent (ignore silently) rather than hard-failing,
to preserve forward compatibility.

---

## 7. State Persistence

### 7.1 Storage Configuration

**R-STORE-001**: The `experiments:` map **MUST** support a reserved `storage` key whose value
is one of `"repo"` (default) or `"cache"`. Any other value **MUST** produce a compile-time warning
and fall back to `"repo"`.

**R-STORE-002**: When `storage` is absent, implementations **MUST** behave as if
`storage: repo` was specified.

**R-STORE-003**: The `storage` key **MUST NOT** be treated as an experiment name; it **MUST** be
excluded from experiment configuration extraction.

### 7.2 `state.json` Format

**R-STORE-004**: The `state.json` file **MUST** be a valid JSON object with the following
top-level structure:

```json
{
  "counts": {
    "<experiment_name>": {
      "<variant>": <integer>
    }
  },
  "runs": [
    {
      "run_id": "<string>",
      "timestamp": "<ISO-8601 UTC string>",
      "assignments": {
        "<experiment_name>": "<variant>"
      }
    }
  ]
}
```

**R-STORE-005**: The `runs` array **MUST** be pruned to at most 512 entries (keeping the most
recent) to prevent unbounded growth.

**R-STORE-006**: When loading a `state.json` that has no `runs` field (legacy format),
implementations **MUST** initialize `runs` to an empty array and continue normally.

**R-STORE-007**: When at least one experiment is assigned on a run, implementations **MUST**
append one run record to `state.runs` before persisting. Each record **MUST** contain:
- `run_id`: the value of `GITHUB_RUN_ID`, or `""` when absent.
- `timestamp`: an ISO-8601 UTC timestamp of the selection moment.
- `assignments`: an object mapping each assigned experiment name to its selected variant.

**R-STORE-008**: When no experiments are assigned (e.g., all experiments are outside their
date window), implementations **MUST NOT** append a run record or rewrite `state.json`.

### 7.3 `repo` Storage Mode

**R-STORE-REPO-001**: When `storage: repo` is active, the activation job **MUST** load
experiment state by fetching `state.json` from the git branch named
`experiments/{sanitizedWorkflowID}` via the GitHub REST API (GET /repos/{owner}/{repo}/contents/{path}).

**R-STORE-REPO-002**: A 404 response (branch or file does not exist) **MUST** be treated as an
empty initial state; the activation job **MUST NOT** fail.

**R-STORE-REPO-003**: After the activation job completes, a dedicated `push_experiments_state`
job **MUST** be generated. This job **MUST**:
- Download the experiment artifact from the current run.
- Commit the updated `state.json` and `assignments.json` to the experiments git branch.
- Declare `permissions: contents: write`.
- Be listed as a dependency of the conclusion job to ensure state is persisted before the
  workflow terminates.

**R-STORE-REPO-004**: The commit **SHOULD** be made via the GitHub GraphQL
`createCommitOnBranch` mutation (producing a verified, signed commit). A plain `git push`
**MAY** be used as a fallback when the GraphQL mutation is unavailable.

**R-STORE-REPO-005**: The push step **SHOULD** implement retry logic with exponential backoff
(minimum 3 attempts, base delay ≥ 1 second) to handle transient API failures and concurrent
push conflicts.

> **Note (race condition, informative)**: When two workflow runs start concurrently, both will
> read the same `state.json` from the branch before either has committed its update. Both runs
> will therefore select the same least-used variant. The retry logic in R-STORE-REPO-005
> handles write conflicts at push time but does not prevent duplicate variant selections at
> read time. On low-frequency workflows (daily cron) this is effectively never a problem.
> On high-frequency workflows (hourly or per-commit), experimenters should account for a
> small probability of temporarily imbalanced runs. A future revision of this specification
> **MAY** address this with an optimistic-concurrency guard at the fetch step.

### 7.4 `cache` Storage Mode

**R-STORE-CACHE-001**: When `storage: cache` is explicitly set, the activation job **MUST**
restore experiment state from GitHub Actions cache using a key of the form
`experiments-{sanitizedWorkflowID}-{GITHUB_RUN_ID}` and a restore-key prefix
`experiments-{sanitizedWorkflowID}-`.

**R-STORE-CACHE-002**: The activation job **MUST** save experiment state back to cache after
variant selection using `if: always()`.

**R-STORE-CACHE-003**: When `storage: cache` is active, no `push_experiments_state` job
**SHALL** be generated.

**R-STORE-CACHE-004**: Implementations **MUST NOT** require `contents: write` permission when
`storage: cache` is configured.

> **Note (informative)**: GitHub Actions cache has a 7-day inactivity eviction policy. State
> accumulated during an experiment may be silently lost over holidays or between infrequent
> runs. For this reason `repo` is the default storage mode. Use `cache` only when
> `contents: write` cannot be granted to the workflow.

### 7.5 Experiment Artifact

**R-STORE-ARTIFACT-001**: The activation job **MUST** upload the experiment state directory
as a GitHub Actions artifact named `{sanitizedWorkflowID}-experiment` (or `experiment` for
`workflow_call` triggers) with `if: always()` and a retention period of at least 30 days.

**R-STORE-ARTIFACT-002**: When `assignments.json` exists in the state directory, it **MUST**
be included in the artifact alongside `state.json`.

---

## 8. Expression and Template Integration

### 8.1 Compiler Expression Rewriting

**R-EXPR-001**: The compiler **MUST** rewrite every `${{ experiments.<name> }}` expression
in the frontmatter or prompt source to `steps.pick-experiment.outputs.<name>` during the
expression extraction phase, so the runtime value is injected by the GitHub Actions expression
engine.

**R-EXPR-002**: Each experiment **MUST** be mapped to an environment variable named
`GH_AW_EXPERIMENTS_<NAME>` (uppercased) that resolves to `steps.pick-experiment.outputs.<name>`.
This environment variable **MUST** be set in every workflow step that performs prompt
interpolation or template substitution.

### 8.2 Handlebars Template Integration

**R-EXPR-003**: Implementations **MUST** substitute `__GH_AW_EXPERIMENTS_<NAME>__`
placeholders in the raw prompt text **before** Handlebars template rendering, so that
`{{#if experiments.<name> == "value" }}` conditionals evaluate the actual runtime variant.

**R-EXPR-004**: Implementations **MUST NOT** pass raw `__GH_AW_EXPERIMENTS_*__` placeholders
to the Handlebars rendering engine; all substitutions **MUST** occur in a prior step.

**R-EXPR-005**: The `isTruthy` helper used in Handlebars conditionals **MUST** treat the
string `"no"` as falsy, in addition to the standard falsy values `""`, `"false"`, `"0"`,
`undefined`, and `null`. This enables yes/no flag experiments where
`{{#if experiments.feature }}` evaluates to false when the `no` variant is active.

> **Note (informative)**: The `"no"` falsy behavior is a deliberate design choice that enables
> simple boolean-flag experiments (`feature: [yes, no]`). It differs from standard JavaScript
> truthiness and should be clearly documented for contributors.

---

## 9. Activation Job Structure

**R-JOB-001**: When the `experiments` field is present in the frontmatter, the compiled
activation job **MUST** include the experiment steps defined in §9.1 or §9.2 as appropriate.

**R-JOB-002**: Implementations **MUST NOT** inject experiment steps into workflows that do
not declare the `experiments` frontmatter field.

**R-JOB-003**: The activation job **MUST** expose a `needs.activation.outputs.experiments`
output containing the full JSON variant assignment object so that downstream jobs can
reference it via `needs.activation.outputs.experiments`.

### 9.1 `cache` Storage Step Order

When `storage: cache`, the activation job **MUST** include the following steps in order:

1. **Restore experiment state** — `actions/cache/restore` with the workflow-specific key.
2. **Pick experiment variants** — `pick_experiment.cjs` via `actions/github-script`.
3. **Save experiment state** — `actions/cache/save` with `if: always()`.
4. **Upload experiment artifact** — `actions/upload-artifact` with `if: always()`.

### 9.2 `repo` Storage Step Order

When `storage: repo` (default), the activation job **MUST** include the following steps in order:

1. **Restore experiment state from git** — `load_experiment_state_from_repo.cjs` via `actions/github-script`.
2. **Pick experiment variants** — `pick_experiment.cjs` via `actions/github-script`.
3. **Upload experiment artifact** — `actions/upload-artifact` with `if: always()`.

A separate `push_experiments_state` job (R-STORE-REPO-003) commits the updated state after
the activation job completes.

### 9.3 OTEL Resource Attributes

**R-JOB-004**: After variant selection, when at least one experiment is assigned,
`pick_experiment.cjs` **MUST** call `core.exportVariable("OTEL_RESOURCE_ATTRIBUTES", …)`
with key-value pairs of the form `experiment.<name>=<variant>`, comma-separated when multiple
experiments are active.

**R-JOB-005**: When `OTEL_RESOURCE_ATTRIBUTES` is already set, implementations **MUST** append
the experiment attributes to the existing value with a comma separator rather than overwriting it.

**R-JOB-006**: When no experiments are assigned, implementations **MUST NOT** modify
`OTEL_RESOURCE_ATTRIBUTES`.

---

## 10. Audit CLI Integration

### 10.1 Filter Flags

**R-AUDIT-001**: The `gh aw audit` command **MUST** accept an `--experiment <name>` flag that
filters runs to those with a variant assignment for the named experiment.

**R-AUDIT-002**: The `gh aw audit` command **MUST** accept a `--variant <value>` flag that,
when combined with `--experiment`, further restricts results to runs assigned that exact
variant value.

**R-AUDIT-003**: `--variant` used without `--experiment` **MUST** cause a non-zero exit code
with an error message that includes a suggestion to add `--experiment`.

**R-AUDIT-004**: When a run is skipped by the filter, an informational message **MUST** be
emitted to stderr identifying the run ID, the experiment name, and (when applicable) the
required variant.

### 10.2 Run Overview Display

**R-AUDIT-005**: The run Overview section **MUST** include an `Experiment` field when the
run's experiment artifact contains one or more assignments.

**R-AUDIT-006**: The experiment label **MUST** be formatted as a comma-separated,
alphabetically sorted list of `name=variant` pairs (e.g., `caveman=yes, style=concise`).

**R-AUDIT-007**: The `Experiment` field **MUST** be omitted from console and JSON output when
no experiment assignments are present (`omitempty` semantics).

### 10.3 Per-Run Assignment Lookup

**R-AUDIT-008**: When `state.runs` is non-empty and the last record's `assignments` map is
non-empty, the audit reporter **MUST** use that record's assignments directly as the current-run
experiment data.

**R-AUDIT-009**: When `state.runs` is empty, absent, or the last record's `assignments` map is
empty, the audit reporter **MUST** fall back to the max-count heuristic: the variant with the
highest cumulative count is assumed to have been selected on the most recent run; ties are
broken by sorted variant order.

### 10.4 Filter Application

**R-AUDIT-010**: Implementations **MUST** apply the experiment/variant filter before calling
any report-rendering code. A filtered-out run **MUST** return `nil`, not an error.

**R-AUDIT-011**: Implementations **MUST** apply the filter in both the cached-summary path and
the fresh-processing path for consistent behavior.

**R-AUDIT-012**: Implementations **SHOULD** extract experiment data at most once per
`AuditWorkflowRun` invocation to avoid redundant artifact reads.

**R-AUDIT-013**: When neither `--experiment` nor `--variant` is set, implementations **MUST NOT**
read the experiment artifact solely for filtering purposes.

---

## 11. Statistical Analysis and Reporting

This section applies to the **Level 3 — Complete** conformance class (§2.2) and to any
automated workflow that reports on experiment outcomes.

### 11.1 Per-Run Assignment Source

**R-STAT-001**: Reporting tools that consume `state.json` files **MUST** derive per-run variant
assignments from the `state.runs` array when it is present and non-empty.

**R-STAT-002**: Reporting tools **MUST NOT** use the cumulative-count delta inference method
(comparing consecutive snapshots) as the primary assignment source when `state.runs` is
available. The delta method **MAY** be used as a fallback for legacy state files with no
`runs` array.

> **Note (informative)**: The delta method is fragile — it fails when multiple runs complete
> between downloaded snapshots, when runs are cancelled before the experiment step, or when
> `state.json` is fetched from different points in the artifact history. The `runs` array,
> introduced in v1.1.0 (ADR-29985), provides exact, auditable per-run assignment records.

### 11.2 Statistical Tests

**R-STAT-003**: When `analysis_type` is declared for an experiment, reporting tools **SHOULD**
use the specified test for significance analysis:

| `analysis_type` value | Test to apply |
|---|---|
| `t_test` | Welch's two-sample t-test (does not assume equal variance) |
| `mann_whitney` | Mann-Whitney U non-parametric rank test |
| `proportion_test` | Two-proportion z-test |
| `bayesian_ab` | Bayesian A/B analysis (posterior probability of superiority) |

**R-STAT-004**: When `analysis_type` is absent, reporting tools **SHOULD** default to the
two-proportion z-test for binary outcomes (success/failure) and Welch's t-test for continuous
metrics (e.g., duration).

### 11.3 Multiple Comparison Correction

**R-STAT-005**: When an experiment declares K ≥ 3 variants and reporting tools perform
pairwise comparisons against the control, the significance threshold **SHOULD** be adjusted
using the Bonferroni correction: `α_adjusted = 0.05 / (K − 1)`.

> **Note (informative)**: Without correction, the probability of at least one false positive
> across K−1 pairwise tests at α = 0.05 is approximately 1 − (1 − 0.05)^(K−1). For K = 3
> this is ~9.75%; for K = 5 it exceeds 18%. The Bonferroni correction is conservative but
> simple. The Holm-Bonferroni step-down procedure is a less conservative alternative.

**R-STAT-006**: When a multiple-comparison correction is applied, reporting tools **MUST**
state the correction method and the adjusted α threshold in the report output.

### 11.4 Minimum Sample Size Gate

**R-STAT-007**: Reporting tools **MUST NOT** issue a PROMOTE decision for any variant
until all variants in the experiment have accumulated at least `min_samples` usable observations
(or 20 if `min_samples` is not declared). When any variant is below threshold, readiness **MUST**
be `COLLECTING`. For an otherwise supported two-variant decision, the decision **MUST** be `EXTEND`.
The multi-variant limitation in R-STAT-016 takes precedence for experiments with more than two
variants.

**R-STAT-008**: When weights are non-uniform (§5.2), the `min_samples` target applies to the
**smallest expected group**. For a `weight: [70, 30]` experiment with `min_samples: 30`, the
control arm is not eligible for analysis until the 30% arm has at least 30 observations,
even if the 70% arm has accumulated many more.

### 11.5 Guardrail Evaluation

**R-STAT-009**: Reporting tools that evaluate `guardrail_metrics` **MUST** emit a failed
guardrail status for any variant that violates a declared threshold, and **MUST** override the
decision to REJECT regardless of the primary-metric p-value.

**R-STAT-010**: Multi-variant experiments **MUST** show guardrail pass/fail status per variant,
not aggregated across the experiment.

**R-STAT-013**: When evaluating a guardrail entry that carries a bare numeric `threshold`
(i.e. `threshold` is a number, not a comparison string), reporting tools **MUST** consult the
`direction` field to determine the pass condition:

- `direction: min` — the metric value **MUST** be ≤ threshold (lower is better; the guardrail
  catches regressions that push the value above the limit).
- `direction: max` — the metric value **MUST** be ≥ threshold (higher is better; the guardrail
  catches regressions that push the value below the limit).

When `threshold` is a comparison string (e.g. `">=0.95"`), the explicit operator in the string
governs the comparison. The `direction` field, if present, is treated as informative metadata
and **MUST NOT** alter the comparison logic.

> **Note (informative)**: The `direction` + numeric threshold form is preferred when generating
> experiment configs programmatically (e.g. from a generator agent), because it separates the
> numeric bound from the comparison direction, making both machine-readable and human-readable.

### 11.6 Deterministic Decision Policy

**R-STAT-014**: An experiment MAY declare a `decision` object with these fields:

| Field | Type | Default | Meaning |
|---|---|---|---|
| `minimum_effect` | number ≥ 0 | `0` | Minimum absolute primary-metric improvement required for promotion. |
| `regression_tolerance` | number ≥ 0 | `minimum_effect` | Maximum absolute primary-metric regression tolerated before rejection. |
| `confidence` | number, 0 < value < 1 | `0.95` | Required evidence probability. |

**R-STAT-015**: Decision effects are absolute and use the primary metric's units. Reporting tools
MUST normalize direction so a positive normalized effect means the candidate is better. For
frequentist methods, sufficient evidence means `p <= 1-confidence`. For `bayesian_ab`, tools
MUST use the analyzer's probability of superiority and MUST NOT convert it to a p-value.

**R-STAT-016**: A deterministic two-variant decision MUST apply this precedence:

1. Fewer than `min_samples` usable observations: `EXTEND`.
2. Missing or insufficient mandatory guardrail observations: `EXTEND`.
3. Failed mandatory guardrail: `REJECT`.
4. Material candidate regression with sufficient evidence: `REJECT`.
5. Material candidate improvement with sufficient evidence: `PROMOTE`.
6. Otherwise: `INCONCLUSIVE`.

Statistical significance without the configured practical effect MUST NOT produce `PROMOTE`.
Reporting tools MUST return `INCONCLUSIVE` with `unsupported_multi_variant` for automatic decisions
with more than two variants, regardless of readiness. Statistical comparisons MAY still be reported
with the multiple-comparison correction in §11.3, but the decision layer does not rank or select a
winner.

**R-STAT-017**: The decision layer MUST consume existing analysis results. It MUST NOT fetch
observations, rerun statistical tests or graders, mutate experiment state, change traffic, or
promote workflow sources.

**R-STAT-018**: Structured analysis output MUST expose readiness independently from decision.
Readiness is `COLLECTING` until every variant reaches `min_samples` usable observations, then
`READY`. `READY` does not imply `PROMOTE`; a ready experiment may be `EXTEND`, `PROMOTE`, `REJECT`,
or `INCONCLUSIVE`. The legacy `recommendation` field MAY remain `EXTEND` / `READY_FOR_ANALYSIS`
for backward compatibility.

**R-STAT-019**: The stable core decision values are `EXTEND`, `PROMOTE`, `REJECT`, and
`INCONCLUSIVE`. `ABANDON`, `GUARDRAIL_FAILED`, and `READY_FOR_ANALYSIS` MUST NOT be emitted as core
decision values. `READY_FOR_ANALYSIS` MAY be retained only in the legacy readiness-oriented
`recommendation` field.

**R-STAT-020**: The stable core reason codes and their meanings are:

| Reason code | Meaning |
|---|---|
| `insufficient_samples` | One or more variants are below `min_samples`. |
| `insufficient_observations` | Fewer than two variants, a primary comparison, or a mandatory guardrail lacks usable observations. |
| `candidate_improved` | Sufficient evidence establishes a material candidate improvement. |
| `candidate_regressed` | Sufficient evidence establishes a material candidate regression. |
| `guardrail_failed` | A mandatory guardrail failed. |
| `guardrail_unsupported` | A mandatory guardrail has no supported observation source. |
| `effect_below_threshold` | Evidence is sufficient but practical effect is below policy. |
| `evidence_insufficient` | The configured evidence threshold is not satisfied. |
| `unsupported_multi_variant` | Automatic adjudication requires exactly two variants. |
| `analysis_unavailable` | Required statistical effect or evidence cannot be computed. |

**R-STAT-021**: Each structured analysis entry MUST emit `readiness`, `decision`, `reason_code`,
`reason`, `samples`, `decision_guardrails`, and `decision_policy`. It MUST emit `control`,
`candidate`, `direction`, `effect`, and `evidence` when those values are available for the decision
path. The enclosing analysis contract also exposes the configured `metric`, `analysis_type`,
`min_samples`, per-variant assignment and observation counts, comparisons, and detailed guardrail
statuses. Consumers SHOULD depend on structured readiness, decision, and reason fields rather than
human-readable prose.

The `effect` object reports the raw absolute candidate-minus-control effect, an optional relative
effect when the control mean is non-zero, and a direction-normalized absolute effect. The `evidence`
object reports the analysis method, whether the configured evidence threshold was satisfied, and
either a p-value or Bayesian probability of superiority when applicable.

### 11.7 Reporting Workflow Permissions

**R-STAT-011**: Any automated workflow that posts comments to issues (e.g., via `notify.issue`
or step-based issue comment creation) **MUST** declare `permissions: issues: write` in its
frontmatter.

**R-STAT-012**: Any automated workflow that posts discussions **MUST** declare
`permissions: discussions: write`.

---

## 12. Simultaneous Experiments and Interaction Effects

**R-MULTI-001**: Each experiment in the `experiments` map **MUST** be assigned independently.
The selection algorithm for one experiment **MUST NOT** depend on the selected variant of
any other experiment.

**R-MULTI-002**: Implementations **SHOULD NOT** run more than three experiments simultaneously
in a single workflow. When more than three experiments are active, a compile-time warning
**SHOULD** be emitted.

> **Note (statistical, informative)**: When two or more experiments are active simultaneously,
> observed differences in outcome metrics can be caused by either experiment individually or
> by their interaction (i.e., a specific combination of variant values). This violation of
> the Stable Unit Treatment Value Assumption (SUTVA) inflates the risk of misattribution.
> For example, if `prompt_style=concise` and `emoji_density=heavy` are both active, it is
> impossible to determine from pairwise analysis alone whether a change in output quality
> was caused by verbosity, emoji use, or the combination. Experimenters who need to measure
> interactions **MUST** use a full factorial design and ensure sufficient sample size for
> all K₁ × K₂ × … cell combinations.

**R-MULTI-003**: Reporting tools **MUST** note in their output when multiple experiments were
simultaneously active on runs included in the analysis window, to alert reviewers to potential
confounding.

**R-MULTI-004**: Experiments that change the `engine:` frontmatter key **MUST NOT** be
implemented within a single workflow file. Engine-switching experiments **MUST** use separate
compiled workflow files (one per variant), which can then be compared via their respective
GitHub Actions run metrics.

**R-MULTI-005**: When two or more experiments are simultaneously active in the same analysis
window, reporting tools **MUST** detect and bound interaction risk by preserving the full
assignment vector per run and evaluating whether each observed combination cell has sufficient
sample coverage. If interaction effects cannot be bounded (for example, sparse cells below
`min_samples`), the report **MUST** emit an explicit interaction-risk status and **MUST NOT**
recommend PROMOTE for affected variants.

The core decision engine does not currently consume interaction diagnostics. The bundled daily
experiment report preserves a core `PROMOTE` decision but sets its presentation-level
`report_action` to `EXTEND` with `interaction_underpowered` when this safety hold applies.
`interaction_underpowered` is not a core decision reason code.

### 12.1 Conflict Resolution Norms

A **conflict** occurs when two or more simultaneously active experiments would assign
incompatible configurations to the same workflow run. This subsection defines normative
behavior for each storage mode.

**R-CONFLICT-001 (general)**: When two experiments assign variants that together produce a
logically invalid workflow configuration (e.g., two `engine:` variants via separate
experiment keys), the compiler **MUST** reject the workflow at compile time with a
descriptive error. Runtime conflict detection is **NOT** a substitute for compile-time
validation.

#### 12.1.1 Conflict Resolution for `repo` Storage Mode

**R-CONFLICT-REPO-001**: Under `repo` storage, each experiment's variant selection reads and
writes an independent key in `state.json`. There is no shared mutable state between
experiments at the selection layer. Variant assignments for experiment A **MUST NOT** block
or override variant assignments for experiment B, even when both experiments are active on
the same run.

**R-CONFLICT-REPO-002**: When a concurrent write conflict is detected at push time (e.g., a
non-fast-forward rejection from the GitHub API), the push step **MUST** retry with the
merged state from both runs. The retry **MUST NOT** discard either run's assignment record.

**R-CONFLICT-REPO-003**: If two concurrent runs select the same least-used variant for the
same experiment (a read-time race), both selections are considered valid. The run records
**MUST** reflect each run's independently selected variant. No conflict error is raised for
this condition.

#### 12.1.2 Conflict Resolution for `cache` Storage Mode

**R-CONFLICT-CACHE-001**: Under `cache` storage, GitHub Actions cache is eventually
consistent across concurrent runs. When two runs attempt to save conflicting cache entries
under the same key, GitHub Actions will store one entry and silently drop the other.
Implementations **MUST** treat this as an acceptable data loss (see §7.4 informative note on
cache eviction) and **MUST NOT** treat a missing cache restore as an error condition.

**R-CONFLICT-CACHE-002**: Because `cache` storage does not provide atomic read-modify-write
semantics, implementations using `cache` mode **MUST** document to users that high-concurrency
workflows may experience elevated variant imbalance compared to `repo` mode.

#### 12.1.3 Conflict Resolution for Mixed Storage Mode

**R-CONFLICT-MIX-001**: All experiments within a single workflow **MUST** share the same
`storage` mode. Mixed-mode configurations (some experiments in `repo`, others in `cache`)
are **NOT SUPPORTED** and **MUST** produce a compile-time error.

**R-CONFLICT-MIX-002**: This restriction exists because the `storage` key is a single
top-level field in the `experiments` map that applies uniformly to all experiments in that
map. Workflow authors who require different storage modes for different experiments **MUST**
split them into separate workflow files.

---

## 13. Security Considerations

### 13.1 State File Integrity

The experiment state is stored in a git branch (`repo` mode) or GitHub Actions cache
(`cache` mode). Both backends are protected by repository access controls. However:

- Any user with write access to the repository can modify `state.json` on the experiments
  branch, potentially manipulating variant counters or forging run records.
- Implementers that require tamper-evident state **SHOULD** use signed commits via the
  GitHub GraphQL `createCommitOnBranch` mutation (R-STORE-REPO-004).

### 13.2 Prompt Injection via Variant Values

Variant strings declared in frontmatter are static strings set by the workflow author.
They are not derived from user-supplied input and therefore do not introduce prompt injection
risk at the frontmatter level. Workflow authors **MUST NOT** use runtime user input (e.g.,
issue titles, PR bodies) as variant values.

### 13.3 OTEL Attribute Leakage

Experiment assignments exported as OTEL resource attributes (§9.3) may be visible in
distributed-tracing backends. Variant names and experiment names **SHOULD NOT** embed
sensitive information.

### 13.4 Permission Minimization

- The `repo` storage mode requires `contents: write`. Workflows **SHOULD** limit all other
  permissions to `read` to minimize the blast radius of a compromised token.
- Reporting workflows that post comments require `issues: write` or `discussions: write`
  (§11.6). These permissions **SHOULD** be granted only to the specific reporting workflow,
  not to the experiment-running workflow itself.

---

## 14. Compliance Testing

### 14.1 Test Suite Requirements

Conformance at each level is verified by the following test categories.

#### 14.1.1 Schema Tests (Level 1)

| Test ID | Requirement | Description |
|---|---|---|
| T-SCHEMA-001 | R-SCHEMA-005 | Reject bare-array with fewer than 2 variants |
| T-SCHEMA-002 | R-SCHEMA-003 | Skip and warn on invalid experiment name |
| T-SCHEMA-003 | R-SCHEMA-007 | Reject object form with `variants` containing < 2 entries |
| T-SCHEMA-004 | R-SCHEMA-011 | Reject guardrail with invalid threshold pattern (non-numeric string that does not match comparison expression) |
| T-SCHEMA-004a | R-SCHEMA-011 | Accept guardrail with bare numeric threshold and `direction: min` or `direction: max` |
| T-SCHEMA-005 | R-SCHEMA-013 | Reject `notify` object with unknown keys |
| T-SCHEMA-006 | R-SCHEMA-001 | Compile workflow without `experiments:` field — output unchanged |

#### 14.1.2 Variant Selection Tests (Level 1)

| Test ID | Requirement | Description |
|---|---|---|
| T-SELECT-001 | R-SELECT-001 | Round-robin: select variant with lowest count |
| T-SELECT-002 | R-SELECT-002 | Round-robin: random tie-breaking on first run |
| T-SELECT-003 | R-SELECT-003 | Round-robin: counter incremented after selection |
| T-SELECT-004 | R-SELECT-004 | Weighted: selection probability proportional to weights |
| T-SELECT-005 | R-SELECT-006 | Weighted: counter incremented after selection |
| T-SELECT-006 | R-SELECT-005 | Weighted: all-zero weights return control variant |
| T-SELECT-007 | R-SELECT-007 | Weighted: mismatched length falls back to round-robin |

#### 14.1.3 Expression Integration Tests (Level 1)

| Test ID | Requirement | Description |
|---|---|---|
| T-EXPR-001 | R-EXPR-001 | `${{ experiments.x }}` rewritten to step output reference |
| T-EXPR-002 | R-EXPR-003 | Placeholder substituted before Handlebars rendering |
| T-EXPR-003 | R-EXPR-005 | `"no"` treated as falsy in `isTruthy` |
| T-EXPR-004 | R-EXPR-004 | Raw placeholder not passed to Handlebars engine |

#### 14.1.4 State Persistence Tests (Level 2)

| Test ID | Requirement | Description |
|---|---|---|
| T-STORE-001 | R-STORE-REPO-002 | Empty state on first run (404 branch) |
| T-STORE-002 | R-STORE-004 | Valid `state.json` structure written after run |
| T-STORE-003 | R-STORE-007 | Run record appended with correct fields |
| T-STORE-004 | R-STORE-005 | `runs` pruned to ≤ 512 entries |
| T-STORE-005 | R-STORE-006 | Legacy state (no `runs` field) initialized to empty array |
| T-STORE-006 | R-STORE-CACHE-004 | No `contents: write` required for cache mode |

#### 14.1.5 Audit CLI Tests (Level 2)

| Test ID | Requirement | Description |
|---|---|---|
| T-AUDIT-001 | R-AUDIT-003 | `--variant` without `--experiment` returns non-zero exit |
| T-AUDIT-002 | R-AUDIT-008 | Assignment read from `state.runs` when available |
| T-AUDIT-003 | R-AUDIT-009 | Fallback to max-count heuristic for legacy state |
| T-AUDIT-004 | R-AUDIT-005 | Overview `Experiment` field present when assignments exist |
| T-AUDIT-005 | R-AUDIT-007 | `Experiment` field omitted when no assignments |

#### 14.1.6 Statistical Reporting Tests (Level 3)

| Test ID | Requirement | Description |
|---|---|---|
| T-STAT-001 | R-STAT-001 | Assignments derived from `state.runs`, not delta inference |
| T-STAT-002 | R-STAT-005 | Bonferroni correction applied for K ≥ 3 variants |
| T-STAT-003 | R-STAT-007 | PROMOTE withheld until all variants reach `min_samples` |
| T-STAT-004 | R-STAT-009 | A failed mandatory guardrail forces a REJECT decision |
| T-STAT-005 | R-STAT-011 | Reporting workflow declares `issues: write` |

### 14.2 Compliance Checklist

| Requirement | Test ID | Level | Status |
|---|---|---|---|
| R-SCHEMA-001 | T-SCHEMA-006 | 1 | Required |
| R-SCHEMA-005 | T-SCHEMA-001 | 1 | Required |
| R-SELECT-001 | T-SELECT-001 | 1 | Required |
| R-SELECT-002 | T-SELECT-002 | 1 | Required |
| R-SELECT-003 | T-SELECT-003 | 1 | Required |
| R-SELECT-006 | T-SELECT-005 | 1 | Required |
| R-EXPR-001 | T-EXPR-001 | 1 | Required |
| R-EXPR-005 | T-EXPR-003 | 1 | Required |
| R-STORE-002 | — | 2 | Required |
| R-STORE-REPO-002 | T-STORE-001 | 2 | Required |
| R-STORE-007 | T-STORE-003 | 2 | Required |
| R-AUDIT-003 | T-AUDIT-001 | 2 | Required |
| R-AUDIT-008 | T-AUDIT-002 | 2 | Required |
| R-STAT-001 | T-STAT-001 | 3 | Required |
| R-STAT-005 | T-STAT-002 | 3 | Recommended |
| R-STAT-007 | T-STAT-003 | 3 | Required |
| R-STAT-011 | T-STAT-005 | 3 | Required |

---

## 15. References

### Normative References

- **[RFC 2119]** Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", RFC 2119, March 1997. <https://www.ietf.org/rfc/rfc2119.txt>
- **[ADR-29534]** gh-aw maintainers, "Frontmatter A/B Experiments with Balanced Variant Selection", 2026-05-01. `docs/adr/29534-frontmatter-ab-experiments-variant-selection.md`
- **[ADR-29618]** gh-aw maintainers, "Rich Experiment Metadata Schema Extension with Weighted Selection and Date Gating", 2026-05-01. `docs/adr/29618-rich-experiment-metadata-schema-extension.md` *(normative sections superseded by §5.2 of this document)*
- **[ADR-29628]** gh-aw maintainers, "Add `--experiment` and `--variant` Filter Flags to `gh aw audit`", 2026-05-01. `docs/adr/29628-experiment-variant-filter-flags-for-audit.md`
- **[ADR-29985]** gh-aw maintainers, "Experiment Per-Run State, OTEL Integration, and Schema Extensions", 2026-05-03. `docs/adr/29985-experiment-per-run-state-otel-integration-and-schema-extensions.md`
- **[ADR-29996]** gh-aw maintainers, "Experiment State Storage — Git Branch as Default, Cache as Fallback", 2026-05-03. `docs/adr/29996-experiment-state-git-branch-storage.md`

### Informative References

- **[SUTVA]** Rubin, D. B., "Estimating Causal Effects of Treatments in Randomized and Nonrandomized Studies", *Journal of Educational Psychology*, 66(5):688–701, 1974. (Stable Unit Treatment Value Assumption)
- **[BONFERRONI]** Dunn, O. J., "Multiple Comparisons Among Means", *Journal of the American Statistical Association*, 56(293):52–64, 1961.
- **[WELCH-TTEST]** Welch, B. L., "The Generalization of Student's Problem When Several Different Population Variances are Involved", *Biometrika*, 34(1/2):28–35, 1947.
- **[GitHub Actions Cache]** GitHub Docs, "Caching dependencies to speed up workflows". <https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/caching-dependencies-to-speed-up-workflows>

---

## Appendices

### Appendix A: Full Object-Form Example

```yaml
---
on:
  schedule: daily on weekdays
engine: copilot
permissions:
  contents: read
  pull-requests: read

experiments:
  storage: repo
  prompt_style:
    variants: [concise, detailed, step_by_step]
    description: "Test whether verbosity level affects output quality"
    hypothesis: "H0: no change in aic. H1: concise reduces by >=15%"
    metric: aic
    secondary_metrics: [duration_ms, discussion_word_count]
    guardrail_metrics:
      - name: success_rate
        threshold: ">=0.95"
      - name: empty_output_rate
        direction: min
        threshold: 0.0
    weight: [40, 40, 20]
    min_samples: 30
    start_date: "2026-05-01"
    end_date: "2026-08-01"
    issue: 1234
    analysis_type: t_test
    tags: [cost, prompting, verbosity]
    notify:
      issue: 1234
---

Summarize the pull requests merged today.

{{#if experiments.prompt_style == "concise" }}
Write a maximum of 5 bullet points.
{{#else if experiments.prompt_style == "detailed" }}
Write a structured report with sections for new features, bug fixes, refactors, and docs.
{{#else}}
Write a numbered step-by-step walkthrough of each change with rationale.
{{#endif}}
```

### Appendix A2: Weighted Variant Selection — Worked Example

This appendix walks through the probability math for a three-variant `weighted` experiment to
illustrate how the `weight` array maps to selection probability, how counters are updated, and
how balance is maintained over many runs.

#### A2.1 Scenario Setup

An experiment named `response_tone` has three variants with non-uniform weights:

```yaml
experiments:
  storage: repo
  response_tone:
    variants: [formal, casual, neutral]
    weight: [20, 50, 30]
```

The weight values are **relative proportions**, not absolute percentages. The implementation
normalizes them to compute probabilities:

```
total_weight = 20 + 50 + 30 = 100

P(formal)  = 20 / 100 = 0.20  (20%)
P(casual)  = 50 / 100 = 0.50  (50%)
P(neutral) = 30 / 100 = 0.30  (30%)
```

For a 10-run experiment sequence, the **expected** variant distribution is:

| Variant | Weight | Expected runs (of 10) |
|---------|--------|-----------------------|
| formal  | 20     | 2                     |
| casual  | 50     | 5                     |
| neutral | 30     | 3                     |

#### A2.2 Selection Algorithm (Weighted Random)

The `weighted` algorithm draws a uniform random number `r ∈ [0, 1)` and maps it to a variant
via cumulative weight:

```
Cumulative ranges:
  [0.00, 0.20)  →  formal
  [0.20, 0.70)  →  casual
  [0.70, 1.00)  →  neutral
```

Example draws:

| r      | Selected variant |
|--------|-----------------|
| 0.11   | formal           |
| 0.45   | casual           |
| 0.72   | neutral          |
| 0.19   | formal           |
| 0.68   | casual           |

#### A2.3 Counter Updates

After each run, the counter for the selected variant is incremented in `state.json`.
After 10 runs with the distribution above, a typical `counts` object is:

```json
{
  "counts": {
    "response_tone": {
      "formal":  2,
      "casual":  5,
      "neutral": 3
    }
  }
}
```

Per R-SELECT-006, the `weighted` algorithm **MUST** increment invocation counters after every
selection. This allows the audit CLI and reporting workflows to verify that observed variant
frequencies approximate the declared weights over time.

#### A2.4 Long-Run Balance Verification

Over N runs, the observed frequency for variant v should converge to `weight[v] / total_weight`
by the Law of Large Numbers. Reporting workflows SHOULD flag experiments where any variant's
observed frequency deviates from its target weight by more than ±10 percentage points over at
least 30 runs, as this may indicate a misconfigured `weight` array or a bug in the selection
implementation.

For the example above, after 100 runs:

| Variant | Expected runs | Acceptable range (±10 pp) |
|---------|--------------|--------------------------|
| formal  | 20           | 10 – 30                  |
| casual  | 50           | 40 – 60                  |
| neutral | 30           | 20 – 40                  |

#### A2.5 Contrast with Balanced Round-Robin

The `balanced` (least-used) algorithm ignores weights and selects the least-run variant
deterministically. Use `weighted` when you intentionally want unequal traffic allocation
(e.g., to expose fewer users to an experimental variant while still gathering comparative
data). Use `balanced` when you want equal allocation and maximum statistical efficiency per
total run count.

### Appendix B: `state.json` Schema

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["counts"],
  "properties": {
    "counts": {
      "type": "object",
      "additionalProperties": {
        "type": "object",
        "additionalProperties": { "type": "integer", "minimum": 0 }
      }
    },
    "runs": {
      "type": "array",
      "maxItems": 512,
      "items": {
        "type": "object",
        "required": ["run_id", "timestamp", "assignments"],
        "properties": {
          "run_id": { "type": "string" },
          "timestamp": { "type": "string", "format": "date-time" },
          "assignments": {
            "type": "object",
            "additionalProperties": { "type": "string" }
          }
        }
      }
    }
  }
}
```

### Appendix C: Sample Size Reference

For a two-proportion test with 80% statistical power and α = 0.05 (two-tailed), the
approximate minimum runs per variant are:

| Minimum Detectable Effect (pp) | Runs per variant |
|---|---|
| 5 | ~620 |
| 10 | ~160 |
| 15 | ~70 |
| 20 | ~40 |
| 30 | ~20 |

> **Note for weighted experiments**: When `weight` is non-uniform, apply these figures to
> the **smaller group**. For a 70/30 split aiming to detect a 10 pp effect, you need
> ~160 runs in the 30% arm (≈ 533 total runs).

### Appendix D: Known Limitations

1. **Read-time race condition**: Concurrent runs with `repo` storage may read stale state and
   select the same variant. See R-STORE-REPO-005 and the informative note in §7.3.

2. **Interaction effects**: Running multiple experiments simultaneously can produce unattributable
   results. See §12 and R-MULTI-002.

3. **Engine-switching experiments**: Changing the `engine:` key requires separate workflow files;
   see R-MULTI-004.

4. **`analysis_type` advisory only**: Reporting workflows that do not implement all four
   statistical tests will fall back to defaults. The field documents intent; it does not
   enforce a specific computation path.

5. **State branch growth**: The experiments git branch grows monotonically. Operators
   **MAY** prune old commits from the experiments branch without affecting the current state.

### Sync Follow-ups (May 2026 Expert Review)

This appendix itemizes corrective follow-ups referenced in the abstract.

- **FR-001 (implemented via R-SELECT-006)**: Weighted selection increments invocation counters after each selection.
- **FR-002 (implemented via R-STAT-001/R-STAT-002)**: Reporting uses `state.runs` assignment records instead of count-delta inference.
- **FR-003 (implemented via R-STAT-011/R-STAT-012)**: Reporting workflows that write issues/discussions declare explicit write permissions.
- **FR-004 (implemented via R-MULTI-005)**: Concurrent-experiment interaction effects are explicitly detected and bounded before promotion decisions.
- **FR-005 (implemented via daily-experiment-report workflow update)**: Reporting guidance now includes a factorial-interaction helper for K₁×K₂ cell-level significance output and sparse-cell risk surfacing.
- **FR-006 (implemented via compiler diagnostics)**: The compiler now emits a warning when more than one experiment is active and weighted traffic is configured, indicating potential sparse interaction cells.

---

## Change Log

### Version 1.1.0 (Draft) — 2026-05-12

- **Added**: Daily reporting helper guidance for factorial K₁×K₂ interaction cell significance output.
- **Added**: Compiler warning requirement for sparse interaction-cell risk when multiple experiments and weighted traffic are configured.
- **Updated**: Sync Follow-ups appendix to replace v1.1.0 TODOs with implemented corrective items.

### Version 1.0.1 (Draft) — 2026-05-07

- **Added**: R-MULTI-005 requiring interaction-risk detection/bounding for simultaneous experiments.
- **Added**: Sync Follow-ups appendix with itemized May 2026 expert-review corrective items and owned TODOs.

### Version 1.0.0 (Draft) — 2026-05-03

- **Initial publication** consolidating ADR-29534, ADR-29618, ADR-29628, ADR-29985, and ADR-29996.
- **Correction**: R-SELECT-006 supersedes ADR-29618 Rule 9 — weighted selection MUST increment invocation counters (was incorrectly stated as MUST NOT; the reference implementation already implements the correct behavior).
- **Added**: R-STAT-001/R-STAT-002 — reporting tools MUST use `state.runs` for per-run assignment lookup, not the fragile delta-count inference method.
- **Added**: R-STAT-005/R-STAT-006 — Bonferroni correction SHOULD be applied for K ≥ 3 variants to control family-wise error rate.
- **Added**: R-STAT-008 — `min_samples` applies to the smallest expected group when weights are non-uniform.
- **Added**: R-STAT-011/R-STAT-012 — reporting workflows MUST declare `issues: write` / `discussions: write` when posting comments.
- **Added**: R-MULTI-002/R-MULTI-003 — warning for > 3 simultaneous experiments; interaction effects must be noted in reports.
- **Added**: §13 Security Considerations — state integrity, prompt injection, OTEL leakage, permission minimization.
- **Added**: Appendix C (sample size reference) and Appendix D (known limitations).
- **Informative note**: `storage: cache` default changed to `storage: repo` in ADR-29996; any documentation or issue templates that still refer to "cache-based" assignment should be updated.

---

*Copyright © 2026 GitHub, Inc. All rights reserved. This specification is maintained by the
gh-aw project team.*
