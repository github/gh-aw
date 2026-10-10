# ADR-67322: AW-Managed Work Queues with Automatic Bootstrap and Deployment CAS

**Date**: 2026-10-10
**Status**: Draft
**Deciders**: pelikhan (PR author); gh-aw maintainers (review pending)

---

### Context

The Git-backed work queue (`pkg/workqueue`, `actions/setup/js/work_queue_*.cjs`) required operators to
provision a scheduling Policy before any Work could be admitted. The ESLint factory dispatcher hit
`work_queue_policy_missing`, and an earlier empty-queue run admitted no Work at all, because
queue-specific producer enrollment, author-configured worker principals, and administrative seeding
all had to be done by hand before first use. Scheduling and backing Issue/label settings were also
spread across workflow-level frontmatter (`work-queue-policy`, `tools.work-queue.issues`), so global
economics could be changed implicitly from any single workflow. Separately, routine worker revisions
had no prospective upgrade path: changing a worker invalidated its exact-ref binding, so operators had
to drain the queue — losing queued obligations, fairness debt, and recovery state — just to deploy a
compatible change.

### Decision

We will make AW-managed work queues self-bootstrapping and durably upgradeable:

- **AW owns authorization; the work queue owns scheduling and reliable delivery.** Redundant producer
  enrollment and author-configured worker principals are removed for AW-managed queues, while
  authenticated operation roles, caller-local approved targets, credential/run binding, Claim
  ownership, and explicit GitHub permission failures are retained.
- **Global scheduling and backing Issue/label configuration is centralized** in the `work_queue`
  section of `.github/workflows/aw.json`. A missing file/section or `{}` yields documented defaults
  (one weighted-priority pool, concurrency 16, pending limit 4,096, singleton assignments, three
  attempts with 30-second backoff); invalid or conflicting settings fail explicitly.
- **The first accepted submission atomically publishes the Policy and the first Work**, in both the
  JavaScript and native Go implementations. Racing submissions retry against the winning installed
  Policy without duplicate admission. Reads, activation, and dispatch-only requests never seed a queue.
- **The installed Policy remains authoritative.** Economic and shared-resource changes require an
  explicit, quiescent `policy --from-config --epoch EPOCH`; editing config does not silently replace it.
- **Worker evolution uses a durable Deployment CAS.** Compiler-derived logical contracts cover
  authority/interface declarations and imports — not prompt, engine, or source identity — so compatible
  worker revisions deploy without draining the queue. New admissions freeze the approved contract,
  explicit `execution_ref` pins stay pinned, and incompatible, unavailable, or locally unapproved
  workers pause only the affected Work without charging it or broadening authority.

The primary drivers are first-use ergonomics (no manual seeding before a queue is usable) and
operational continuity (deploy worker revisions without destroying queued obligations).

### Alternatives Considered

#### Alternative 1: Keep explicit administrative seeding and producer enrollment

Operators would continue to run a one-time `policy`/seed command and enroll producers per queue before
any submission. This preserves the strongest "nothing exists until an administrator says so" property
and the simplest audit story, and it was a close call for that reason. It was rejected because the
observed failures (`work_queue_policy_missing`, empty-queue runs) are precisely the cost of that
model: every new dispatcher requires out-of-band administrative work, and a forgotten step silently
degrades to "admitted nothing" rather than an actionable error.

#### Alternative 2: Keep exact-ref worker binding and drain the queue for every worker revision

Each worker revision would continue to be pinned by exact ref, with upgrades handled by quiescing
writers and draining outstanding Work. This keeps the authority model trivially verifiable: an
assignment can only ever run the exact commit it was admitted against. It was rejected because routine
revisions are frequent while draining is destructive and slow — it discards queued obligations,
fairness debt, reservations, and recovery context for changes that do not alter authority or interface
at all. Deployment CAS narrows the invariant to what actually matters (the logical contract) instead of
the whole source identity.

#### Alternative 3: Keep global scheduling configuration in workflow frontmatter

Scheduling economics and backing Issue settings would remain declarable per workflow. This requires no
new repository-level config surface and keeps a workflow self-describing. It was rejected because
global, shared-resource economics configured from one of many workflows is ambiguous by construction:
two workflows can disagree, and the "winner" is an accident of ordering. Centralizing in
`aw.json` with explicit conflict rejection makes the shared resource have exactly one declared owner.

### Consequences

#### Positive
- A new dispatcher works on first submission with no administrative seeding or per-queue producer
  enrollment, removing the `work_queue_policy_missing` class of failure.
- Compatible worker revisions deploy drain-free, preserving Work IDs, dependencies, Results, fairness
  debt, reservations, and frozen dispatch/credential bindings for in-flight completion and reconciliation.
- Shared scheduling economics have a single declared location (`aw.json` `work_queue`), with explicit
  failures instead of silent precedence between workflows.
- Incompatible or unapproved workers pause only the affected Work, rather than failing it, charging it,
  or widening authority.

#### Negative
- This is a breaking change: standalone seeding, producer allowlists, and workflow-level
  `work-queue-policy` / `tools.work-queue.issues` configuration are removed or deprecated, and
  conflicts are rejected, so existing repositories must migrate.
- Rollout is ordered and therefore error-prone: upgraded readers/runtimes must be deployed before
  contract-stamped immutable workers, and historical unmarked Policies keep exact-ref semantics until an
  explicit quiescent migration.
- Automatic bootstrap moves Policy creation into the submission path, so an unintended first submission
  can install a default-economics queue; preventing that now relies on the dispatch/activation paths
  correctly never seeding.
- Deployment CAS adds a compiler-derived contract-stamping surface that must be kept correct (this PR
  already fixes one such defect: worker `RawMarkdown` carries only the body, so stamps must include
  authority frontmatter), and large shared Policy proposals must be chunked under the Actions per-value
  limit and reconstructed byte-for-byte.

#### Neutral
- Historical Policy-only genesis remains readable, and historical availability updates use
  `activate:false` without repromoting old routes.
- Native `deploy --from-config` and `submit-work --execution-ref` share the same semantics as their
  JavaScript counterparts, keeping the two runtimes behaviourally paired.
- Existing dispatchers (ESLint, daily-report, engine-conformance) were migrated and workflow locks
  regenerated; TypeSpec schemas, Go/JavaScript tests, bounded TLA+ models, and protocol/deployment docs
  were synchronized in the same change.
- Pre-existing backing Issues, cross-repository projection, and closure still require explicit trusted
  target grants; ordinary same-repository Issue projection requires no projector enrollment.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
