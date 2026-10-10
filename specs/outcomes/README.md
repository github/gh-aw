# Outcome evaluation: executable TLA+ specification

`OutcomeEvaluation.tla` specifies snapshot-based outcome classification and its
typed reporting contract. `OutcomeEvaluation.cfg` checks a finite abstraction of
the current Go/JavaScript rules, including creation, engagement, close provenance,
retained changes, review attribution, milestone identity, pushed-commit ancestry,
agent assignment, dispatch, unsupported types, and generic fallback.

## Model and assumptions

One evidence record is chosen nondeterministically. Abstract Go and JavaScript
evaluators compute in either order, then publish their typed results and count
each status. There are six phases and two evaluator identities; the count vector
represents two observations of the same action, not two production actions.
Evidence is immutable during this evaluation. `WF_vars(Next)` excludes indefinite
stuttering before publication; it is an assumption, not a guarantee about the
collector scheduler.

The finite universe has **2,712 distinct evidence records**. Each action family
varies relevant state, missing/available execution evidence, API success/failure,
reference availability, actor provenance, time eligibility, or retention.
PR effort additionally varies completeness, human activity, and timestamp
validity. Actors, timestamps, snapshots, reviews, and SHAs are abstracted into
facts rather than unbounded concrete strings, histories, or API pages.

`Classify` is the normative classifier. `Evaluate` normally delegates to it for
both abstract engines; the `Bug` constant enables deliberate deviations.
`RuntimeParity` alone therefore does not prove that native runtimes agree.
`AcceptanceEvidence` checks refinement to the normative classifier; independent
invariants also constrain the evidence that can justify acceptance.

| Invariant or property | Requirement |
|---|---|
| `TypeOK` | Valid phase, status, strength, and zero-touch flag |
| `PrimaryFailureCannotAccept` | Primary API failure cannot justify acceptance |
| `AcceptanceEvidence` | Accepted results require normative action-specific evidence |
| `ExecutionAttributionRequired` | Identity-sensitive actions require recorded, matched execution evidence |
| `ReviewRequiresPostActionHuman` | Ready-for-review acceptance requires a strictly post-action visible non-bot review |
| `OpenCreationsPending` | Open PR/issue creation is pending, not accepted through existence or engagement |
| `DeletionRequiresPrimary404` | Deleted signal requires a primary-object 404 |
| `ZeroTouchRequiresCompleteEvidence` | Accepted PR plus valid action time, complete effort evidence, and no human activity |
| `RuntimeParity` | Both abstract engines return identical typed results |
| `TypedExportPreserved` | Publication does not reinterpret result, strength, signal, or zero-touch |
| `SummaryReconciles` | All statuses, including unknown/errors/lifecycle/skipped, are counted exactly |
| `FixtureAgreement` | Input projections classify to the shared fixture's expected typed result |
| `EventuallyPublished` | Publication and aggregation eventually finish under weak fairness |

## Verify

Use Node.js and a working Java runtime with the official
[TLA+ Tools v1.7.4 JAR](https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar).
The checker rejects any other SHA-256:
`936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88`.
It does not download tools or install dependencies.

From the repository root:

```sh
JAVA_BIN=/path/to/java TLA2TOOLS_JAR=/path/to/tla2tools.jar \
  node specs/outcomes/check.mjs /path/to/verification-output
```

Omit the output argument to use a new temporary directory. Each TLC process is
limited to two workers, 1 GiB heap, seed 1, and 60 seconds. Successful positive
runs require exit zero, TLC's exhaustive-success verdict, and an empty remaining
state queue. Each negative control requires exit 12 and its exact named invariant
violation; syntax errors, timeouts, and unrelated failures do not count.

The output contains configs, generated fixture module, raw logs, input
projections, and `report.json` with source/tool/fixture/projection hashes, command
arguments, state counts, Java version, and verdicts. Retain that directory for
run-level evidence; generated artifacts are not source files.

## Verified bounds and controls

TLC 2.19 (official v1.7.4 distribution) exhaustively checked the baseline:
**18,984 generated states, 16,272 distinct states, depth 5, zero queued states**.
The fixture model checked **all 63 shared cases**:
**371 generated states, 318 distinct states, zero queued states**. Equal projected
records collapse into the same initial state, explaining the smaller state count.
These results are reproducible bounds, not a proof over arbitrary inputs.

All ten negative controls produced their required invariant counterexamples:

| Deliberate defect | Required invariant violation |
|---|---|
| Target existence accepted | `AcceptanceEvidence` |
| Open issue bot activity accepted | `OpenCreationsPending` |
| Merged push without execution identity accepted | `AcceptanceEvidence` |
| Supplementary API failure treated as deletion | `DeletionRequiresPrimary404` |
| Incomplete effort evidence marked zero-touch | `ZeroTouchRequiresCompleteEvidence` |
| Typed result reinterpreted on publication | `TypedExportPreserved` |
| Unknown results omitted from summary | `SummaryReconciles` |
| Submitted review borrows an unrelated review ID | `ExecutionAttributionRequired` |
| Team review assumed attributable without membership proof | `ExecutionAttributionRequired` |
| Review at/before action boundary accepted | `ReviewRequiresPostActionHuman` |

## Connection to runtime behavior and limits

`check.mjs` independently projects raw inputs from
`pkg/cli/testdata/outcome_conformance.json` into model facts. It never derives
facts from expected results and rejects unmapped fixture types. Expectations are
checked by TLC, including status, evidence strength, signal, and zero-touch.
Go and JavaScript execute the same corpus through their native conformance tests:

```sh
go test ./pkg/cli -run '^TestOutcomeCrossRuntimeConformance$' -count=1
npm --prefix actions/setup/js exec -- vitest run outcome_conformance.test.cjs
```

Fixture agreement is an example-based bridge, not a refinement proof of the
adapter, native evaluators, or every API payload. The model does not prove
pagination completeness, string normalization, actor authenticity, timestamp
parsing, merge ancestry returned by GitHub, transport persistence, collector
revisits, or economic/AIC attribution. It also does not prove unbounded
multi-action aggregation or telemetry delivery. No TLAPS, F*, or Z3 proof is
claimed.
