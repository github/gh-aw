---
title: Dispatch Work Coordinator protocol model
description: TLA+ model, safety proof argument, and bounded verification for the dispatch-work coordinator.
---

# Dispatch Work Coordinator protocol model

This TLA+ model formalizes the proposal in [issue #64852](https://github.com/github/gh-aw/issues/64852): deferred dispatcher transactions, trusted worker finalization, optimistic branch writes, orphan recovery, and compaction.

**Verification status:** the module includes parameterized safety theorem statements and the inductive proof argument below. TLC exhaustively checks the supplied finite configurations. The theorem statements are not mechanically checked by TLAPS; bounded model checking is not an unbounded proof.

## Concrete protocol choices

The proposal leaves arbitration and duplicate-finalization handling underspecified. This model makes the following refinements explicit:

| Boundary | Modeled rule |
|---|---|
| Arbitration | The least stable, uncancelled Claim identity wins on nonterminal Work. A persisted Completion fixes the winner; WorkCancellation removes authority. |
| Terminal Work | Reject new state-changing transactions for completed/cancelled Work. Identical physical records may be duplicated without changing the fact set. |
| Dispatcher | Append each intent to both local and serialized logs. Rebase/revalidate the batch in `safe_outputs`; never push from the agent job. Pending competing Claims may become durable on nonterminal Work. |
| Worker | Finalize carries no authority parameters. Completion binds trusted inbound Claim identity to a unique safe-output attempt. |
| Authorization | Only the attempt that newly commits Completion may authorize outputs, after fresh verification. Observing an existing Completion is not a reusable permit. |
| Compaction | Canonicalize order and remove identical duplicate records only. Preserve the entire fact set, including cancelled/superseded Claim history. More aggressive compaction needs a separate proof. |

These are protocol refinements, not claims that an implementation already enforces them. Claim ordering is by stable identity, not arrival time. Arrival order can change which transactions are accepted; it cannot change replay of the same accepted fact set.

`WorkOf`, `Inbound`, and `Origin` provide small, deterministic identity mappings for TLC. `Inbound` represents validated trusted context, not agent-selected input. Claims also identify their owning run; multiple worker attempts may share one Claim/run. Payloads are abstracted to stable Work identities, assuming collision-free canonical identity and idempotent submission.

## State and job boundaries

`log` is the only authoritative durable state. `head` abstracts an opaque, non-reused Git HEAD token. Branch creation is a CAS write against the initial absent-branch token. All successful mutations change that token; no force-push or ABA is permitted.

`dispatch[d].local` and `.serialized` are append-only session logs. MCP reads evaluate current remote facts plus pending intents. `PrepareDispatch` and `RetryDispatch` regenerate a candidate from the current log; `PushDispatch` succeeds only against the captured HEAD.

Workers progress through admission, execution, finalize/no-finalize, preparation, push, verification, and effects. Admission is not retained authority: `WorkerCandidate` checks current ownership again. A stale candidate must be regenerated. Missing finalize cancels an effective Claim without permitting outputs; losing attempts stop without effects.

Compaction and recovery have independent prepared snapshots and bounded retries. Recovery cancels unresolved Claims whose owning runs terminated, including superseded Claims on nonterminal Work. Compaction discards stale snapshots and rebuilds from current facts.

`terminalHistory`, `authorizations`, and `effects` are observer histories, not additional coordinator files or decision-making state. Authorization/effect histories are sequences, so repeated execution of the same record cannot disappear through set deduplication. One `ExternalEffect` represents entry into one attempt's ordinary safe-output batch, not one GitHub API call.

`Apply` abstracts explicit rejection and idempotent no-op outcomes as no append. Implementations must report rejected intents; the abstraction does not prescribe silently treating errors as success.

## Invariants

| Predicate | Guarantee |
|---|---|
| `TypeOK`, `ValidLog` | Valid identities/references; at most one terminal transaction per Work; Completion belongs to the trusted inbound Claim and the selected claimant. |
| `SnapshotValidity`, `SnapshotVersions` | A candidate whose source HEAD still matches is a valid fact-set extension, or an equivalent compaction. No candidate is based on a future HEAD. |
| `Serialization` | Every locally proposed dispatcher transaction is serialized identically for safe-output processing. |
| `TerminalPersistence` | Previously committed Completion/WorkCancellation facts remain durable. |
| `SingleEffectiveClaim` | At most one effective Claim per Work. |
| `WorkerOrigin`, `FinishRequired` | Worker mutations concern only the inbound Claim; authorization requires finalize. |
| `AuthorizationSoundness`, `EffectSoundness` | Authorization follows durable Completion; effects follow authorization. |
| `LifecycleAccounting` | Each attempt authorizes/emits at most once, without resetting its lifecycle. |
| `SingleAuthorization`, `SingleEffect` | At most one safe-output attempt per Work receives authorization or enters its output batch. |

## Inductive proof argument

The argument is parameterized by the identity-domain sizes and retry limit. It does not use `MaxHead` or `MaxLog`, which appear only in TLC's exploration constraint.

### Replay and compaction lemmas

`Replay(s)` is defined as `Projection(Facts(s))`. Substituting equal fact sets gives `ReplayDeterminism` immediately; physical record order and duplicates have no influence.

For finite `f`, induction on its cardinality proves `Facts(Canonical(f)) = f`. The empty case returns the empty sequence. The nonempty case emits one chosen member and recursively emits exactly the remaining members. Thus `CompactionEquivalence` follows by the replay lemma.

For any intent list, induction on its length also shows that applying it to logs with equal fact sets produces equal resulting fact sets: `Allowed` depends only on facts, and append adds the same accepted fact in either case. Consequently the modeled compaction preserves future mutation decisions, not just current displayed state. It may change HEAD and provoke a retry, as any concurrent write can.

### Accepted-extension lemma

Assume `ValidFacts(f)`. One accepted transaction preserves it:

1. Work adds an identity without references.
2. Claim and ClaimCancellation require their existing references and nonterminal Work. No existing Completion can have its arbitration result changed.
3. WorkCancellation requires nonterminal Work, so it cannot coexist with a prior terminal decision.
4. Completion requires nonterminal Work, the current winner, and the trusted attempt/Claim binding. It becomes the sole terminal decision.

Duplicate records preserve the fact set. Induction over the serialized batch gives the same result for `Apply`.

### Initial state and snapshot preservation

The empty log, empty histories, zero HEAD/bases, and initial phases satisfy `Safety`.

Preparing or retrying a dispatcher/worker/recovery candidate applies the accepted-extension lemma to the current log. Preparing a compaction uses the canonicalization lemma. Source versions are captured from the current HEAD.

A local/read/lifecycle step does not change remote facts or HEAD. A successful write either leaves the log unchanged or increases HEAD. Existing snapshot versions cannot equal the new HEAD because `SnapshotVersions` bounds them by the old HEAD. Their matching-source implications become false. Newly prepared snapshots again satisfy those implications. Therefore snapshot validity is inductive.

### Durable safety

A push is guarded by equality with its recorded source HEAD. `SnapshotValidity` therefore supplies a valid extension of the actual current facts, not a stale projection. Compaction supplies exactly the current fact set. Neither removes a terminal fact; `Commit` adds newly observed terminal facts to `terminalHistory`. This preserves `ValidLog` and `TerminalPersistence`.

The winner is a single-valued function: a stored Completion's Claim, no Claim for cancelled Work, or the unique minimum active identity. Valid references and terminal exclusion prevent ambiguity. Hence `SingleEffectiveClaim`.

### Authorization and effects

`WorkerOrigin` restricts a fresh worker append to its own Completion or ClaimCancellation. A newly committed Completion requires finalize. A terminal/no-op candidate moves to `stopped`, not `committed`.

Only `VerifyCompletion` extends authorization history. It requires the attempt's `committed` phase and its exact durable Completion. The phase becomes `authorized`; it cannot return to `committed`. A second attempt for the same Claim cannot newly append Completion to terminal Work. Thus authorization records are durable, finalize-bound, and unique per attempt.

`ValidLog` allows only one Completion record per Work. Since an authorization contains that exact attempt-bound record and each attempt authorizes at most once, `SingleAuthorization` follows.

Only `ExternalEffect` extends effect history. It requires `authorized`, then moves irrevocably to `done`. The effect is backed by its authorization and is emitted at most once per attempt. Together with `SingleAuthorization`, this proves `SingleEffect`.

### Remaining actions

Agent staging changes only equal local/serialized logs. Admission and agent execution do not grant authorization. Retry exhaustion changes phase to `failed` without writing or emitting effects. Run termination can stop a worker but neither invents nor removes a terminal fact or authorization. Maintenance uses the same guarded write lemmas. Every action in `Next`, and stuttering, therefore preserves the strengthened `Safety` predicate.

By induction on execution length, `Spec => []Safety` follows under the modeled assumptions. This is a reviewable proof argument, not a TLAPS proof certificate.

## Reproduce verification

Use Java 21 and the official [`tla2tools.jar` v1.7.4](https://github.com/tlaplus/tlaplus/releases/tag/v1.7.4), which reports TLC 2.19. Jar SHA-256:

```text
936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88
```

```bash
TLA2TOOLS_JAR=/path/to/tla2tools.jar \
JAVA_BIN=/path/to/java \
bash specs/dispatch-work-coordinator/check.sh
```

The runner checks both positive configurations to exhaustion and requires the three negative controls to fail with the named invariant, not a parse/tooling failure. Full reports are saved under a printed temporary path; set `TLC_RESULTS_DIR` to retain them at a chosen location.

| Configuration | Scope / expected result |
|---|---|
| `DispatchWorkCoordinator.cfg` | One Work, two competing Claims, two dispatchers, three workers (two share a Claim), three branch changes, four records; `Safety` holds. |
| `Recovery.cfg` | One Work, two Claims/workers, one dispatcher, four branch changes, five records; `Safety` holds through recovery/compaction interleavings. |
| `BrokenAuthorization.cfg` | Reauthorize an already-completed Claim; `SingleAuthorization` fails. |
| `BrokenCAS.cfg` | Write a stale snapshot without CAS; `TerminalPersistence` fails. |
| `BrokenTerminal.cfg` | Append a better-ranked Claim after Completion; `ValidLog` fails. |

Verification on 2026-10-02 completed both positive searches: 3,577,277 distinct states at graph depth 33 for the concurrency configuration, and 188,446 distinct states at depth 23 for recovery. All three negative controls produced the expected invariant violations, at depths 12, 7, and 11 respectively.

`Bound` constrains branch changes and physical log size, not execution depth. TLC also checks immediate successor states before pruning them. Deadlock checking is disabled because stopped/failed workflows are intentional; no fairness or liveness theorem is asserted.

## Limits

Safety does not imply eventual dispatch, successful external effects, or eventual orphan recovery. Those require fairness, available workflows, and successful retries. A crash after Completion but before outputs can leave completed Work with no output; recovering that gap requires an additional idempotent effect-delivery protocol.

The model does not prove GitHub authentication, `aw_context` provenance validation, payload canonicalization, parser behavior, transport errors, or the implementation's refinement of these abstract actions. These remain implementation obligations. It does not make arbitrary external API batches atomic or exactly-once.
