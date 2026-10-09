# ADR-67025: Mirror Git-backed Work Queue Work to Scoped GitHub Issues

**Date**: 2026-10-08
**Status**: Accepted
**Deciders**: pelikhan (approved implementation plan)

---

### Context

The work queue (`pkg/workqueue`, `actions/setup/js/work_queue_*.cjs`) keeps its authoritative state in a Git-backed ledger: Work, Claims, and Results are committed as checked records with expected-head publication. That design gives durability and verifiable authority, but it is effectively opaque to humans — there is no place in the GitHub UI where a maintainer can watch admitted Work progress through claim, execution, and conclusion. Operators asked for a human-facing view without weakening the properties the ledger provides: scheduling, Claim authority, and Result verification must continue to derive solely from the ledger, and agent execution must never receive queue-write or Issue-write credentials. Any mirror must also tolerate partial failure (missing permissions, unprovisioned organization fields, ambiguous writes) without corrupting the queue or creating duplicate Issues.

### Decision

Project already-admitted Work into scoped GitHub Issues as a **one-way, derived
mirror**, enabled by optional `tools.work-queue.issues` configuration. Every
enabled configuration has a tracking label, defaulting to `work`. Omit
`status-field` for comment-only status, or name a separately provisioned native
organization single-select field. `WorkStatus` is a convention, not a reserved
name. A missing or unwritable requested field remains explicitly pending,
without silent fallback or queue rollback.

Extend the shared closed version-3 Go/JavaScript/TypeSpec contract with immutable,
lossless `backing_issue`, checked `IssueLink` and `IssueComment` records, and
installed projector rules. A backing Issue tracks one Work. Scope every
mutation to this run's checked admissions or original authenticated Claims,
including workflow revision, principal, run and attempt. Worker reruns cannot
inherit attempt-one Claim authority or establish authenticated execution.

Use only existing activation and conclusion hooks, with fresh checked reads,
expected-head GraphQL publication, durable receipt journals, and per-Work/Issue
coordination. No new job, trigger, Project, intake path, or global reconciliation
loop is introduced. Derive status and policy-gated closure from the ledger:
Completion means Verifying, verified PR delivery means Needs review, and native
job success or human Issue edits do not establish Result. Preserve human text,
unrelated labels/fields and discussion.

Persist creation intent before native Issue/comment writes. A crash before
sending cannot be distinguished from an accepted remote write whose receipt was
lost. GitHub provides no documented creation deduplication, and neither markers
nor bot identity prove origin. Prefer explicit pending synchronization, even
indefinitely, to duplicate creation or stale writes. Retained coordination refs
never expire or get stolen; automatic safe recovery of interrupted phases is
not implemented. `withRetry` retries only proven pre-execution creation
rejections, never ambiguous writes. Review of this PR does not imply live
intended-token API validation or approval of future recovery mechanisms.

### Alternatives Considered

#### Alternative 1: Bidirectional sync with GitHub Issues as an intake path

Human-to-Work intake and bidirectional synchronization would need separate
authenticated admission and command semantics. They are outside the approved
scope. Human Issue edits cannot substitute for checked Claims or verified
Results.

#### Alternative 2: A separate reconciliation job/workflow with global sweep

An independent reconciliation service could repair drift when the original
hooks no longer run, but would require separately installed authority and a new
operational surface. It is outside the approved scope. A shared queue or label
does not authorize the existing hooks to repair other workflows' Work.

#### Alternative 3: GitHub Projects (v2) board instead of Issues

Project-local status requires a Project and different provisioning/API contracts.
The approved design uses Issues with comments and optional native organization
fields instead. Comment-only status is selected by omitting `status-field`, not
by silently falling back when a requested field is unavailable.

### Consequences

#### Positive

- Maintainers get a human-readable, notification-capable view of admitted Work, Claims, and conclusions without touching ledger internals.
- The ledger remains the single source of truth: all mirror state is derived, so a broken or disabled mirror can never corrupt scheduling or Result verification.
- Mutations are narrowly scoped and attributable (workflow revision, principal, run, attempt), and agents still receive no writer credentials.
- Human text, unrelated labels/fields, and discussion on mirrored Issues are preserved, so the mirror coexists with normal maintainer workflow.

#### Negative

- The closed version-3 wire contract gains new record shapes without a version-number bump: **every reader and deployment must be upgraded before the new records are enabled**, because older readers reject unknown fields and operations.
- Significant new surface area (~1500 added lines across `pkg/workqueue`, `pkg/workflow`, `actions/setup/js`, and 45 generated schemas) must be maintained and kept in sync across Go, JavaScript, and TypeSpec.
- Protected hooks now request `issues: write` in addition to `contents: write` and `actions: read`, widening the token scope of the activation and conclusion hooks.
- Interrupted phases and unprovable native writes can leave synchronization pending indefinitely. A later authorized hook cannot clear retained locks or safely recreate an unreceipted Issue/comment; automatic recovery of those fences is deliberately not claimed.

#### Neutral

- The feature is opt-in via `tools.work-queue.issues`; absent or false preserves disabled-mode behavior. New protocol records still require coordinated reader deployment.
- Administrators must provision native status fields/options out of band; missing or unwritable fields leave the queue committed and synchronization pending rather than failing the run.
- Hook batches contain up to 25 owned targets. Authenticated mocks measure 6 requests for 25 unchanged Issues, 9 for an existing Issue with a new summary, and 15 for new Issue creation plus bindings and coordination. Ordinary checked read/publication takes 2 requests. Both hooks, shared activation work, App token minting, pagination and retries add to the total; these are not end-to-end limits or live mutation-cost measurements.
- Issue bodies, summaries, and Claim comments live as templates in `actions/setup/md` and always carry generated-by attribution, making the mirror visibly machine-authored.

---

This ADR records the user-approved implementation scope. Maintainer review and
live intended-token validation remain separate from acceptance of that scope.
