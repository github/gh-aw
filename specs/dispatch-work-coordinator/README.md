---
title: Work Queue protocol model
description: TLA+ model, safety proof argument, and bounded verification for the work queue.
---

# Work Queue protocol model

This TLA+ model formalizes the proposal in [issue #64852](https://github.com/github/gh-aw/issues/64852): deferred dispatcher transactions, trusted worker finalization, optimistic branch writes, orphan recovery, and compaction.

The design rationale and trade-offs are recorded in [ADR-64955](../../docs/adr/64955-git-backed-dispatch-work-coordination.md).

## Queue inspection and operator commands

`gh aw work-queue` operates on a dedicated branch without using the current checkout. Supply
`--repo owner/repo`; the default branch is `gh-aw-dispatch-work-coordinator`.
Use `--branch` to select a different
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
All subcommands support `--json` for machine-readable output. Workflows enable the
snapshot-backed MCP server with `tools.work-queue: true`. The MCP process has no
Git credentials; its writes are pending intents published by trusted safe outputs.

| Command | Arguments |
|---|---|
| `replay` | Display the projected Work and Claims |
| `stats` | Count Work, Claims, and distinct transactions |
| `compact` | Canonically order facts and remove only identical duplicates |
| `submit-work` | `--file work.json` (or `--file -` for stdin); derives an id from the canonical JSON object |
| `claim` | `--work-id ID --run-id RUN` |
| `claim-next` | `--run-id RUN [--selection policy.json]`; defaults to available Work in FIFO order |
| `finish` | `--claim-id ID --attempt-id ATTEMPT [--outcome TEXT]` |
| `cancel-work` | `--work-id ID` |
| `cancel-claim` | `--claim-id ID` |

These are **operator** commands; `finish` writes a Completion fact, but is not a
worker safe-output authorization mechanism and does not execute external effects.
The caller must independently establish the provenance of `--run-id` and
`--attempt-id`. Workflow MCP claims instead obtain run provenance from trusted
safe-output context. Their pending results are revalidated against the current
queue before publication and dispatch.

## FIFO and declarative selection

New Work receives a monotonically increasing `sequence` in the trusted submission
transaction, recalculated on a publication retry. FIFO uses that immutable
sequence, not physical log order, timestamps, or the payload hash; compaction
therefore preserves selection. Available work alone is eligible. Explicit
objectives precede FIFO, with work ID as the final deterministic tie-break.

The CLI's `--selection` accepts a JSON file (or `-` for stdin). MCP
`dispatch_claim_next` accepts the same policy as `selection`. Filters, ordered sort
objectives, and per-group concurrency limits operate on payload JSON Pointers;
see the [work-queue tool reference](../../docs/src/content/docs/reference/tools.md#work-queue-work-queue--experimental).
The [shared fixtures](selection-fixtures.json) exercise Go and JavaScript selection
semantics, including missing/null distinctions and claimed work outside filters.
Group policies apply to each selection call; explicit operator Claims can bypass
them, and dispatchers sharing a concurrency limit must use the same grouping policy.

CLI `ClaimNext` retries selection against the latest branch state. The MCP
equivalent stages a selected Work and Claim identity so a later dispatch can
reference that exact payload. Trusted publication reruns selection on every retry
and fails closed if that identity is no longer the next eligible Work; it never
substitutes a different payload after the agent has inspected the pending result.
Session-local pending claims count toward group occupancy without granting
durable authority. All pending claims in a dispatcher batch must validate before
any are published. Dispatch inputs use the reserved `work_queue_claim_id` key,
which the trusted handler replaces with a verified `aw_context` assignment.

## Unified ledger and historical upgrades

Both runtimes write version 3 records using `work_id`, `claim_id`, `run_id`, and
`attempt_id` as appropriate; Work carries its JSON object and FIFO `sequence`.
Claims use the same lexicographic arbitration and terminal guards as before.
The original runtime's unversioned/version-1 `work`/`claim`/`attempt` records and
the CLI's unversioned payload-aware records upgrade before replay. The ordered
version-2-to-3 codemod changes only the version field, retaining identities,
payloads, sequences, provenance, and terminal outcomes. The activation artifact
envelope remains version 2; its version is independent of ledger message versions.
Missing
sequences use first-seen Work order in the historical log and are persisted before
compaction. Submission order already erased by historical compaction cannot be
recovered. Legacy runtime records had no payload or provenance: retain their
opaque identities, use `{"legacy_work_id": ID}` as the payload, and mark run
provenance as `legacy:CLAIM_ID`; this is not trusted workflow-run evidence.

When the default branch is absent, readers recognize the historical runtime branch
`dispatch-coordinator`. Activation is read-only. A trusted mutation or upgrade
publishes the unified branch with the historical head as parent, retaining the
old branch. **Stop older writers before upgrading**; afterwards all writers must
use the unified default branch (or the same explicit CLI `--branch`). Automatic
orphan recovery is not implemented; operators can cancel an unresolved Claim if
publication succeeded but its downstream dispatch did not.

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
| Dispatcher | The activation job reads the coordinator branch and packages its log and branch version into the activation artifact. The work-queue MCP server reads only that immutable snapshot and never accesses Git; the view may be stale while the agent runs. Mutations are revalidated and published only by trusted `safe_outputs`; pending competing Claims may become durable on nonterminal Work. |
| Worker | Each worker has one immutable inbound Claim and one pass through safe-output processing; it records at most one distinct Completion. Finalize carries no authority parameters. |
| Authorization | The winning worker verifies its newly committed Completion before outputs. Finished, stopped, and failed workers cannot restart or receive authorization again. |
| Compaction | Canonicalize order and remove identical duplicate records only. Preserve the entire fact set, including cancelled/superseded Claim history. More aggressive compaction needs a separate proof. |

Claim arbitration remains by stable identity, not arrival time. Queue selection
is separate: available Work is ordered by its durable FIFO sequence unless a
policy supplies sort objectives, with Work identity as the last tie-break.
Arrival order can change which transactions are accepted; it cannot change replay
or selection over the same accepted fact set.

### Selection and version abstractions

Version 3 facts contain the same identity, payload, sequence, and provenance
fields as the runtime ledger. Fields absent on a wire message use `0` in the
uniform model record. Work identifiers remain integers; fixed payloads supply
priority, cost, and group fields. Filters are represented by accepted Work sets,
and policies cover FIFO, descending priority, compound objectives, filtered
grouping, and group caps of one or two. JSON Pointer parsing, mixed scalar types,
and arbitrary filter operators are covered by the shared Go/JavaScript fixtures,
not claimed as TLA+ parser verification.

`WorkIntent` has no assigned enqueue sequence. `Materialize` assigns the next
sequence against the current fact set, including on a publication retry.
`CaptureDispatcherSnapshot` stores an immutable activation source;
`StageNext` selects against that source plus session-local pending intents.
Explicit operator Claims may compete with existing Claims without a queue policy.
Trusted `Reconcile` reruns each queued selection against the latest source and
the preceding intents in the batch. A stale selection rejects the whole batch,
and retries never substitute another Work identity. Existing effective Claims
remain idempotent without authorizing a new Claim.

`UpgradeMessage` models the declarative version-2-to-3 header change.
`VersionUpgradeEquivalence` checks fact preservation and whole-batch rejection
of an unsupported message; historical layout transformations remain covered by
runtime codemod tests.

`WorkOf`, `Inbound`, and `Origin` provide small, deterministic identity mappings for TLC. `Inbound` represents validated trusted context, not agent-selected input. Claims also identify their owning run; multiple worker attempts may share one Claim/run. Payloads are abstracted to stable Work identities, assuming collision-free canonical identity and idempotent submission.

## State and job boundaries

`log` is the only authoritative durable state. `head` abstracts an opaque, non-reused Git branch version. Branch creation is a version-checked write against the initial absent-branch token. All successful mutations change that version; no force-push or reuse of an old version is permitted.

The activation artifact is an immutable snapshot of the coordinator log, branch version, and validated worker assignment read during activation. The MCP server mounts that snapshot read-only and derives its query results with shared replay; it has no Git client or repository credentials. Its only writable mount is the safe-output intent directory, where `dispatch_claim_finish(outcome?)` records an outcome without accepting Work or Claim identity. The snapshot is only an early view and can be stale by the time the agent asks a question or submits work. Any future local pending-intent view remains non-authoritative. Every trusted writer records both the source log and its branch version; `CandidateDerivation` verifies that the candidate was generated from that latest source, not from the activation snapshot.

Workers progress through activation snapshot capture/admission, execution, finalize/no-finalize, preparation, push, verification, and effects. Admission reads the immutable activation snapshot and is not retained authority: `WorkerCandidate` checks current ownership against the latest log again. A stale candidate must be regenerated. Missing finalize cancels an effective Claim without permitting outputs; losing attempts stop without effects. In the implementation, safe-output processing downloads the activation and agent artifacts, reconciles against the latest Git-backed log, and gates user steps and handlers until a Completion for the trusted inbound Claim is verified. The finish outcome `cancelled` and an absent finish intent both map to `finalize = FALSE`; `completed` maps to `finalize = TRUE`.

Each worker's inbound Claim is fixed by `Inbound`. `SingleCompletionPerWorker` bounds its distinct Completion facts, and `WorkerOneShot` makes finished, stopped, and failed phases absorbing. The former reauthorization example added an impossible restart transition; it was not a reachable protocol failure and has been removed.

Compaction and recovery have independent prepared snapshots and bounded retries. Recovery cancels unresolved Claims whose owning runs terminated, including superseded Claims on nonterminal Work. Compaction discards stale snapshots and rebuilds from current facts.

`terminalHistory` records each Work item's complete fact set at its first terminal decision. It and `authorizations`/`effects` are observer histories, not additional coordinator files or decision-making state. Authorization/effect histories are sequences, so repeated execution of the same record cannot disappear through set deduplication. One `ExternalEffect` represents entry into one attempt's ordinary safe-output batch, not one GitHub API call.

`Apply` abstracts explicit rejection and idempotent no-op outcomes as no append. Implementations must report rejected intents; the abstraction does not prescribe silently treating errors as success.

## Required protections

**Activation snapshot:** the activation job reads the coordinator branch and validates any trusted inbound assignment before uploading the activation artifact. The coordinator MCP process receives a read-only mount of the packed snapshot plus a writable safe-output intent directory; it does not receive a Git client or repository token. Snapshot queries are informative, not authority, and may be stale.

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
| `ValidLog`, `FIFOSelection`, `SelectionCompaction` | Trusted submission assigns distinct contiguous FIFO ranks; available Work selection and future ranks survive compaction and record reordering. |
| `QueueStagingSoundness`, `QueueSelectionSoundness`, `GroupSelection` | Pending choices use immutable activation views; prepared queue batches remain valid against their source, including unfiltered group occupancy. |
| `CurrentProtocol`, `VersionUpgradeEquivalence` | Durable facts use version 3; upgrading version-2 headers preserves facts and rejects unsupported mixed batches atomically. |
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

For Work, the next FIFO rank is one greater than the maximum durable rank.
Uniqueness and contiguous ranking therefore extend with each accepted submission;
Claims and terminal facts cannot change ranks. Since policy eligibility, group
occupancy, objective comparisons, and FIFO ties depend only on facts, compaction
preserves `NextWork` as well as replay.

For a queued Claim, reconciliation checks that the selected Work is still the
next eligible Work in the refreshed source and the preceding accepted intents.
Invalid reconciliation returns the original source for the entire batch and
moves the dispatcher to `failed`; a prepared queue candidate always has valid
reconciliation. Ordinary operator Claims retain the existing arbitration rules
and do not establish a global grouping guarantee.

The version-2 codemod alters only `version`, so all identity, payload, rank,
provenance, and terminal fields are retained. An unsupported record rejects the
whole upgrade before a candidate ledger exists. These lemmas are reflected in
the executable invariants; the parameterized theorem statements are not TLAPS proofs.

### Initial state and snapshot preservation

The empty log, empty histories, zero HEAD/bases, and initial phases satisfy `Safety`.

The activation artifact contains the then-current branch version and full replayable transaction log. The finite model stores the dispatcher's captured source and abstracts worker activation to the version and admission result for its inbound Claim. `Activate` derives admission from the captured log and stores the snapshot version only for admitted workers; no later action changes that field. Consequently `ActivationSnapshotValidity` and the bound on activation snapshot versions are inductive, while later execution can observe a stale version. Worker preparation derives its candidate from the latest log, never the artifact. Preparing or retrying a dispatcher/worker/recovery candidate applies the accepted-extension and queue-reconciliation lemmas to the current log. Preparing a compaction uses the canonicalization lemma. Both the source log and its version are captured, and `CandidateDerivation` records their exact relationship to the regenerated candidate. Recovery also captures the observed terminated runs.

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

Agent staging changes only equal local/serialized logs. Activation snapshot capture and admission do not grant authorization; execution-time worker preparation rechecks current Git facts. Retry exhaustion changes phase to `failed` without writing or emitting effects. Run termination can stop a worker but neither invents nor removes a terminal fact or authorization. Maintenance uses the same guarded write lemmas. Every action in `Next`, and stuttering, therefore preserves the strengthened `Safety` predicate.

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

The runner checks four positive configurations and the one-shot worker property
to exhaustion, and requires four negative controls to fail with the named
invariant, not a parse/tooling failure. These controls bypass branch-version,
terminal-state, FIFO-selection, or latest-source reconciliation guards; they are
not reachable behaviors of the guarded protocol. Full reports are saved under a
printed temporary path; set `TLC_RESULTS_DIR` to retain them at a chosen location.

| Configuration | Scope / expected result |
|---|---|
| `DispatchWorkCoordinator.cfg` | One Work, two competing Claims, two dispatchers, three workers (two share a Claim), three branch changes, four records; `Safety` holds. |
| `Recovery.cfg` | One Work, two Claims/workers, one dispatcher, four branch changes, five records; `Safety` holds through recovery/compaction interleavings. |
| `Submission.cfg` | Two Work, two dispatchers, two branch changes, three records; submission ranks are assigned and regenerated independently of Work identity. |
| `Selection.cfg` | Dispatcher-only `WorkQueueSelection.tla` refinement: three Work in every initial enqueue permutation, three Claims, two dispatchers, three branch changes, six records; FIFO, objectives, filtered group caps, snapshots, and stale reconciliation are checked. |
| `BrokenCAS.cfg` | Bypass the branch-version check and overwrite with a stale snapshot; `TerminalPersistence` fails. |
| `BrokenTerminal.cfg` | Bypass terminal protection and append a competing Claim after Completion; `TerminalFreeze` fails. |
| `BrokenSelection.cfg` | Stage an available Work other than the FIFO choice; `QueueStagingSoundness` fails. |
| `BrokenQueue.cfg` | Prepare a stale queued choice without reconciliation; `QueueSelectionSoundness` fails. |

The version-3 profiles completed exhaustive checking on 2026-10-02 with TLC 2.19:

| Positive profile | Distinct states | Complete graph depth |
|---|---:|---:|
| `DispatchWorkCoordinator` | 9,466,659 | 33 |
| `Recovery` | 441,032 | 23 |
| `Submission` | 66,530 | 20 |
| `Selection` | 23,843 | 11 |

All four checked `Safety` and `WorkerOneShot` without violations. The four negative
controls produced their expected invariant violations at depths 7, 10, 3, and 7,
respectively. Existing competing-Claim, orphan-recovery, and external-effect
reachability witnesses also produced the expected counterexamples. These results
apply to the documented finite profiles, not an unbounded proof or a mechanically
verified runtime refinement.

`Bound` constrains branch changes, physical log size, and local batch size, not
execution depth. `QueueEnabled` isolates selection actions from the baseline
submission/worker profiles. `SeedQueue` initializes every enqueue permutation
for the selection profile and limits its primitive operator surface to Claim
overrides; submissions and Work cancellations remain covered by the unseeded
profiles. The dispatcher-only selection refinement omits worker, recovery,
compaction, and duplicate-record transitions, which remain covered by the
baseline profiles and compaction invariants. The combined runtime interactions
are not claimed to be exhaustively covered by this isolated profile.
These profiles avoid redundant state expansion while retaining their checked
invariants. TLC also checks immediate successor states
before pruning them. Deadlock checking is disabled because stopped/failed
workflows are intentional; no fairness or liveness theorem is asserted.

To run selected profiles without rerunning an already completed search, pass
their configuration names to the same checker, for example
`bash specs/dispatch-work-coordinator/check.sh Selection BrokenSelection BrokenQueue`.
The default invocation runs all profiles.

## Inspect execution traces

Generate bounded textual traces of the guarded `Spec` and three reachable counterexamples to deliberately false *witness* invariants:

```bash
TLA2TOOLS_JAR=/path/to/tla2tools.jar \
TLC_TRACE_DEPTH=16 TLC_TRACE_COUNT=3 \
bash specs/dispatch-work-coordinator/traces.sh
```

The script prints a temporary results directory (or uses `TLC_RESULTS_DIR` when set). `simulation_*` files are TLC's textual TLA+ state traces, with at most `TLC_TRACE_DEPTH` states each; `TLC_TRACE_COUNT` sets the number of seeded random simulations. These samples illustrate possible schedules, not exhaustive coverage or guaranteed occurrences of a particular action. The `*Witness.log` files contain model-checked textual counterexample states and action names. Each witness configuration also checks `Safety`; the script accepts only the named witness violation, not a safety violation or a TLC failure.

| Witness | What to inspect in its counterexample |
|---|---|
| `NoCompetingClaims` | Two Claims for one nonterminal Work are persisted; replay still selects one effective winner. |
| `NoRecoveredOrphan` | A run terminates, then recovery prepares and publishes its ClaimCancellation. |
| `NoExternalEffect` | A worker finalizes, commits Completion, verifies it, and enters its output batch. |

These invariants are intentionally **not** protocol requirements: their violation demonstrates reachability of legitimate behavior under the existing guarded `Spec`. Unlike `BrokenCAS.cfg` and `BrokenTerminal.cfg`, the witness configurations do not add unsafe actions. Inspect the preceding states, not just the final state, to verify the ordering claimed by the ADR. Witnesses show that the model permits these paths; they do not establish that a runtime implementation follows them or that progress is guaranteed.

## Runtime smoke coverage

The private `.github/workflows/smoke-dispatch-work-coordinator.md` workflow
exercises the activation snapshot read tool and the trusted finish-intent tool
through the compiled MCP mount, verifies the finish intent in the downloaded
agent artifact, and runs it through safe-output reconciliation. It runs without
an inbound worker claim, so it must not append to the durable coordinator log.
This smoke test validates tool wiring and finish-intent transport; it does not
cover dispatcher transaction submission, recovery, or compaction.

## Limits

Safety does not imply eventual dispatch, successful external effects, or eventual orphan recovery. Those require fairness, available workflows, and successful retries. A crash after Completion but before outputs can leave completed Work with no output; recovering that gap requires an additional idempotent effect-delivery protocol. The activation artifact is a snapshot, not a live subscription; reads can be stale and are never used as final authority.

The model does not prove GitHub authentication, `aw_context` provenance validation, payload canonicalization, parser behavior, transport errors, or the implementation's refinement of these abstract actions. These remain implementation obligations. It does not make arbitrary external API batches atomic or exactly-once.
