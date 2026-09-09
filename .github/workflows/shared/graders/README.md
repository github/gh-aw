# Trajectory Graders Catalog

Deterministic, per-trace behavioral graders that go beyond the existing
built-in graders (step count, retries, loops, duration, tool-success rate,
trajectory efficiency — see
[Graders reference](https://githubnext.github.io/gh-aw/reference/trace-graders/)).
Each grader below is implemented as its own importable `graders:`
frontmatter script under `shared/graders/<id>.md`, built once as a
projection over the [canonical Trajectory IR](trajectory-ir.md) so adding a
new grader never requires a new trace parser.

Ranked by diagnostic value, novelty relative to existing built-in graders,
deterministic computability, applicability to a single completed trace, and
usefulness as a harness/agent-evolution signal.

`daily-trajectory-grader-implementer` (see
`.github/workflows/daily-trajectory-grader-implementer.md`) implements
exactly **one** grader per run, until all 25 are shipped. Implementation
order is **tier-first, then rank-within-tier**: walk Tier 1 top-to-bottom,
then Tier 2, then Tier 3 (ranks are not contiguous within a tier — Tier 1
is ranks 5-11, Tier 2 is ranks 1-4 and 12-19, Tier 3 is ranks 20-25 —
because tiers reflect implementation priority: Tier 1 graders need no
golden answer, no second model, and no task-specific annotations beyond
the canonical Trajectory IR, so they ship first even though some of them
rank below Tier 2 graders). When a grader ships, flip its **Status** below
to `Implemented` in the same PR that adds `shared/graders/<id>.md`.

## Tier 1 — no golden answer, no second model, IR-only (implement first)

| Rank | Grader ID | Runtime requirement | Status |
|---|---|---|---|
| 5 | `state-revisit-probability-rep` | Canonical state IDs | Implemented |
| 6 | `recurrence-determinism` | Canonical state/event sequence | Implemented |
| 7 | `recurrence-laminarity` | Canonical states | Implemented |
| 8 | `recurrence-trapping-time` | Canonical states | Implemented |
| 9 | `recurrence-rate` | Canonical states | Implemented |
| 10 | `event-entropy-rate` | Event sequence only | Implemented |
| 11 | `lempel-ziv-trajectory-complexity` | Event sequence only | Implemented |

## Tier 2 — needs explicit constraints/states/provenance/objectives

| Rank | Grader ID | Runtime requirement | Status |
|---|---|---|---|
| 1 | `policy-near-miss` | Policy/guard predicates | Implemented |
| 2 | `skill-constraint-coverage` | Precompiled constraints | Implemented |
| 3 | `exploration-error` | State/task model | Implemented |
| 4 | `exploitation-error` | State/task model | Implemented |
| 12 | `tool-output-consumption-rate` | Provenance/reference IDs | Implemented |
| 13 | `end-to-end-lineage-completeness` | Provenance graph | Not started |
| 14 | `action-provenance-coverage` | Provenance graph | Not started |
| 15 | `premature-termination-gap` | Completion predicates | Not started |
| 16 | `evidence-saturation-stopping-lag` | Completion predicates | Not started |
| 17 | `dependency-order-violation-rate` | Objective DAG | Not started |
| 18 | `objective-coverage` | Objective predicates | Not started |
| 19 | `grounding-accuracy` | Valid-action schemas | Not started |

## Tier 3 — benchmark/evaluation mode (needs a reference trajectory/patch/process model)

| Rank | Grader ID | Runtime requirement | Status |
|---|---|---|---|
| 20 | `tool-wise-score` | Reference trajectory | Not started |
| 21 | `trajectory-ndtw` | Reference trajectory + state distance | Not started |
| 22 | `code-search-recall` | Reference patch | Not started |
| 23 | `code-read-precision` | Reference patch + symbol extraction | Not started |
| 24 | `code-edit-precision` | Reference patch | Not started |
| 25 | `process-alignment-fitness` | Process model | Not started |

## What each grader answers

1. **`policy-near-miss`** — did the trace reach the correct outcome without performing required checks? Detects "successful" traces that skipped a guard.
2. **`skill-constraint-coverage`** — which behavioral requirements of the harness/skill were exercised, and did they pass? Converts a trace into a harness-improvement signal.
3. **`exploration-error`** — did the agent fail because it never gathered enough information (insufficient search)?
4. **`exploitation-error`** — did the agent have enough information but fail to use it effectively? Complements #3.
5. **`state-revisit-probability-rep`** — `(visited states − distinct states) / visited states`; wasted exploration from any revisit, not only adjacent loops.
6. **`recurrence-determinism`** — RQA DET: fraction of recurrent points forming diagonal (repeated-subsequence) structures.
7. **`recurrence-laminarity`** — RQA LAM: fraction of recurrent points forming vertical (stagnation) structures.
8. **`recurrence-trapping-time`** — average length of vertical recurrence structures: once stuck, how long does the agent stay stuck?
9. **`recurrence-rate`** — RQA RR: overall density of recurrent (previously-visited) states across the run.
10. **`event-entropy-rate`** — Shannon entropy rate of the ordered event process; effective diversity/unpredictability, not raw tool-name count.
11. **`lempel-ziv-trajectory-complexity`** — LZ76 complexity of the canonical event string; detects repeated motifs without hand-defining them.
12. **`tool-output-consumption-rate`** — fraction of tool outputs that were subsequently referenced by a later action.
13. **`end-to-end-lineage-completeness`** — fraction of final outputs traceable through the provenance graph back to evidence/tool/environment roots.
14. **`action-provenance-coverage`** — fraction of consequential actions (edits, comments, mutations, safe outputs) with a recorded informational antecedent.
15. **`premature-termination-gap`** — count of explicit completion/evidence conditions still unsatisfied at termination.
16. **`evidence-saturation-stopping-lag`** — events elapsed between all objectives becoming satisfied and the agent actually stopping.
17. **`dependency-order-violation-rate`** — fraction of DAG-dependent actions executed before their declared prerequisites.
18. **`objective-coverage`** — fraction of explicitly declared objectives actually completed.
19. **`grounding-accuracy`** — fraction of actions that were structurally valid in the state in which they were issued.
20. **`tool-wise-score`** — longest correct execution prefix vs. a reference trajectory, incorporating parameter correctness.
21. **`trajectory-ndtw`** — normalized dynamic time warping distance between the observed path and a reference trajectory.
22. **`code-search-recall`** — fraction of files required by a reference patch that were ever located during the run.
23. **`code-read-precision`** — fraction of read/inspected symbols that were actually relevant to the reference patch (agents often over-inspect).
24. **`code-edit-precision`** — fraction of edited lines/hunks that overlap the reference patch's actual changes.
25. **`process-alignment-fitness`** — process-mining conformance score between the observed trace and an allowed workflow/process model.

## Consuming a grader from a workflow

Import the specific grader fragment(s) you need:

```yaml
imports:
  - shared/graders/state-revisit-probability-rep.md
```

Each grader fragment contributes a deterministic custom grader script through
frontmatter; none require network access or a second model call.
