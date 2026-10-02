# ADR-64955: Git-Backed Dispatch Work Coordination

**Date**: 2026-10-02
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

The [Dispatch Work Coordinator proposal in issue #64852](https://github.com/github/gh-aw/issues/64852) needs a durable Work queue shared by independent dispatchers and worker workflows. Concurrent Claims, stale reads, workflow failures, and compaction must not produce conflicting terminal decisions or authorize losing workers' external effects. Agent execution can be duplicated, so admission checks alone cannot establish final ownership. The design should reuse repository storage and existing workflow job boundaries rather than require an external coordination service.

### Decision

Implement `tools.dispatch-work-coordinator` as a first-class, compiler-aware tool backed by one log of immutable transactions, `dispatch-work-coordinator.jsonl`, on a dedicated coordinator branch. Derive authority through one shared deterministic replay implementation, using the issue's protocol and its [TLA+ specification](../../specs/dispatch-work-coordinator/DispatchWorkCoordinator.tla) as the design baseline. Defer dispatcher mutations and worker completion to trusted `safe_outputs` processing, preserving the existing `activation -> agent -> detection -> safe_outputs -> conclusion` topology without an additional worker job. This pull request records and models the proposed protocol; it does not implement the runtime feature.

#### Protocol commitments

| Boundary | Commitment |
|---|---|
| Replay | The same valid transaction set produces the same projection regardless of record order, commit order, or timing. The model selects the smallest uncancelled Claim identity in a stable ordering on nonterminal Work; all consumers share arbitration. |
| Dispatcher | Agent-facing tools replay current remote facts plus session-local pending transactions. Every proposed transaction is appended to both the internal log and the safe-output log; only trusted safe-output processing publishes it. Pending Claims do not establish durable authority. |
| Worker | Trusted `aw_context` supplies one immutable inbound Claim. Activation is an early admission check, not final authority. `dispatch_claim_finish(outcome?)` records intent without accepting authority fields; each worker records at most one Completion and has one pass through safe-output processing. |
| External effects | Before ordinary safe outputs, refresh and replay, verify ownership, persist Completion, and confirm the terminal result. Missing finalize cancels an effective Claim; losing workers stage intents and stop without external effects. Uncertain reconciliation fails closed. |
| Publication | Publish only if the branch still matches the version originally read. Otherwise fetch the latest log, replay it, and regenerate the proposed changes. Apply this rule to dispatchers, workers, orphan recovery, compaction, and concurrent branch initialization, with bounded retries and diagnostics. |
| Terminal Work | Reject new state-changing transactions after Work becomes completed or cancelled. Freeze its complete fact set so late competing Claims cannot reopen the decision. |
| Recovery and compaction | Recover unresolved Claims using trusted terminal workflow-run provenance. Generate the existing maintenance system's coordinator compaction job, rewrite the same canonical file, and preserve current replay and future protocol semantics. The initial model only removes duplicate records and canonicalizes order. |

The [formal verification notes and reproducible checker](../../specs/dispatch-work-coordinator/README.md) document the invariants and assumptions. The current bounded checks cover 3,845,751 distinct states, including one-shot workers, source-version publication, and terminal freezing. They support a parameterized inductive proof argument, not a mechanically checked unbounded proof or proof that a future runtime implementation refines the model.

### Alternatives Considered

#### Alternative 1: An External Transactional Queue or Database

An external service could provide efficient queue operations and atomic ownership changes without full-log replay or Git branch contention. It was not chosen because it would require operators to provision storage, credentials, and recovery infrastructure outside the repository. Repository-local durability and integration with existing trusted workflow boundaries are the primary drivers of this proposal.

#### Alternative 2: A Mutable Current-State Snapshot

A single projected-state file with version-checked updates would reduce read and replay costs and could enforce ownership through careful update rules. It was not chosen because it replaces explicit transaction history with a separate mutable-state reconciliation protocol. The append-only fact model makes arbitration, recovery, and compaction share one replay implementation and provides an inspectable transaction history.

### Consequences

#### Positive

- Durable coordination and an auditable history remain in the repository without an additional service.
- Shared replay and trusted publication boundaries give dispatchers, workers, recovery, and maintenance one consistent authority model.
- The executable model and explicit invariants provide a baseline for implementation and regression testing.

#### Negative

- Full-log reads and replay add latency; competing writers contend on one branch and may exhaust bounded retries.
- Duplicate-only compaction does not bound unique transaction growth or Git history size. More aggressive retention requires another equivalence proof.
- A crash after Completion but before external effects can leave completed Work without outputs. Reliable effect delivery would require an additional idempotent delivery protocol.

#### Neutral

- Runtime implementation still needs schema, compiler, context, tool-server, safe-output, and maintenance integration; this decision does not imply those surfaces already exist.
- The guarantee is a single externally effective winner, not exactly-once agent execution or atomic external operation batches. Eventual progress also requires available workflows and successful retries.

---

*Draft decision record for [pull request #64955](https://github.com/github/gh-aw/pull/64955). Review before changing status to Accepted.*
