# ADR-73922: Bind PR Merges to the Evaluated Head with Workflow-Owned Gates

**Date**: 2026-11-01
**Status**: Draft
**Deciders**: Unknown; pending maintainer review of [PR #73922](https://github.com/github/gh-aw/pull/73922)

---

### Context

The experimental `merge-pull-request` safe output merges into non-default, unprotected integration branches that are themselves the head of an open upstream pull request. These branches cannot rely on protected-branch enforcement alone, and a PR head or its policy evidence can change between evaluation and merge. Workflow authors need optional CI and independent approval gates without allowing the agent's merge message to choose its own authorization policy. Missing or malformed review evidence must not be interpreted as approval.

### Decision

Bind every new merge attempt to the evaluated PR head SHA through the merge API's `sha` precondition, and do not automatically retry the merge mutation. Keep `required-checks` and `required-approvers` in workflow-owned configuration, combine configured check names with discovered branch-policy requirements, and fail closed on missing, stale, malformed, or blocking review evidence. Revalidate relevant PR state, branch eligibility, the upstream PR's existence, checks, and reviews before either a merge or a staged preview; changed policy evidence requires a fresh evaluation. Return structured `decision_evidence` for evaluated decisions and record count- and phase-oriented debug events without dumping review bodies or full API payloads.

Compare stable authorization inputs separately from asynchronously computed mergeability. Bounded read retries allow mergeability computation to settle; unconfirmed mergeability and conflicts still block execution, without retrying the merge mutation.

When configured, each required check must have a successful, completed check run on the evaluated SHA. `required-check-apps` can bind named checks to workflow-owned GitHub App IDs. Effective changes-requested reviews always block, even without an approver allowlist or aggregate blocking review decision. The approval gate requires at least one allowlisted GitHub `User` account's latest effective approval on that SHA, excluding both the PR author and the acting identity; it does not require every allowlisted account to approve. CI and affirmative approval remain opt-in gates. Existing draft, mergeability, requested-reviewer, unresolved-thread, label, title, source-branch, and integration-branch constraints remain in force.

The handler manager exposes only summary-safe gate codes and minimized evidence for blocked decisions. Audit artifacts for successful, blocked, and staged decisions retain run identity, outcome, fixed failure codes, and an allowlisted repository/PR identity, head/base revisions, and evaluation timestamps, not titles, labels, branches, check names, reviewer identities, or approval details. Producer rejections retain only fixed phase/code and validated check-run/app IDs; early target rejections retain a fixed code and valid attempted identity when available. Records remain bounded. Uploads require successful standard tracked-secret redaction and configured custom masking; secret tracking collects uppercase `secrets.NAME` references from preceding generated steps rather than claiming exhaustive detection.

Audit persistence is separate from the operation outcome. If writing the audit fails after a completed merge, preserve the successful merge result, revision, URL, and internal decision evidence while surfacing an explicit audit-failure warning. Do not retry the merge or report that the merge failed merely because its audit could not be written.

Global staged and trial runs retain merge decision audit artifacts while suppressing applied-item manifests. A staged preview never calls the merge API and does not claim that a merge occurred.

### Alternatives Considered

#### Alternative 1: Rely only on branch protection or rulesets

Server-enforced policy is preferable where applicable because it can reject changes atomically at the repository boundary. However, this safe output deliberately excludes default and protected target branches, so repository enforcement alone does not express the workflow's integration-branch policy. Workflow-owned gates supplement rather than replace repository protections.

#### Alternative 2: Bind only the head SHA

A SHA precondition prevents merging a newer, unevaluated head and is the essential atomic check. It does not establish that checks passed, an independent account approved, review evidence was complete, or the branch remained eligible. Keeping that precondition without policy revalidation would leave those decisions dependent on stale observations.

#### Alternative 3: Accept policy from the agent's merge message

Agent-supplied check names and approvers would allow per-request flexibility without changing workflow configuration. It would also let the actor requesting the merge weaken its own gates, defeating the separation between workflow policy and agent intent. The message therefore selects the merge request and supported merge options, not the required-check or approver policy.

### Consequences

#### Positive

- A changed head cannot silently replace the revision that was evaluated; an unsuccessful or ambiguous mutation is not automatically repeated.
- Workflow authors can require CI and independent head-specific approval on integration branches, while structured evidence makes the evaluated decision inspectable.
- Missing or inconsistent review evidence blocks the operation, and staged previews pass the same final policy revalidation without performing the merge.

#### Negative

- Only the head SHA is an atomic merge precondition. Other policy inputs can still change after the final reads; revalidation narrows but does not eliminate that race.
- Required check names are not producer authentication unless pinned with `required-check-apps`. Even a pinned app ID cannot distinguish independently protected CI from agent-editable workflows using the same app, and a GitHub `User` account is not proof that a person performed the review.
- Additional reads and review pagination increase latency, permission requirements, and sensitivity to API failures or concurrent edits. Check-run reads need `checks: read` even when requirements originate from branch policy rather than an explicit workflow list.
- A revert can restore repository content but cannot undo deployments, notifications, or other external effects caused by a merge.

#### Neutral

- The feature remains experimental and restricted to eligible integration branches; this record does not establish maintainer approval or broaden merge targets.
- Labels, title prefixes, and source-branch patterns remain workflow filters, not authorization credentials.
- Empty required-check and required-approver lists do not establish mandatory CI or independent approval. Workflow authors remain responsible for selecting those policies and trusted check producers.
- An already-merged PR retains its idempotent success path without a new merge or fresh `decision_evidence`; early request-validation failures may also precede evidence collection.

---

*Draft record for PR #73922. Review and finalize before changing status to Accepted.*
