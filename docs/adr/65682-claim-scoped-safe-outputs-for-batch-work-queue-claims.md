# ADR-65682: Claim-Scoped Safe Outputs for Batch Work Queue Claims

**Date**: 2026-10-05
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

[ADR-64955](64955-git-backed-work-queue-coordination.md) commits the worker boundary to **one** immutable inbound Claim: "Trusted `aw_context.work_queue` supplies one immutable inbound Claim with `work_id`, `claim_id`, and a `work` payload object" and "each worker records at most one Completion and has one pass through safe-output processing." Dispatchers now need to hand a single worker run several queue items at once, but the existing finish and safe-output flow is all-or-nothing: one `work_queue_claim_finish({outcome})` intent gates every safe output of the run. Without a change, a worker that completes three of four assigned items either publishes outputs for the unfinished item or loses the outputs for the three it did finish. Safe-output authority must still be established by trusted reconciliation after agent execution, never by agent-supplied authority fields, and the single-claim protocol must keep working unchanged for existing workflows.

### Decision

We will extend the worker boundary so `aw_context.work_queue` accepts either a single assignment or an **array** of assignments with unique `work_id`/`claim_id` pairs, and we will make safe outputs **claim-scoped**. When a batch is assigned, `work_queue_claim_finish` requires a `claim_id` argument, and every safe-output tool gains an optional `claim_id` property. During reconciliation, each claim is reconciled individually against the durable ledger using a claim-scoped attempt identity (`<attempt>/claim/<claim_id>`), because a queue attempt may complete only one work item. The primary driver is correctness of partial completion: outputs tagged with a claim are applied only after that claim's completion is verified; untagged outputs, asset uploads, and custom safe-output steps/jobs require **all** claims to complete; and a missing finish intent for any claim cancels the entire batch and blocks ordinary safe outputs (fail-closed). A new `all_authorized` job output is exposed alongside `authorized` so downstream jobs can distinguish "some claims applied" (`partial`) from "every claim applied".

### Alternatives Considered

#### Alternative 1: Keep one claim per worker run and dispatch N runs

The simplest option, and it is what ADR-64955 commits to today: dispatchers would start one workflow run per work item, leaving the finish and safe-output flow untouched. It was rejected because it multiplies runner cost and activation latency per item and gives the agent no opportunity to amortize shared context across related work. It also does not remove the underlying need — a batching dispatcher already exists in the PR's motivating scenario.

#### Alternative 2: All-or-nothing batch semantics without per-output `claim_id`

Accept an array of assignments but keep a single authorization decision: if every claim completes, publish all outputs; otherwise publish none. This is a much smaller change (no schema change to safe-output tools, no output filtering in reconciliation). It was rejected because it reintroduces the exact failure this PR targets — one unfinished item discards the verified work of all the others — and makes batches strictly worse than separate runs for reliability. It was a close call on complexity: the chosen design adds a filtering pass over `agent_output.json` and a `claim_id` field threaded through NDJSON collection.

#### Alternative 3: Let the agent declare authority directly in the finish intent

Allow the agent to state which claims it owns and which outputs belong to them without cross-checking the trusted inbound snapshot. Rejected on security grounds: it contradicts ADR-64955's "intent, not authority" boundary. The implementation instead validates every `claim_id` (in the MCP finish tool, in the finish-intent reader, and in output filtering) against the trusted inbound assignment, and drops outputs tagged with unknown claims.

### Consequences

#### Positive

- A worker that finishes some of its assigned items keeps the external effects for those items instead of losing all of them.
- Batching reduces per-item activation and runner overhead for dispatchers that hand out related work.
- Authority remains with trusted reconciliation: claim-scoped attempt identities keep each Completion distinct in the durable ledger, and unknown or agent-invented `claim_id` values are rejected.
- Single-claim behavior, including the legacy `aw_context.work_claim` compatibility path, is preserved unchanged.

#### Negative

- The worker protocol surface grows: an optional `claim_id` on every safe-output tool, a conditionally required `claim_id` on `work_queue_claim_finish`, and two prompt variants. This is more for agents and workflow authors to get right.
- Reconciliation now mutates `agent_output.json` (filtering and stripping `claim_id`), adding a write step on a path that previously only read the artifact, and it re-reads the queue log once per claim.
- The attempt identity format changes for batches (`<attempt>/claim/<claim_id>`), so tooling that parses attempt strings in the ledger must tolerate the suffix.
- ADR-64955's worker commitment ("one immutable inbound Claim", "at most one Completion per worker") is now out of date and must be amended or superseded; otherwise the two records disagree. [TODO: verify whether ADR-64955 will be amended in this PR or a follow-up.]

#### Neutral

- All generated `.lock.yml` workflows gain `work_queue_all_authorized` / `work_queue_authorized` / `work_queue_output_types` job outputs and must be recompiled.
- Gating conditions that previously keyed off `authorized` (for example the work-queue smoke test) must choose deliberately between `authorized` and `all_authorized`.
- Presence of the work-queue snapshot file is used as the signal for advertising `claim_id` on safe-output tools and for validating `claim_id` during NDJSON collection, coupling those components to the snapshot path.
- A new reconciliation status value, `partial`, appears in run summaries.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
