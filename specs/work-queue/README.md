---
title: Work Queue protocol model
description: TLA+ model, safety proof argument, and bounded verification for the work queue.
---

# Work Queue protocol model

This TLA+ model formalizes the proposal in [issue #64852](https://github.com/github/gh-aw/issues/64852): best-effort oldest-first selection, deferred dispatcher transactions, trusted worker finalization, optimistic branch writes, orphan recovery, and compaction.

The design rationale and trade-offs are recorded in [ADR-64955](../../docs/adr/64955-git-backed-work-queue-coordination.md).

## Queue inspection and operator commands

`gh aw work-queue` operates on a dedicated branch without using the current checkout. Supply
`--repo owner/repo`; use `--branch` to select a different
queue branch (default: `gh-aw-work-queue`). The experimental command uses authenticated GitHub Git APIs to
read and validate `work-queue.jsonl`, create trees and commits, and
publish changes with non-force reference updates. It needs neither a checkout nor
a Git executable, and accepts GitHub repositories rather than local Git remotes.
Rejected concurrent updates are retried against a fresh branch snapshot and replay.
In an initialized repository, an absent queue branch is initialized with a
parentless commit; other files in an existing queue branch are preserved.
GitHub Git APIs cannot create the first reference in an entirely empty repository.
Authentication uses the GitHub
CLI configuration or `GH_TOKEN`/`GITHUB_TOKEN`, with repository contents write
permission required for mutations.
All subcommands support `--json` for machine-readable output. Workflows enable the
read-only snapshot MCP server with `tools.work-queue: true`.

| Command | Arguments |
|---|---|
| `replay` | Display the projected Work and Claims |
| `stats` | Count Work, Claims, and distinct transactions |
| `compact` | Canonically order facts and remove only identical duplicates |
| `submit-work` | `--file work.json` (or `--file -` for stdin); derives an id from the canonical JSON object |
| `claim` | `--run-id RUN [--work-id ID]`; defaults to the oldest available Work |
| `finish` | `--claim-id ID --attempt-id ATTEMPT [--outcome TEXT]` |
| `cancel-work` | `--work-id ID` |
| `cancel-claim` | `--claim-id ID` |

These are **operator** commands; `finish` writes a Completion fact, but is not a
worker safe-output authorization mechanism and does not execute external effects.
The caller must independently establish the provenance of `--run-id` and
`--attempt-id`. Worker authorization and MCP/safe-output integration remain
separate implementation obligations described in the ADR.

The operator transaction wire format (`Transaction`) is defined in
[`transactions.tsp`](transactions.tsp).
The emitted JSON Schemas are embedded in `pkg/workqueue/schema/` and validate
each operator record before replay or publication. The workflow runtime uses a
separate `work-queue` branch and the versioned `WorkQueueTransaction` format:
required fields `version: 2`, `kind`, `work`, `claim`, and `attempt`, plus optional
`enqueued` on Work. Unused claim/attempt
fields are explicitly `null`; identities are nonempty strings. Its loader upgrades
unversioned/version-0/version-1 workflow records through successive codemods before
validation and replay. Version 1 remains the closed five-field format without
`enqueued`; historical records with extra fields are rejected before upgrading.
The 1-to-2 upgrade changes only the version, leaving historical Work at age zero
without inventing enqueue metadata. The snapshot envelope version is independent
of the transaction version. These formats
are not interchangeable; the CLI branch also preserves Work payloads and run
provenance, which the workflow fact format does not contain.

Trusted workflow assignments use `aw_context.work_queue`, containing `work_id`,
`claim_id`, and the `work` payload object (`WorkQueueAssignment`). Activation
checks that the Claim is currently effective before capturing the assignment.
When `tools.work-queue: true` and `safe-outputs.dispatch-workflow` are both
configured, a dispatcher can read an available identity with `work_queue_read`
and pass `work_queue: {work_id: "..."}` to an allowed same-repository worker's
dispatch tool. The worker must declare a `workflow_dispatch` `aw_context` input
and enable `tools.work-queue: true` so its activation admission and completion
reconciliation are compiled.
Trusted safe-output processing refreshes the queue, publishes a new Claim for
that identity, verifies it is effective, and injects the assignment with
`work: {id: work_id}`. The selection is not forwarded as a workflow input.
Dispatch errors attempt to cancel the Claim. An ordinary dispatch without a
selection and a staged preview create no Claim; `call-workflow` does not create
one either. Concurrent Claims may supersede an assignment before worker
activation, so admission and completion still recheck the durable queue.
The MCP tools are `work_queue_read` and `work_queue_claim_finish`; the latter
records a `WorkQueueFinishIntent` containing only `outcome: "completed"` or
`outcome: "cancelled"`. Artifacts use `work-queue.snapshot.json` and
`work-queue.finish.jsonl`; the durable transaction file is `work-queue.jsonl`.

When the runtime prompt advertises `work-queue` under `<mcp-clis>`, invoke these
tools as `work-queue work_queue_read '{"work":"example"}'` and
`work-queue work_queue_claim_finish '{"outcome":"completed"}'`. The tool names
are subcommands of the server's CLI wrapper, not standalone executables.
Copilot advertises this wrapper when CLI mounting is active; other engines
advertise it with `tools.cli-proxy: true`.
Both tools return JSON serialized in MCP text content. The finish-intent file
contains only the outcome and is readable by the runner artifact collector even
when the MCP container runs as a different user.

Pre-rename `aw_context.work_claim` assignments are schema-checked and normalized
to `work_queue`, not treated as unassigned. Existing runtime storage on
`dispatch-coordinator` / `dispatch-work-coordinator.jsonl` is read and updated in
place with the same checked publication protocol. It is not silently copied to
an independent queue. Explicit migration requires quiescing all writers and old
workflows, renaming the existing branch/log, and deploying recompiled workflows
before resuming. Both branch names or both log filenames together are ambiguous
and fail closed. Logs/audit retain read compatibility for historical artifacts.

To regenerate the schemas with TypeSpec 1.16.0,
install `@typespec/compiler` and `@typespec/json-schema` in a temporary directory,
compile `transactions.tsp` from that directory with emitter options
`file-type=json` and `seal-object-schemas=true`, and copy the emitted JSON files
into `pkg/workqueue/schema/`. The TLA+ model abstracts identities as integers;
the CLI uses stable string identities and canonical JSON Work payloads. Protocol
versions and payload data are outside the model's arbitration abstraction.

**Verification status:** the module includes parameterized safety theorem statements and the inductive proof argument below. TLC exhaustively checks the supplied finite configurations. The theorem statements are not mechanically checked by TLAPS; bounded model checking is not an unbounded proof.

## Concrete protocol choices

The model fixes arbitration and makes the worker lifecycle, publication guards, and terminal-state protection explicit:

| Boundary | Modeled rule |
|---|---|
| Arbitration | The least stable, uncancelled Claim identity wins on nonterminal Work. A persisted Completion fixes the winner; WorkCancellation removes authority. |
| Queue selection | Stage a Claim for the oldest available Work in the dispatcher's local replay. Use an immutable enqueue-time key with stable Work identity as the tie-breaker; skip claimed and terminal Work. |
| Terminal Work | Reject new state-changing transactions for completed/cancelled Work. Identical physical records may be duplicated without changing the fact set. |
| Dispatcher | The activation job reads the queue branch and packages its log and branch version into the activation artifact. The work-queue MCP server reads only that immutable snapshot and never accesses Git; the view may be stale while the agent runs. Mutations are revalidated and published only by trusted `safe_outputs`; pending competing Claims may become durable on nonterminal Work. |
| Worker | Each worker has one immutable inbound Claim and one pass through safe-output processing; it records at most one distinct Completion. Finalize carries no authority parameters. |
| Authorization | The winning worker verifies its newly committed Completion before outputs. Finished, stopped, and failed workers cannot restart or receive authorization again. |
| Compaction | Canonicalize order and remove identical duplicate records only. Preserve the entire fact set, including cancelled/superseded Claim history. More aggressive compaction needs a separate proof. |

These are protocol refinements, not claims that an implementation already enforces them. Claim ordering is by stable identity, not arrival time. Arrival order can change which transactions are accepted; it cannot change replay of the same accepted fact set.

`WorkOf`, `Inbound`, and `Origin` provide small, deterministic identity mappings for TLC. `Inbound` represents validated trusted context, not agent-selected input. Claims also identify their owning run; multiple worker attempts may share one Claim/run. Payloads are abstracted to stable Work identities, assuming collision-free canonical identity and idempotent submission.

### Best-effort queue ordering

Each Work fact carries immutable `enqueued` metadata, set once at submission and preserved through retries, replay, and compaction. The finite model uses the Work integer as the rank of the `(enqueue time, Work identity)` key, not as a physical log position or a globally allocated sequence number.

JavaScript and Go represent `enqueued` as Unix milliseconds, restricted to nonnegative integers no larger than `9007199254740991` so both languages compare exactly. Both replay projections expose an oldest-first `available` identity list, with UTF-8 Work identity as the tie-breaker. Historical Work without metadata has age zero and sorts before timestamped Work; replay never invents timestamps from record position or the current clock. Repeated submissions preserve the first durable Work fact's age, while conflicting metadata already present in the durable log is rejected.

JavaScript callers construct Work with `createWorkTransaction` and select or stage Claims with `oldestAvailableWork` / `claimOldestAvailableWork`, passing their local view including earlier pending intents. Go callers use `NewWork` and `OldestAvailable`. `gh aw work-queue claim --run-id RUN` selects once from its initial view and retains that Work identity across publication retries; `--work-id ID` remains an explicit operator override. `work_queue_read` lists available Work first in enqueue order by default, includes enqueue metadata, and returns the snapshot's recommended `next_work` identity or `null`. Its optional `work` selector reads a single identity, while optional `sort` reorders available Work and the queue-wide `next_work` recommendation. `sort` accepts one to four ordered keys (`id`, `enqueued`, or `id_length`), each with a direction (`asc` or `desc`): `{"sort":[{"key":"enqueued","direction":"desc"}]}` recommends newest available Work. Later keys break ties, then the default oldest-first order does. ID length counts Unicode code points. Non-available Work remains after available Work. Sorting is read-only: it does not change Claim selection, safe-output dispatch authority, or publication order, and the recommendation is not durable authority.

`OldestAvailable` chooses the least key among Work whose replayed state is `available`. `Stage` applies that preference to the local view, including earlier pending intents, so a batch cannot repeatedly select the same Work. Claimed Work does not block selection of newer available Work; cancellation of its last active Claim makes it eligible again with its original age. Work and claim arbitration remain separate.

Publication deliberately does **not** enforce oldest-first ordering. `Allowed` continues to accept competing Claims on nonterminal Work, and a version-conflict retry replays already selected intents without moving a Claim to another Work. An older submission can become visible after a newer item was selected; concurrent workers can also complete out of order. The model's local view is current durable facts plus pending intents at staging, but even that view may be outdated before publication. Implementations selecting from immutable activation snapshots have the same limitation.

This is a queue preference, not strict FIFO, a bounded-overtaking guarantee, or a claim that reordering has a particular probability. Reordering should be uncommon with fresh views and prompt publication, but TLA+ explores adversarial schedules and does not measure frequency. No global sequence allocator, head-of-line lock, or completion barrier is introduced.

## State and job boundaries

`log` is the only authoritative durable state. `head` abstracts an opaque, non-reused Git branch version. Branch creation is a version-checked write against the initial absent-branch token. All successful mutations change that version; no force-push or reuse of an old version is permitted.

The activation artifact is an immutable snapshot of the queue log, branch version, and validated worker assignment read during activation. The MCP server mounts that snapshot read-only and derives its query results with shared replay; it has no Git client or repository credentials. Its only writable mount is the safe-output intent directory, where `work_queue_claim_finish(outcome?)` records an outcome without accepting Work or Claim identity. The snapshot is only an early view and can be stale by the time the agent asks a question or submits work. Any future local pending-intent view remains non-authoritative. Every trusted writer records both the source log and its branch version; `CandidateDerivation` verifies that the candidate was generated from that latest source, not from the activation snapshot.

Workers progress through activation snapshot capture/admission, execution, finalize/no-finalize, preparation, push, verification, and effects. Admission reads the immutable activation snapshot and is not retained authority: `WorkerCandidate` checks current ownership against the latest log again. A stale candidate must be regenerated. Missing finalize cancels an effective Claim without permitting outputs; losing attempts stop without effects. In the implementation, safe-output processing downloads the activation and agent artifacts, reconciles against the latest Git-backed log, and gates user steps and handlers until a Completion for the trusted inbound Claim is verified. The finish outcome `cancelled` and an absent finish intent both map to `finalize = FALSE`; `completed` maps to `finalize = TRUE`.

Each worker's inbound Claim is fixed by `Inbound`. `SingleCompletionPerWorker` bounds its distinct Completion facts, and `WorkerOneShot` makes finished, stopped, and failed phases absorbing. The former reauthorization example added an impossible restart transition; it was not a reachable protocol failure and has been removed.

Compaction and recovery have independent prepared snapshots and bounded retries. Recovery cancels unresolved Claims whose owning runs terminated, including superseded Claims on nonterminal Work. Compaction discards stale snapshots and rebuilds from current facts.

`terminalHistory` records each Work item's complete fact set at its first terminal decision. It and `authorizations`/`effects` are observer histories, not additional queue files or decision-making state. Authorization/effect histories are sequences, so repeated execution of the same record cannot disappear through set deduplication. One `ExternalEffect` represents entry into one attempt's ordinary safe-output batch, not one GitHub API call.

`Apply` abstracts explicit rejection and idempotent no-op outcomes as no append. The model admits an identity-based Work resubmission with different enqueue metadata as idempotent and checks that staging it does not change replayed facts or the first durable Work age. Other exact duplicate intents are abstracted as no append; the model does not track implementation diagnostic counts. To keep the finite search bounded, each exact intent can be staged at most once per dispatcher.

JavaScript `applyTransactions` returns the candidate `transactions`, invalid intents in `rejected`, and an `idempotent` count. Exact duplicates of every kind and identity-based Work resubmissions are idempotent, not rejected, even when resubmitted Work carries a different enqueue time. Publication logs count requested intents as new, rejected, or idempotent on each attempt, independently of duplicate physical records in the source log. Retry outcomes describe only the refreshed attempt, not accumulated counts. All-no-op publication skips writes unless the log needs a protocol upgrade or canonicalization.

## Required protections

**Activation snapshot:** the activation job reads the queue branch and validates any trusted inbound assignment before uploading the activation artifact. The queue MCP process receives a read-only mount of the packed snapshot plus a writable safe-output intent directory; it does not receive a Git client or repository token. Snapshot queries are informative, not authority, and may be stale.

**Publication:** publish only if the branch still matches the version originally read. Otherwise fetch the latest log, replay it, and regenerate the proposed changes.

All dispatcher, worker, recovery, and compaction pushes use `Publish`. It requires both `snapshot.base = head` and `snapshot.source = log`. Their retry actions capture the latest version/log and rebuild the entire candidate through replay; none reuse stale output or splice it into newer state. Retry exhaustion fails without publication or external effects.

**Terminal Work:** reject new state-changing transactions after Work becomes completed or cancelled. Late competing Claims cannot reopen the decision.

`Allowed` applies the nonterminal guard to every new transaction type. `TerminalFreeze` requires a terminal Work item's current fact set to remain identical to its recorded terminal snapshot. Physical duplicate records and compaction may change representation, but cannot change those facts.

## Invariants

| Predicate | Guarantee |
|---|---|
| `TypeOK`, `ValidLog` | Valid identities/references; at most one terminal transaction per Work; Completion belongs to the trusted inbound Claim and the selected claimant. |
| `SnapshotValidity`, `SnapshotVersions`, `ActivationSnapshotValidity`, `CandidateDerivation` | A matching writer snapshot contains the current source log and an exactly regenerated candidate; activation admission reflects its immutable artifact snapshot; no snapshot is based on a future version. |
| `Serialization` | Every locally proposed dispatcher transaction is serialized identically for safe-output processing. |
| `TerminalPersistence` | Previously committed Completion/WorkCancellation facts remain durable. |
| `TerminalHistoryValid`, `TerminalFreeze` | All facts for a terminal Work item are frozen; neither late Claims nor other new transactions can change the decision. |
| `SingleCompletionPerWorker`, `WorkerOneShot` | One fixed inbound Claim and at most one distinct Completion per worker; terminal worker phases never restart. |
| `SingleEffectiveClaim` | At most one effective Claim per Work. |
| `QueueSelection` (action property) | Every staged Claim selects the oldest available Work in the local view at that step, not necessarily the oldest at publication or completion. |
| `WorkResubmissionNoOp` (action property) | A same-identity Work resubmission with different enqueue metadata may be staged, but does not change replayed facts or the original Work age. |
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

`OldestAvailable` also depends only on facts and immutable Work metadata. Equal fact sets therefore give the same next selection, and compaction preserves the ordering preference even when it rearranges physical records.

### Accepted-extension lemma

Assume `ValidFacts(f)`. One accepted transaction preserves it:

1. Work adds an identity without references.
2. Claim and ClaimCancellation require their existing references and nonterminal Work. No existing Completion can have its arbitration result changed.
3. WorkCancellation requires nonterminal Work, so it cannot coexist with a prior terminal decision.
4. Completion requires nonterminal Work, the current winner, and the trusted attempt/Claim binding. It becomes the sole terminal decision.

Duplicate records preserve the fact set. Induction over the serialized batch gives the same result for `Apply`.

### Initial state and snapshot preservation

The empty log, empty histories, zero HEAD/bases, and initial phases satisfy `Safety`.

The activation artifact contains the then-current branch version and full replayable transaction log. The finite model abstracts this to the version and admission result for the worker's inbound Claim. `Activate` derives admission from the captured log and stores the snapshot version only for admitted workers; no later action changes that field. Consequently `ActivationSnapshotValidity` and the bound on activation snapshot versions are inductive, while later execution can observe a stale version. Worker preparation derives its candidate from the latest log, never the artifact. Preparing or retrying a dispatcher/worker/recovery candidate applies the accepted-extension lemma to the current log. Preparing a compaction uses the canonicalization lemma. Both the source log and its version are captured, and `CandidateDerivation` records their exact relationship to the regenerated candidate. Recovery also captures the observed terminated runs.

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

Agent staging changes only equal local/serialized logs. Only `Stage` extends these sequences, and its Claim guard selects `OldestAvailable` from the preceding local view; every other action leaves them unchanged. Thus `QueueSelection` holds without imposing a publication-order invariant. Activation snapshot capture and admission do not grant authorization; execution-time worker preparation rechecks current Git facts. Retry exhaustion changes phase to `failed` without writing or emitting effects. Run termination can stop a worker but neither invents nor removes a terminal fact or authorization. Maintenance uses the same guarded write lemmas. Every action in `Next`, and stuttering, therefore preserves the strengthened `Safety` predicate.

By induction on execution length, `Spec => []Safety` follows under the modeled assumptions. This is a reviewable proof argument, not a TLAPS proof certificate.

## Reproduce verification

Use Java 21 and the official [`tla2tools.jar` v1.7.4](https://github.com/tlaplus/tlaplus/releases/tag/v1.7.4), which reports TLC 2.19. Jar SHA-256:

```text
936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88
```

```bash
TLA2TOOLS_JAR=/path/to/tla2tools.jar \
JAVA_BIN=/path/to/java \
bash specs/work-queue/check.sh
```

The runner checks all three positive configurations, the one-shot worker property, Work resubmission idempotency, and oldest-available selection to exhaustion. It requires the three negative controls to fail with the named invariant or action property, not a parse/tooling failure. These controls deliberately bypass branch-version, terminal-state, or selection protection; they are not reachable behaviors of the guarded protocol. A separate guarded witness demonstrates that strict FIFO is not required. Full reports are saved under a printed temporary path; set `TLC_RESULTS_DIR` to retain them at a chosen location.

| Configuration | Scope / expected result |
|---|---|
| `WorkQueue.cfg` | One Work, two competing Claims, two dispatchers, three workers (two share a Claim), three branch changes, four records; `Safety`, `WorkerOneShot`, `QueueSelection`, and `WorkResubmissionNoOp` hold. |
| `Recovery.cfg` | One Work, two Claims/workers, one dispatcher, four branch changes, five records; `Safety` holds through recovery/compaction interleavings. |
| `QueueOrdering.cfg` | Two Work items, two Claims/workers/dispatchers, two branch changes, four records; `Safety`, `WorkerOneShot`, and `QueueSelection` hold across selection, retry, cancellation, and compaction interleavings. |
| `BrokenCAS.cfg` | Bypass the branch-version check and overwrite with a stale snapshot; `TerminalPersistence` fails. |
| `BrokenTerminal.cfg` | Bypass terminal protection and append a competing Claim after Completion; `TerminalFreeze` fails. |
| `BrokenQueueSelection.cfg` | Bypass oldest-available selection while retaining publication safety; `QueueSelection` fails. |
| `WeakOrderingWitness.cfg` | Under guarded `Spec`, a newer Work is claimed before an older submission becomes visible; the deliberately false `NoOutOfOrderClaim` invariant fails. |

All three positive searches completed on 2026-10-02, checking `Safety`, `WorkerOneShot`, and `QueueSelection`: 7,218,153 distinct states at graph depth 33 for concurrency, 49,087 at depth 19 for recovery, and 1,696,812 at depth 24 for two-Work ordering. The branch-version, terminal-state, and queue-selection negative controls produced their expected violations at depths 7, 10, and 4. The guarded weak-ordering witness reached an out-of-order Claim at depth 6 without a safety or selection violation.

`Bound` constrains branch changes and physical log size, not execution depth. TLC also checks immediate successor states before pruning them. Deadlock checking is disabled because stopped/failed workflows are intentional; no fairness or liveness theorem is asserted.

## Inspect execution traces

Generate bounded textual traces of the guarded `Spec` and four reachable counterexamples to deliberately false *witness* invariants:

```bash
TLA2TOOLS_JAR=/path/to/tla2tools.jar \
TLC_TRACE_DEPTH=16 TLC_TRACE_COUNT=3 \
bash specs/work-queue/traces.sh
```

The script prints a temporary results directory (or uses `TLC_RESULTS_DIR` when set). `simulation_*` files are TLC's textual TLA+ state traces, with at most `TLC_TRACE_DEPTH` states each; `TLC_TRACE_COUNT` sets the number of seeded random simulations. These samples illustrate possible schedules, not exhaustive coverage or guaranteed occurrences of a particular action. The `*Witness.log` files contain model-checked textual counterexample states and action names. Each witness configuration also checks `Safety`; the script accepts only the named witness violation, not a safety violation or a TLC failure.

| Witness | What to inspect in its counterexample |
|---|---|
| `NoCompetingClaims` | Two dispatchers select the same Work in their local views before either Claim is durable. Both Claims persist, but replay still selects one effective winner. |
| `NoRecoveredOrphan` | A run terminates, then recovery prepares and publishes its ClaimCancellation. |
| `NoExternalEffect` | A worker finalizes, commits Completion, verifies it, and enters its output batch. |
| `NoOutOfOrderClaim` | A dispatcher selects newer Work from its local view before staging an older submission; publication preserves the Claim rather than imposing strict FIFO. |

These invariants are intentionally **not** protocol requirements: their violation demonstrates reachability of legitimate behavior under the existing guarded `Spec`. Unlike the three `Broken*.cfg` configurations, the witness configurations do not add unsafe actions. Inspect the preceding states, not just the final state, to verify the ordering claimed by the ADR. Witnesses show that the model permits these paths; they do not establish that a runtime implementation follows them or that progress is guaranteed.

## Runtime smoke coverage

The private `.github/workflows/smoke-work-queue.md` workflow
exercises the activation snapshot read tool and the trusted finish-intent tool
through the compiled MCP mount, verifies the finish intent in the downloaded
agent artifact, and runs it through safe-output reconciliation. It runs without
an inbound worker claim, so it must not append to the durable queue log.
This smoke test validates tool wiring and finish-intent transport; it does not
cover dispatcher transaction submission, recovery, or compaction.

## Limits

Safety does not imply eventual dispatch, successful external effects, or eventual orphan recovery. Those require fairness, available workflows, and successful retries. A crash after Completion but before outputs can leave completed Work with no output; recovering that gap requires an additional idempotent effect-delivery protocol. The activation artifact is a snapshot, not a live subscription; reads can be stale and are never used as final authority.

The model does not prove GitHub authentication, `aw_context` provenance validation, payload canonicalization, parser behavior, transport errors, or the implementation's refinement of these abstract actions. These remain implementation obligations. It does not make arbitrary external API batches atomic or exactly-once.
