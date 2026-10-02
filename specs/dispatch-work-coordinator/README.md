---
title: Dispatch Work Coordinator protocol model
description: TLA+ model, safety proof argument, and bounded verification for the dispatch-work coordinator.
---

# Dispatch Work Coordinator protocol model

This TLA+ model formalizes the proposal in [issue #64852](https://github.com/github/gh-aw/issues/64852): deferred dispatcher transactions, trusted worker finalization, optimistic branch writes, orphan recovery, and compaction.

The design rationale and trade-offs are recorded in [ADR-64955](../../docs/adr/64955-git-backed-dispatch-work-coordination.md).

## Queue inspection and operator commands

`gh aw work` operates on a dedicated branch without using the current checkout. Supply
`--repo owner/repo`; use `--branch` to select a different
coordinator branch. The experimental command uses authenticated GitHub Git APIs to
read and validate `dispatch-work-coordinator.jsonl`, create trees and commits, and
publish changes with non-force reference updates. It needs neither a checkout nor
a Git executable, and accepts GitHub repositories rather than local Git remotes.
Rejected concurrent updates are retried against a fresh branch snapshot and replay.
In an initialized repository, an absent coordinator branch is initialized with a
parentless commit; other files in an existing coordinator branch are preserved.
GitHub Git APIs cannot create the first reference in an entirely empty repository.
Authentication uses the GitHub
CLI configuration or `GH_TOKEN`/`GITHUB_TOKEN`, with repository contents write
permission required for mutations.
All subcommands support `--json` for machine-readable output.

| Command | Arguments |
|---|---|
| `replay` | Display the projected Work and Claims |
| `stats` | Count Work, Claims, and distinct transactions |
| `compact` | Canonically order facts and remove only identical duplicates |
| `submit-work` | `--file work.json` (or `--file -` for stdin); derives an id from the canonical JSON object |
| `claim` | `--work-id ID --run-id RUN` |
| `finish` | `--claim-id ID --attempt-id ATTEMPT [--outcome TEXT]` |
| `cancel-work` | `--work-id ID` |
| `cancel-claim` | `--claim-id ID` |

These are **operator** commands; `finish` writes a Completion fact, but is not a
worker safe-output authorization mechanism and does not execute external effects.
The caller must independently establish the provenance of `--run-id` and
`--attempt-id`. Worker authorization and MCP/safe-output integration remain
separate implementation obligations described in the ADR.

The transaction wire format is defined in [`transactions.tsp`](transactions.tsp).
The emitted JSON Schemas are embedded in `pkg/workqueue/schema/` and validate
each record before replay or publication. To regenerate them with TypeSpec 1.16.0,
install `@typespec/compiler` and `@typespec/json-schema` in a temporary directory,
compile `transactions.tsp` from that directory with emitter options
`file-type=json` and `seal-object-schemas=true`, and copy the emitted JSON files
into `pkg/workqueue/schema/`. The TLA+ model abstracts identities as integers;
the CLI uses stable string identities and canonical JSON Work payloads.

**Verification status:** the module includes parameterized safety theorem statements and the inductive proof argument below. TLC exhaustively checks the supplied finite configurations. The theorem statements are not mechanically checked by TLAPS; bounded model checking is not an unbounded proof.

## Concrete protocol choices

The model fixes arbitration and makes the worker lifecycle, publication guards, and terminal-state protection explicit:

| Boundary | Modeled rule |
|---|---|
| Arbitration | The least stable, uncancelled Claim identity wins on nonterminal Work. A persisted Completion fixes the winner; WorkCancellation removes authority. |
| Terminal Work | Reject new state-changing transactions for completed/cancelled Work. Identical physical records may be duplicated without changing the fact set. |
| Dispatcher | Append each intent to both local and serialized logs. Rebase/revalidate the batch in `safe_outputs`; never push from the agent job. Pending competing Claims may become durable on nonterminal Work. |
| Worker | Each worker has one immutable inbound Claim and one pass through safe-output processing; it records at most one distinct Completion. Finalize carries no authority parameters. |
| Authorization | The winning worker verifies its newly committed Completion before outputs. Finished, stopped, and failed workers cannot restart or receive authorization again. |
| Compaction | Canonicalize order and remove identical duplicate records only. Preserve the entire fact set, including cancelled/superseded Claim history. More aggressive compaction needs a separate proof. |

These are protocol refinements, not claims that an implementation already enforces them. Claim ordering is by stable identity, not arrival time. Arrival order can change which transactions are accepted; it cannot change replay of the same accepted fact set.

`WorkOf`, `Inbound`, and `Origin` provide small, deterministic identity mappings for TLC. `Inbound` represents validated trusted context, not agent-selected input. Claims also identify their owning run; multiple worker attempts may share one Claim/run. Payloads are abstracted to stable Work identities, assuming collision-free canonical identity and idempotent submission.

## State and job boundaries

`log` is the only authoritative durable state. `head` abstracts an opaque, non-reused Git branch version. Branch creation is a version-checked write against the initial absent-branch token. All successful mutations change that version; no force-push or reuse of an old version is permitted.

`dispatch[d].local` and `.serialized` are append-only session logs. MCP reads evaluate current remote facts plus pending intents. Every writer records both the source log and its branch version; `CandidateDerivation` verifies that the candidate was generated from that source, not a different cached projection.

Workers progress through admission, execution, finalize/no-finalize, preparation, push, verification, and effects. Admission is not retained authority: `WorkerCandidate` checks current ownership again. A stale candidate must be regenerated. Missing finalize cancels an effective Claim without permitting outputs; losing attempts stop without effects.

Each worker's inbound Claim is fixed by `Inbound`. `SingleCompletionPerWorker` bounds its distinct Completion facts, and `WorkerOneShot` makes finished, stopped, and failed phases absorbing. The former reauthorization example added an impossible restart transition; it was not a reachable protocol failure and has been removed.

Compaction and recovery have independent prepared snapshots and bounded retries. Recovery cancels unresolved Claims whose owning runs terminated, including superseded Claims on nonterminal Work. Compaction discards stale snapshots and rebuilds from current facts.

`terminalHistory` records each Work item's complete fact set at its first terminal decision. It and `authorizations`/`effects` are observer histories, not additional coordinator files or decision-making state. Authorization/effect histories are sequences, so repeated execution of the same record cannot disappear through set deduplication. One `ExternalEffect` represents entry into one attempt's ordinary safe-output batch, not one GitHub API call.

`Apply` abstracts explicit rejection and idempotent no-op outcomes as no append. Implementations must report rejected intents; the abstraction does not prescribe silently treating errors as success.

## Required protections

**Publication:** publish only if the branch still matches the version originally read. Otherwise fetch the latest log, replay it, and regenerate the proposed changes.

All dispatcher, worker, recovery, and compaction pushes use `Publish`. It requires both `snapshot.base = head` and `snapshot.source = log`. Their retry actions capture the latest version/log and rebuild the entire candidate through replay; none reuse stale output or splice it into newer state. Retry exhaustion fails without publication or external effects.

**Terminal Work:** reject new state-changing transactions after Work becomes completed or cancelled. Late competing Claims cannot reopen the decision.

`Allowed` applies the nonterminal guard to every new transaction type. `TerminalFreeze` requires a terminal Work item's current fact set to remain identical to its recorded terminal snapshot. Physical duplicate records and compaction may change representation, but cannot change those facts.

## Invariants

| Predicate | Guarantee |
|---|---|
| `TypeOK`, `ValidLog` | Valid identities/references; at most one terminal transaction per Work; Completion belongs to the trusted inbound Claim and the selected claimant. |
| `SnapshotValidity`, `SnapshotVersions`, `CandidateDerivation` | A matching-version snapshot contains the current source log and an exactly regenerated candidate; no snapshot is based on a future version. |
| `Serialization` | Every locally proposed dispatcher transaction is serialized identically for safe-output processing. |
| `TerminalPersistence` | Previously committed Completion/WorkCancellation facts remain durable. |
| `TerminalHistoryValid`, `TerminalFreeze` | All facts for a terminal Work item are frozen; neither late Claims nor other new transactions can change the decision. |
| `SingleCompletionPerWorker`, `WorkerOneShot` | One fixed inbound Claim and at most one distinct Completion per worker; terminal worker phases never restart. |
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

Preparing or retrying a dispatcher/worker/recovery candidate applies the accepted-extension lemma to the current log. Preparing a compaction uses the canonicalization lemma. Both the source log and its version are captured, and `CandidateDerivation` records their exact relationship to the regenerated candidate. Recovery also captures the observed terminated runs.

A local/read/lifecycle step does not change remote facts or HEAD. A successful write either leaves the log unchanged or increases HEAD. Existing snapshot versions cannot equal the new HEAD because `SnapshotVersions` bounds them by the old HEAD. Their matching-source implications become false. Newly prepared snapshots again satisfy those implications. Therefore snapshot validity is inductive.

### Durable safety

A push requires equality with both its recorded source version and source log. `SnapshotValidity` and `CandidateDerivation` therefore supply a valid extension generated from actual current facts, not a stale projection. Compaction supplies exactly the current fact set. Neither removes a terminal fact, preserving `ValidLog` and `TerminalPersistence`.

When Work first becomes terminal, `Commit` records all its facts in `terminalHistory`. Any later `Apply` rejects new facts for that Work through the universal nonterminal guard; worker preparation also stops on terminal Work. Duplicate records and compaction preserve fact sets. The stored terminal snapshot therefore remains equal to current Work facts, proving `TerminalFreeze`. This applies to higher-ranked and lower-ranked late Claims alike.

The winner is a single-valued function: a stored Completion's Claim, no Claim for cancelled Work, or the unique minimum active identity. Valid references and terminal exclusion prevent ambiguity. Hence `SingleEffectiveClaim`.

### Authorization and effects

`WorkerOrigin` restricts a fresh worker append to its own Completion or ClaimCancellation. A newly committed Completion requires finalize. A terminal/no-op candidate moves to `stopped`, not `committed`.

`Inbound` is immutable, and `Completion(a)` is one fixed fact for worker `a`, so `SingleCompletionPerWorker` follows. Workers cannot return to preparation after committing; finished, stopped, and failed phases have no outgoing restart transition. Inspection of every action and stuttering proves `WorkerOneShot`. Reauthorization is therefore unreachable, not an additional failure scenario.

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

The runner checks both positive configurations and the one-shot worker property to exhaustion, and requires the two remaining negative controls to fail with the named invariant, not a parse/tooling failure. These controls deliberately bypass branch-version or terminal-state protection; they are not reachable behaviors of the guarded protocol. Full reports are saved under a printed temporary path; set `TLC_RESULTS_DIR` to retain them at a chosen location.

| Configuration | Scope / expected result |
|---|---|
| `DispatchWorkCoordinator.cfg` | One Work, two competing Claims, two dispatchers, three workers (two share a Claim), three branch changes, four records; `Safety` holds. |
| `Recovery.cfg` | One Work, two Claims/workers, one dispatcher, four branch changes, five records; `Safety` holds through recovery/compaction interleavings. |
| `BrokenCAS.cfg` | Bypass the branch-version check and overwrite with a stale snapshot; `TerminalPersistence` fails. |
| `BrokenTerminal.cfg` | Bypass terminal protection and append a competing Claim after Completion; `TerminalFreeze` fails. |

Verification on 2026-10-02 completed both positive searches, checking `Safety` and `WorkerOneShot`: 3,626,825 distinct states at graph depth 33 for concurrency, and 218,926 distinct states at depth 23 for recovery. The two remaining negative controls produced the expected violations at depths 7 and 10. These results replace the earlier model's counts.

`Bound` constrains branch changes and physical log size, not execution depth. TLC also checks immediate successor states before pruning them. Deadlock checking is disabled because stopped/failed workflows are intentional; no fairness or liveness theorem is asserted.

## Limits

Safety does not imply eventual dispatch, successful external effects, or eventual orphan recovery. Those require fairness, available workflows, and successful retries. A crash after Completion but before outputs can leave completed Work with no output; recovering that gap requires an additional idempotent effect-delivery protocol.

The model does not prove GitHub authentication, `aw_context` provenance validation, payload canonicalization, parser behavior, transport errors, or the implementation's refinement of these abstract actions. These remain implementation obligations. It does not make arbitrary external API batches atomic or exactly-once.
