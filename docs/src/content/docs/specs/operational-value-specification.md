---
title: Operational Value Report Specification
description: Evidence, clocks, and attribution rules for a reproducible workflow value report.
sidebar:
  order: 1371
---

**Version:** 1.0.0 · **Feature status:** Experimental · **Report schema:** 1

## Measurement contract

`gh aw value` separates delivery, adoption, recorded run grades, independently measured outcomes, and attributed value. A workflow is a reporting dimension, not the unit of measurement. The owner chooses one primary metric in its native unit and fixes its eligible population, independent producer, formula, direction, baseline, sampling, lag, durability, overlap and missing-data rules. For prospective use, freeze the definition, recipe and semantic fixtures **before adoption**, with no target results or adoption decisions in the design context. Apply the same rules afterward to all eligible subjects, including negative outcomes. A digest identifies the inputs; it does not certify blindness, timing or causality. Label post-hoc designs retrospective.

## Evidence and clocks

- Match native lifecycle effects by host, repository, workflow path and delivery time. A later GitHub decision can be observed after the report cutoff. Execution or a delivery fallback is not explicit human acceptance. Missing item records do not prove zero deliveries, even when a separate run summary records a no-op.
- Read only the recorded primary `operational-value` grade of a matching completed run and attempt, selected by creation time. Preserve zero, unavailable and error; never aggregate grades into an outcome or rewrite a session. Custom grader execution belongs to the [grader job](/gh-aw/specs/graders-specification/), not the report.
- Git snapshots measure the selected first-parent history. Typed observations instead carry caller-supplied references, event and observation clocks (`at` ≤ `observed_at` ≤ report collection), subject IDs and raw values. Changed context/population, missing samples or insufficient coverage cannot establish improvement. Imported values never establish native adoption, full-window surveillance, or attributed value.
- Optional daily Git rows end at noon UTC. Each counts distinct retained human-accepted effects for the workflow by decision time in `(previous noon, this noon]`; the baseline includes only decisions exactly at the start instant. A positive count is a lower bound. No matching decision is unknown, not a certified zero. Do not interpolate sparse observations or infer a causal association.

## Attribution

1. Require one matching native delivered `create_pull_request` effect for the producer-linked PR, its canonical URL and run. Adoption must be available, `explicit_human`, verified by GitHub at the exact merge time.
2. Compare the exact first parent with the outcome on main. Require comparable populations, the declared sample minima, a mature outcome/durability/isolation window, isolation from conflicting producer-linked merges (including neighbors outside the delivery cohort), and untouched changed files through durability. A later aggregate change without subject linkage remains descriptive.
3. Report `windows[].reason` when proof is absent. An incomplete follow-up is pending; a missing gate is unavailable. A verified zero stays zero. Sum only qualifying individual windows; `attributed_value: null` means none qualified. Complete coverage refers to this report's windows, not the workflow's entire history. Non-PR effects retain native lifecycle observations but cannot contribute to this subtotal.

## Replay and limits

Retain the evaluator version, declaration/recipe digests, Git SHA or evidence bundle, run/attempt IDs, native exports, provider observations and report JSON. Git replay still reads live PR metadata; it never fetches Git history, dispatches workflows, mutates records or executes repository code. No new collector is required by the report. Elapsed time, survival of files and temporal correlation alone do not prove the workflow caused an outcome.

Definitions, recipes and typed bundles are limited to 512 KiB each; a bundle accepts at most 1,024 observations. Grade summaries are limited to 50 MiB. The lifecycle cache is bounded to 16 MiB / 20,000 items and prunes old deliveries on write; it is not a journal. GitHub discovery fails on incomplete, changing or over-1,000-result searches. Shortening the report period need not reduce search size because neighbors must remain visible. Work is bounded per report, not indexed enterprise-wide: Git sampling scales with trend/daily dates plus merge count times sample slots (at most 1,024 per merge). See the [CLI reference](/gh-aw/reference/value/) for inputs and commands.
