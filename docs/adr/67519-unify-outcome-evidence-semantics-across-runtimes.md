# ADR-67519: Unify Outcome Evidence Semantics Across the Go and JavaScript Runtimes

**Date**: 2026-10-10
**Status**: Proposed
**Deciders**: pelikhan (PR author); gh-aw maintainers (review pending)

---

### Context

Safe-output outcomes are evaluated twice in gh-aw: once by the Go CLI (`pkg/cli/outcome_eval*.go`, used by
`gh aw audit`/outcome reporting) and once by the JavaScript collector bundled into the scheduled
`outcome-collector` workflow (`actions/setup/js/evaluate_outcomes.cjs`). The two implementations had drifted
and classified identical safe outputs differently. Worse, both could report `accepted` from weak signals —
the mere existence of the target object, or unrelated downstream activity — rather than from evidence that
the agent's action actually executed. Missing API data was also treated as "no human effort", which inflated
zero-touch acceptance. The constraint is that both runtimes feed the same telemetry/report schema, so their
classifications must be identical for the same input, and the contract must be conservative: ambiguity has to
degrade to `unknown`/`error`, never to `accepted`.

### Decision

We will define one conservative, action-specific evidence contract and enforce it in both runtimes from a
shared fixture set. Acceptance now requires attributable execution evidence — label changes, milestone
assignments, submitted reviews, pushed commits, agent-linked PRs — rather than target existence; open issues
and PRs stay `pending`; existence-only evaluations become `unknown`/`weak` with
`target_exists_only`, while unsupported evaluations become `unknown`/`none` with
`unsupported_evaluator`. Primary-object deletion is distinguished from supplementary API
failure: a primary issue, PR, or comment 404 is `rejected`/`strong`/`deleted`; other
evaluation failures are `error`/`weak`/`evaluation_error`. Optional PR-effort failures
preserve the authoritative classification but prevent zero-touch acceptance.
Zero-touch requires complete comment, review, and commit evidence and no post-action
non-bot activity, including commits from the PR author. Label identity is compared
case-insensitively, and execution IDs must be positive, integral, in-range values
without malformed numeric prefixes. Every outcome carries a normalized
`(outcome_status, evidence_strength, signal)` triple through collector reports, telemetry, and the audit and
JSONL schemas. To stop re-drift, the monolithic evaluator is split into focused modules
(`outcome_evidence.cjs`, `outcome_action_evaluators.cjs`, `outcome_review_evaluators.cjs`, plus Go helpers in
`outcome_eval_evidence.go`), and the shared conformance cases in `pkg/cli/testdata/outcome_conformance.json`
are executed by both `pkg/cli/outcome_conformance_test.go` and
`actions/setup/js/outcome_conformance.test.cjs`. A TLA+ model (`specs/outcomes/`)
checks the bounded evidence and typed-reporting contract, with fixture projections
and deliberately defective negative controls. It is not an unbounded proof or a
refinement proof of native code.

### Alternatives Considered

#### Alternative 1: Keep two independent evaluators and only fix the known discrepancies

The minimal change would have been to patch the specific mismatching cases in each runtime and leave the two
codebases separate. It was rejected because the divergence is structural, not incidental: with no shared
executable contract, any future evaluator change re-introduces drift silently, and the reports/telemetry
consumed downstream would again mix two definitions of `accepted`.

#### Alternative 2: Collapse to a single runtime (Go-only or JS-only) evaluation

Deleting one implementation removes parity concerns entirely. It was rejected because both call sites are
genuinely needed — the CLI must evaluate outcomes locally without a workflow run, and the scheduled collector
runs inside Actions where invoking the Go binary is not the established path. Unifying the *semantics* via a
shared conformance corpus preserves both entry points without a runtime migration.
A single implementation remains a possible future architecture; this PR does not
require or attempt that migration.

#### Alternative 3: Share semantics through prose specification only

The repository already had `specs/safe-output-outcome-evaluation.md`. Strengthening the prose and relying on
review discipline was considered and rejected: prose cannot fail a build. The spec was instead rewritten
to describe the contract, with the conformance JSON and bounded TLA+ model as executable artifacts.

### Consequences

#### Positive
- Identical safe outputs now receive identical classifications in the CLI and the scheduled collector, so
  telemetry and reports are comparable across sources.
- Acceptance claims are backed by attributable execution evidence, so `accepted` counts are no longer
  inflated by existence checks, unrelated activity, or elapsed time.
- Cross-runtime regressions are covered by shared conformance cases rather than only by reviewers, and the TLA+
  model documents the intended state machine precisely.
- Splitting the 1800-line evaluator into focused modules makes individual action evaluators reviewable and
  testable in isolation.

#### Negative
- This is a behavioural break: existing manifests without required execution evidence now produce `unknown`
  instead of `accepted`, so historical outcome rates shift downward and dashboards may appear to regress.
- The conformance corpus becomes a second place every evaluator change must touch; adding a new safe-output
  type now means Go code, JS code, and fixtures, raising the per-change cost.
- A TLA+ specification plus `specs/outcomes/check.mjs` introduces a modelling artifact that few contributors
  can maintain, risking it becoming stale relative to the implementations.
- More `unknown` and `error` states mean some outcomes are permanently inconclusive rather than optimistically
  resolved, reducing apparent coverage.

#### Neutral
- `schemas/audit.schema.json` and `schemas/logs-jsonl.schema.json` gained fields for the normalized status,
  evidence strength, and signal, and the collector prompt (`.github/workflows/outcome-collector.md`, with its
  recompiled `.lock.yml`) and `docs/src/content/docs/reference/outcomes.md` were aligned to the new vocabulary.
- Collector revisit/retry scheduling and per-outcome AIC attribution are explicitly out of scope and unchanged.
- Unknown, error, and lifecycle counts are now surfaced separately in collector reports, which changes report
  shape for consumers even where classifications are unchanged.

---

The proposal is complete for maintainer review. Acceptance of the architectural
decision remains a maintainer responsibility.
