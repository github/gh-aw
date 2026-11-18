---
name: operational-value-designer
description: "Design Go-native value definitions and report recipes from workflow intent; keep run-scoped grading separate. Usage: /operational-value-designer OWNER/REPO WORKFLOW [longitudinal|run-scoped]."
argument-hint: "OWNER/REPO WORKFLOW [longitudinal|run-scoped]"
metadata:
  version: "4.0.0"
---

# Operational Value Designer

Turn workflow intent into a deterministic measurement contract, fixtures, and
an executable `gh aw value` report recipe. Default to longitudinal reporting.
Only the measurement varies by workflow; never redesign delivery or adoption.
Work in the workflow owner's repository, not necessarily `github/gh-aw`.
Definitions, recipes, and fixtures belong there; gh-aw is an installed tool.

## Route and inspect

- For longitudinal reports, slides, or `gh aw value`, read
  [longitudinal-report.md](references/longitudinal-report.md).
- For recorded-session grading, read [run-scoped.md](references/run-scoped.md).
  Prefer the target's existing Go graders or supported session queries. The
  separate `graders.operational-value.script` contract is retained for existing
  custom session graders only; do not introduce JS execution into a Go server.
- For the schema-feature slide replay, use the checked-in definition
  `.github/aw/value/schema-feature-coverage.json` and recipe
  `.github/aw/value/schema-feature-coverage.slides.json`. Load their contents and
  the longitudinal reference before changing either.

Inspect the exact workflow, implementation, and representative evidence first.
Confirm supported evaluator IDs, configuration, query syntax, and evidence
scopes in the installed runtime, not by assuming the owner has gh-aw's source.
Never invent a declaration or query the runtime
cannot execute. Ask only when workflow intent or the measurement boundary is
unclear, not for approval to execute an agreed plan.

## Design contract

Write a compact table: ultimate goal, eligible population, observable outcome,
native unit, direction, evidence scope/inputs, zero/unavailable/error rules,
population churn, confounders, and adoption boundary. Choose one primary metric.

Prefer the furthest downstream independently verifiable outcome. Activity,
output counts, agent claims, successful execution, and merges alone are not
attributed value. Fix the eligible denominator before counting successes.
Preserve subject identities when churn or set differences affect attribution.
Missing, conflicting, incomplete, or inapplicable evidence is unavailable, not
zero. Malformed declarations fail explicitly; do not silently substitute metrics.

Keep three contracts distinct:

1. **Measurement:** versioned metadata plus a named Go evaluator and validated
   parameters, or an already-supported query with an explicit evidence scope.
2. **Recipe:** dates, sampling policy, producer discovery, cohort, and inspected
   windows; separate descriptive batches from attributable individual outputs.
3. **Evidence/result:** pinned input commits, definition/recipe digests,
   versioned evaluator identity, observations, native adoption records,
   collection clocks, unavailable reasons, and the attributed subtotal.

The current `gh aw value` supports native `json-number-v1` for independently
measured, committed numeric evidence and `schema-example-coverage-v1` for
textual schema examples, not arbitrary queries or JavaScript. An external
workflow can use either with local paths, without registration in github/gh-aw.
Unsupported evidence scopes require a reviewed native evaluator or projection;
leave value unavailable until implemented.
Do not stretch session semantics or fabricate session events for repository
snapshots. Do not execute workflow code or introduce a second query language.

## Shared ownership and safety

gh-aw owns producer identity, native lifecycle projection, Git sampling, exact
parent comparison, attribution, reversal, and survival. GitHub owns object
disposition. `safe_output.result` and `safe_output.execution` record execution,
not later human acceptance. Inspect native evidence through `logs --delivery`,
`logs --adoption`, or `audit RUN --adoption`.

Every workflow can report delivery/adoption without a value definition or CAO.
githubnext/gh-cao may schedule refresh and consume records; do not duplicate
adoption classification, databases, or Dashboard Language in this skill.
Run-scoped scores cannot substitute for longitudinal value. A survival snapshot
cannot prove complete-window survival. Current attribution supports exact
human-adopted PR merges only; never attribute descriptive batches.

No report may fetch Git history, dispatch workflows, mutate lifecycle records,
or rewrite past grader results. Surface missing history, unsupported evaluators,
API visibility failures, and unavailable evidence explicitly.

## Verify and deliver

Test attained, observed zero, unavailable, malformed, inapplicable, and
changed-population cases through the production evaluator. Repeat identical
inputs and compare values and identities. Exercise no-adoption, shared-window,
and touched-file gates. A lifecycle-only report is not a validated value
function. In multi-workflow pilots, isolate collection failures, distinguish
complete zero-delivery runs from missing evidence, and report how many
measurements actually executed. Keep proposed metric contracts separate from
working definitions; do not call a design-only candidate implemented.
In the workflow owner's repository, use installed
`gh aw value` and that repository's fixture checks; do not require gh-aw's Go
source, tests, or Makefile. Run focused `TestOperationalValue` tests only when
changing the gh-aw implementation itself, then its final validation gates.

Run the documented command against completed history when available. For the
slides verify all weekly observations, output cohort, six selected windows,
84 daily samples, stable cohort, and native five-field witness against the
recorded baseline in the longitudinal reference. Never hard-code expected
historical totals into measurement logic or treat synthetic fixtures as a live
historical replay.

Present the definition, recipe, fixtures, command, and verification limits.
Freeze semantics and evidence provenance together; corrections are prospective.
Label retrospective exploration and partial attribution explicitly. Stop with
missing evidence rather than relaxing gates or claiming reproduction.
