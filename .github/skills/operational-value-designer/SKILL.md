---
name: operational-value-designer
description: "Set up a workflow's operational value report. Usage: /operational-value-designer OWNER/REPO WORKFLOW."
argument-hint: "OWNER/REPO WORKFLOW"
metadata:
  version: "5.3.0"
---

# Operational Value Designer

Set up one `gh aw value WORKFLOW` report in the workflow owner's repository. Do not ask for a mode or register upstream. The installed CLI reads existing measurements, native lifecycle records and recorded primary grades; it does not generate evidence or execute a grader.

## Design before adoption

1. Use a clean context containing only workflow intent, constraints, evidence formats and synthetic fixtures. Do not inspect the target cohort's runs, grades, PRs, adoption decisions or later outcomes.
2. Choose one independently verifiable outcome in its native unit. Fix eligible subject IDs, denominator, direction, baseline, producer, clocks, sampling, lag, durability, overlap, and rules for zero, missing and malformed inputs. Output counts and an agent's own success claims are not value.
3. Freeze the reviewed definition, recipe and fixtures together. Test improvement, no change, regression, missing input, changed population and malformed data through the production evaluator. If outcomes already informed the design, label that cohort retrospective and apply the frozen rules to a new one. Do not backdate corrections.

## Select existing evidence

- For repository history, use `json-number-v1` for an independently produced committed number or `schema-example-coverage-v1` for textual examples. For measured Git, benchmark, object-state or run observations, use `evidence-number-v1` with `--evidence`. Save `.github/aw/value/WORKFLOW.json` and pass the frozen recipe explicitly. See the [CLI reference](https://github.github.com/gh-aw/reference/value/) for declarations and the [report specification](https://github.github.com/gh-aw/specs/operational-value-specification/) for evidence boundaries. Do not invent a query or evaluator.
- Reuse recorded `operational-value` grades from `--logs-dir/summary.json`; no definition is required to read them. If a custom session grader is needed, follow [the run-scoped contract](references/run-scoped.md). `graders.operational-value.script` executes only in a separate Actions job; never add JavaScript to the report or conclusion.
- `safe_output.result` records an applied effect, not later human acceptance. Use native `logs --adoption` (includes delivery) or `audit RUN --adoption` for lifecycle evidence. Do not duplicate adoption classification, require `githubnext/gh-cao`, or treat a delivery fallback as a human decision.

## Evaluate after freeze

In a separate context apply the unchanged rules to all eligible cases, including no-ops, regressions and incomplete windows. Preserve run/subject identity, input digests, event and collection clocks. A grade is a run result; a measured outcome is not automatically attributed value. Only a direct human-adopted PR passing all report gates can contribute to the subtotal. Never infer zero from absent evidence.

Deliver the definition, recipe, fixtures, reproducible CLI command and limitations. Surface collection or identity failures; do not dispatch a workflow, rewrite prior results or add a new collector to make a score appear.
