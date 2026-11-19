# Safe-output delivery and adoption

Status: implementation contract.

## Ownership

gh-aw owns producer facts, the versioned lifecycle registry and reducer,
provider-observation adapters, retained observations, and logs/audit/episode/
experiment projections. GitHub owns resource disposition. CAO may schedule
refresh, store/query records, and compute longitudinal attributed value; it must
consume the gh-aw contract rather than introduce another adoption classifier.

The implementation is `pkg/lifecycle` with acquisition in
`pkg/cli/logs_lifecycle_*.go`. No standalone lifecycle command or compatibility
aliases exist.

## Delivery

Delivery MUST come from successfully applied execution evidence, not a request,
staged preview, deferred placeholder, skipped action, failed handler, or whole-run
success. A failed workflow can have delivered effects.

Identity MUST distinguish host, source repository, workflow, run, run attempt,
invocation, source evidence, output type, and available exact target.
Identity is assigned before optional request enrichment. Repeated snapshots MUST
not create additional deliveries. Distinct actions and invocations MUST remain
distinct even when they affect the same target.

Delivery event time and observation time MUST be separate. Missing delivery time
MUST NOT be replaced with polling time. Target hosts MUST match the recorded
execution host; response identity MUST match the exact delivered resource.
Cross-repository targets on that host use their own repository.

Missing evidence MUST NOT imply zero deliveries. An executed empty manifest or
a verified skipped safe-output job can establish an empty selection. Unsupported
custom types can establish delivery but not adoption.

## Adoption registry

`gh-aw-adoption-v1` records each built-in type's effect class, parent resource,
human-decision, rejection and reversal signals, explicit delivery fallback,
value eligibility, and survival requirement. The registry includes proposal,
publication, mutation, delegation, and infrastructure classes.

The public states are `pending`, `accepted`, `rejected`, `lifecycle`,
`not_applicable`, and `unknown`. API availability is coverage, not a decision.
Unknown types fail closed. Infrastructure is not value-eligible.

A qualifying human decision MUST have an authoritative event clock, visible
actor evidence excluding known bots/publishers, and supported authorization
evidence. The current issue/PR adapter checks current collaborator authorization;
it cannot attest historical authorization or undisclosed machine-user ownership.
Missing proof MUST remain unknown.

A bot resolution with a visible actor login MUST record the observed resolution
(for example, `accepted`/`merged` or `rejected`/`closed_without_merge`) with
`acceptance_kind: automation`, the deciding `actor` login and type, and
`human_verified: false`. It is never human adoption. A bot resolution without an
actor login remains unknown. Retained `lifecycle`/`automation_decision` records
predate this rule and are replaced when refreshed.

Only rules explicitly allowing fallback may establish adoption at delivery.
They MUST report `acceptance_kind: fallback_delivery` and MUST NOT claim human
verification. Human decisions report `acceptance_kind: explicit_human`.

The current provider refresh covers created issues/PRs and supported existing
effect adapters. Registry signals are the shared semantic vocabulary, not a claim
that every provider event listed by a rule is already acquired. Other proposals
remain pending/unobserved; unsupported effect survival remains unknown.

## Merge and survival

Replayed delivery MUST NOT overwrite stronger or later provider evidence.
Conflicting identities, delivery clocks, rule versions, or equal-clock decisions
MUST fail explicitly. API failure, including 404, MUST NOT fabricate deletion.

The current state can establish an observed reversal without its event time.
Polling time MUST NOT be substituted for an unknown reversal event time.
Current retention/adoption snapshots MUST NOT attest full-window survival.

Attributed value requires a declared measurement, attribution evidence, and
survival through the complete post-adoption measurement window. Adoption counts
and objective-label weights MUST NOT substitute for measured attributed value.
Operational-value grader results and original execution sessions are immutable.
Refresh MUST NOT execute in conclusion or grant grader network access.

## Existing command integration

`logs --delivery` reports recorded delivery without later disposition queries.
`logs --adoption` includes delivery and provider refresh. `audit` exposes the
same evaluator in single-run mode; live data MUST NOT enter its static cache.
`logs episode --adoption` uses the same selected-member join and MUST preserve
repository, request, pagination, and wall-clock bounds. When one selected run's
lifecycle evidence is malformed or unreadable, `logs` MUST record it in
`adoption_errors` with `unavailable` coverage and continue with the other runs.

Public JSON uses `delivery`, `adoption`, `adoption_errors`, `adoption_summary`, and
`episode_adoption`. Experiment references use `adoption:accepted` and
`adoption:zero_touch`, with `adoption_window` and explicit `analyze --adoption`.
Old flags, selectors, field names, and metric references are not aliases.

`value WORKFLOW` consumes these same retained native records and
summary projections for any workflow; value functions MUST NOT replace the
delivery/adoption classifier. Missing measurement definitions leave value
unavailable without suppressing lifecycle evidence. Versioned measurement
definitions select named Go evaluators over immutable Git snapshots or bounded,
typed evidence bundles; the report
MUST NOT launch JavaScript, execute workflow code, or silently fall back to
another evaluator. Unsupported query/evaluator declarations fail explicitly.
Recipes fix discovery/cohort and inspected-window policies separately from
measurement semantics. Descriptive batches MUST NOT acquire attributed value.
Reports retain evaluator identity, declaration digests, resolved Git ref, and
collected evidence; live API reads are not frozen historical evidence replay.
The reporting command MUST NOT mutate lifecycle records or historical graders.
PR attribution additionally requires exact native human adoption, an isolated
merge, and complete-window Git file survival; other output types retain their
lifecycle projections without fabricated longitudinal value.

Window policies MUST record version, baseline strategy, pre/post periods,
outcome lag, independent durability, cadence, minimum samples, and overlap
strategy. The seven-day slide policy is a compatibility default, not a universal
validated horizon. Historical attribution MUST wait for the full outcome,
durability, and forward-isolation horizons. Delayed aggregate improvements
MUST remain unattributed without subject linkage.

Imported `evidence-number-v1` observations MUST bind repository, workflow,
metric, scope, subject, run, source references, and event/collection clocks.
Unknown declarations, conflicting endpoints, malformed clocks, and nonfinite
values MUST fail explicitly. Missing independent evidence MUST remain unavailable.
Caller-supplied values MUST NOT authenticate adoption or survival, or acquire
attributed value. Elapsed-time maturity MUST NOT imply complete temporal
sampling; evidence-only replay MUST expose that sampling was not evaluated.

Compact retained observations use the existing logs JSONL schema with
`kind: output_lifecycle`, independently of run-folder retention. Retention is
365 days from delivery, at most 20,000 identities and 16 MiB. Concurrent writes
MUST serialize or fail explicitly rather than lose observations. Snapshot files use sensitive
permissions and atomic replacement. Invalid retained evidence MUST fail closed.

`logs --adoption --retained --repo OWNER/REPO` refreshes the scoped retained
population without run discovery, run metadata, or source artifacts. `--count`
bounds due delivery checks; time and API-quota bounds remain enforced. The
`retained_adoption` projection distinguishes refreshed and deferred records.
Oldest provider checks are serviced first, with a 24-hour check interval.
Execution selectors needed by provider adapters MUST survive cold restoration;
conflicting replayed selectors MUST fail explicitly.

Scheduled repository collection MUST persist its ledger and discovery cursor
together through repo-memory, separately from aggregate metric files. A missed
execution MUST NOT reset discovery to a rolling 24-hour cohort. Bounded or failed
work MUST remain retryable and visible as partial coverage. Provider failures
MUST NOT become fabricated adoption decisions. The metrics collector's prepared
`metrics/lifecycle.json` is a projection, not the authoritative per-item store.
No webhook delivery or agent classification is required for reconciliation.

## Verification

Core tests cover registry defaults, distinct actions/attempts, verified decisions,
reopening, replay, and equal-clock conflicts. CLI tests cover evidence acquisition,
host/resource binding, cached observations, provider availability, and budgets.
Real-run trials MUST use completed workflows unless a separately authorized
dry-run execution satisfies the repository dispatch gates.

Existing low-level provider-adapter conformance remains in the 95-case fixture corpus
`pkg/cli/testdata/outcome_conformance.json`, executed by
`pkg/cli/outcome_conformance_test.go`. These fixtures check effect observations,
not the complete adoption contract. Their executable model is documented in
`specs/outcomes/README.md`. Lifecycle acceptance is governed by the shared core,
not by a provider adapter's generic retained-existence classification.
