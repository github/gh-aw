# ADR-67025: Mirror Git-backed Work Queue Work to Scoped GitHub Issues

**Date**: 2026-10-08
**Status**: Draft
**Deciders**: pelikhan [TODO: verify additional deciders]

---

### Context

The work queue (`pkg/workqueue`, `actions/setup/js/work_queue_*.cjs`) keeps its authoritative state in a Git-backed ledger: Work, Claims, and Results are committed as checked records with expected-head publication. That design gives durability and verifiable authority, but it is effectively opaque to humans — there is no place in the GitHub UI where a maintainer can watch admitted Work progress through claim, execution, and conclusion. Operators asked for a human-facing view without weakening the properties the ledger provides: scheduling, Claim authority, and Result verification must continue to derive solely from the ledger, and agent execution must never receive queue-write or Issue-write credentials. Any mirror must also tolerate partial failure (missing permissions, unprovisioned organization fields, ambiguous writes) without corrupting the queue or creating duplicate Issues.

### Decision

We will project already-admitted Work into **scoped GitHub Issues as a one-way, read-only mirror**, enabled by an optional `tools.work-queue.issues` configuration with a default `work` label and an optional pre-provisioned native organization single-select status field. The shared Go/JavaScript/TypeSpec contract is extended with immutable, lossless `backing_issue`, `IssueLink`, and `IssueComment` records plus installed projector rules, so every mirror mutation is scoped to this run's checked admissions or original authenticated Claims (workflow revision, principal, run, attempt). Projection runs inside the existing activation and conclusion hooks using fresh checked reads, expected-head GraphQL publication, durable receipt journals, and per-Work/Issue coordination — no new job, trigger, Project, Issue intake path, or global reconciliation loop is introduced. Issue status and policy-gated closure are derived from the ledger rather than from job success, and `withRetry` is used only for proven pre-execution creation rejections, never for ambiguous writes.

### Alternatives Considered

#### Alternative 1: Bidirectional sync with GitHub Issues as an intake path

Allow humans to open Issues that are admitted into the queue, and let Issue edits (labels, closure, assignment) feed back into ledger state. This is the most "natural" GitHub experience and was seriously considered. It was rejected because it would make an unauthenticated, human-editable surface a source of queue authority, breaking the invariant that admission, Claim authority, and Result verification come only from checked ledger records. It also introduces reconciliation races that cannot be resolved by expected-head publication alone.

#### Alternative 2: A separate reconciliation job/workflow with global sweep

Run a scheduled job that periodically diffs the whole ledger against all `work`-labeled Issues and repairs drift. This is simpler to reason about for eventual consistency and would self-heal after outages. It was rejected because a global sweep needs broad `issues: write` credentials outside the scope of any specific admission or Claim, cannot attribute mutations to an authenticated principal/run/attempt, and would add a new trigger and operational surface. The hook-scoped projector keeps credentials and blast radius tied to the run that already holds authority.

#### Alternative 3: GitHub Projects (v2) board instead of Issues

Model Work items as Project items with custom fields. Rejected for this iteration because Projects require provisioning and API surface beyond what most deployments have, and Issues already provide comments, labels, and notifications. The design instead reuses a *pre-provisioned* native organization single-select status field when available, and degrades to label-only status when it is not.

### Consequences

#### Positive

- Maintainers get a human-readable, notification-capable view of admitted Work, Claims, and conclusions without touching ledger internals.
- The ledger remains the single source of truth: all mirror state is derived, so a broken or disabled mirror can never corrupt scheduling or Result verification.
- Mutations are narrowly scoped and attributable (workflow revision, principal, run, attempt), and agents still receive no writer credentials.
- Human text, unrelated labels/fields, and discussion on mirrored Issues are preserved, so the mirror coexists with normal maintainer workflow.

#### Negative

- The shared contract gains a closed-schema version bump: **every version-3 reader and deployment must be upgraded before the new records are enabled**, which is a coordinated rollout cost.
- Significant new surface area (~1500 added lines across `pkg/workqueue`, `pkg/workflow`, `actions/setup/js`, and 45 generated schemas) must be maintained and kept in sync across Go, JavaScript, and TypeSpec.
- Protected hooks now request `issues: write` in addition to `contents: write` and `actions: read`, widening the token scope of the activation and conclusion hooks.
- Unprovable native writes retain non-expiring coordination rather than retrying, so a failed ambiguous write can leave synchronization explicitly pending until an authorized hook for the same Work/Claims runs again — requiring human attention in rare cases.

#### Neutral

- The feature is opt-in via `tools.work-queue.issues`; deployments that do not configure it see no behavioural change beyond the schema version requirement.
- Administrators must provision native status fields/options out of band; missing or unwritable fields leave the queue committed and synchronization pending rather than failing the run.
- Per-run request budgets are bounded and measured in mocked tests (6 requests for 25 unchanged Issues, 9 for an existing Issue with a new summary, 15 for new Issue creation plus bindings and coordination), on top of the ordinary 2-request checked read/publication.
- Issue bodies, summaries, and Claim comments live as templates in `actions/setup/md` and always carry generated-by attribution, making the mirror visibly machine-authored.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
