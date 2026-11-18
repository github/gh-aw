---
name: operational-value-designer
description: "Set up a workflow's operational value report from its goal and available evidence. Usage: /operational-value-designer OWNER/REPO WORKFLOW."
argument-hint: "OWNER/REPO WORKFLOW"
metadata:
  version: "5.3.0"
---

# Operational Value Designer

Set up one `gh aw value WORKFLOW` report in the workflow owner's repository.
Do not ask for a mode or register upstream. Keep native delivery and adoption rules.

**Design blind; evaluate later.** Before the target cohort's adoptions, design from
workflow intent, evidence schemas and synthetic fixtures in a clean context. Do not
load that cohort's runs, grades, PRs, adoption decisions, metric history, slide
results or prior analyses. Freeze definition, recipe and fixtures together. If
outcomes already informed the design, label that cohort retrospective and apply
the frozen contract to the next unseen cohort. No timestamp check certifies this.
Only after freeze, in a separate evaluation context, inspect the target evidence.
Never tune a rule or exclude a case to improve the result.

## Route and inspect

- For prospective design, use the [report specification](https://github.github.com/gh-aw/specs/operational-value-specification/);
  do not open its pilot results. For historical replay or slides **after freeze**, read
  [longitudinal-report.md](references/longitudinal-report.md).
- For recorded-grade reporting or session grading, read
  [run-scoped.md](references/run-scoped.md).
  Prefer the target's existing Go graders or supported session queries. The
  separate `graders.operational-value.script` contract runs custom session
  graders in Actions; never add JS execution to a report or Go server.
- For non-Git replay or window verification after freeze, read
  [evidence-and-windows.md](references/evidence-and-windows.md).
- For the schema-feature slide replay, use the checked-in definition
  `.github/aw/value/schema-feature-coverage.json` and recipe
  `.github/aw/value/schema-feature-coverage.slides.json`. Load their contents and
  the longitudinal reference before changing either.

Confirm supported evaluators, configuration, queries and evidence scopes in
the installed runtime. Ask only if the workflow intent or boundary is unclear.

`gh aw value` reads primary grades from the existing logs `summary.json` under
`--logs-dir`. No new evaluator definition is needed for that view. Preserve
zero, unavailable, and error states; never substitute an efficiency grade.
This read-only bridge does not regrade, aggregate, change execution isolation,
or prove external lake ingestion.

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

1. **Measurement:** an existing session-grader contract, or versioned metadata
   with a named Go report evaluator/validated parameters or supported query.
   Keep session and historical evidence scopes distinct.
2. **Recipe:** dates, sampling policy, producer discovery, cohort, and inspected
   windows; separate descriptive batches from attributable individual outputs.
3. **Evidence/result:** pinned input commits, definition/recipe digests,
   versioned evaluator identity, observations, native adoption records,
   collection clocks, unavailable reasons, and the attributed subtotal.

`gh aw value` supports `json-number-v1` for committed independent numbers,
`schema-example-coverage-v1` for textual examples, and `evidence-number-v1`
with `--evidence` for typed Git, run, benchmark, or object-state observations.
Imported evidence is caller-supplied, never native adoption or survival proof.
All three work in external repositories without upstream registration.
Unsupported semantics require a reviewed evaluator/projection; stay unavailable.
Do not stretch session semantics or fabricate session events for repository
snapshots. Reports must not execute workflow code or add a query language.

Declare versioned baseline, before/after periods, lag, durability, cadence,
minimum samples, and overlap rules before future collection. Seven days is
the slides' compatibility policy, not an empirically validated universal window.
Do not tune horizons to favorable outcomes or confuse elapsed time with complete
evidence. Label policies designed after observing a cohort as retrospective.

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

Test attained, zero, unavailable, malformed, inapplicable and changed-population
fixtures through the production evaluator. Exercise no-adoption, overlap and
touched-file gates. After freeze, replay all eligible cases, including no-ops,
regressions and pending outcomes. Compare identities and values across repeats.
For pilots, isolate collection failures; count executed measurements, not merely
configured definitions or successful commands. Missing evidence is not zero.
Use installed `gh aw value` and the owner's fixture checks. Only gh-aw contributors
run `TestOperationalValue` and the repository gates. For slides, compare all layers
to the historical baseline in the replay reference; never hard-code results.

Present the definition, recipe, fixtures, command, and verification limits.
Freeze semantics and evidence provenance together; corrections are prospective.
Label retrospective exploration and partial attribution explicitly. Stop with
missing evidence rather than relaxing gates or claiming reproduction.
