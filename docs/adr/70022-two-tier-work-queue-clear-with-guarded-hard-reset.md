# ADR-70022: Two-Tier Work-Queue Clear with a Guarded Hard Reset

> [!NOTE]
> Superseded by work queue protocol v4 ([specification](../src/content/docs/specs/work-queue-specification.md)): v4 removed `clear --hard`, pause controls, and delivery barriers; `clear` releases open dispatches and cancels available Work.

**Date**: 2026-10-18
**Status**: Proposed (implemented in PR #70022; maintainer acceptance pending)
**Deciders**: No deciders were recorded in the generated draft. This record does not assert maintainer approval.

---

### Context

The Git-backed work queue (`pkg/workqueue`) is an append-only ledger recording Work, Claims, delivery barriers, admission identities, fairness charges, and native reservations. Existing `cancel-work` and `control` commands support targeted cancellation and pausing, but not a single queue-wide clear.

Cancelling unfinished Work must preserve the audit trail and reconciliation evidence for effects still in flight, including pending delivery from completed Claims that retained their Work. A fresh ledger instead forgets task and request identities, allowing old submissions and already-settled effects to repeat. These are distinct administrative operations, not interchangeable forms of compaction. Concurrent admissions and lost publication acknowledgments also require retries to recover the original accepted operation without sweeping later Work or applying stale targets to a replacement queue.

### Decision

PR #70022 implements `gh aw work-queue clear --reason CODE` with two modes:

1. **History-preserving clear (default).** One atomic `cancel_work` request terminally cancels cancellable `available` and `claimed` Work selected from the snapshot, including dependency-blocked and retry-delayed Work. Available Work with pending delivery from a completed `keep_open` Claim is excluded, preserving that delivery while unrelated backlog is cancelled. Existing `WorkCancellation` rules fence claimed items on their selected Claims. Completed Work, unresolved deliveries, historical Claims, admission identities, provenance, fairness charges, and native reservations remain intact. The request must fit the installed Policy's operation limit (at most 256 targeted Work); otherwise nothing is cancelled. Concurrent completion, a changed selected Claim, or queue-incarnation replacement between selection and publication fails the whole action, including on publication retries. Reused Work IDs in a replacement queue do not validate the original selection. Later admissions are not added to the target set. Admission and grant controls are unchanged; operators pause them separately when draining requires a stable backlog. A snapshot with no cancellable Work, including one containing only pending retained deliveries, is a no-op that records no request ID.
2. **Guarded hard reset (`--hard`).** `Branch.HardClear` replaces the active ledger with fresh `Genesis`, control requests, and availability-preserving deployment requests in one Git publication. It requires an existing, valid ledger with admission and grants already paused, every native reservation released, no open Claims or claimed Work, no pending or failed delivery barriers (including retained historical Claims), and no uncertain Work effects. Available Work may be discarded once these obligations are settled; the backlog need not first be cancelled. Current Policy, current worker routes and availability, and credential generation carry over. Admission and grants remain paused until explicitly resumed. Work/Claim/request identities, retry history, observations, historical worker revisions, and fairness debt are forgotten. The fast-forward-only publication uses the checked snapshot as its Git parent and an exact ref lease, retaining the prior ledger at the returned `prior_git_sha`. A conflicting publication fails rather than resetting a newer snapshot.

Both modes require authenticated repository **maintain** or **admin** permission and a sanitized reason code: 1–128 ASCII characters, starting with a letter or digit and containing only letters, digits, `_`, `.`, `:`, or `-`. Permission is rechecked before publication; the existing queue `administrator` actor records the authenticated principal, and unrelated administrator operations retain their stricter access requirements. New mutations display a payload-free queue preview on stderr and require interactive confirmation with no bypass. Declining, prompt failure, or non-TTY stdin refuses the mutation. Hard clear is bound to the previewed Git revision and refuses changes made while confirmation is open. Read-only previews, no-ops, and recovery do not prompt.

`--dry-run` authenticates and validates without publishing or reserving a request ID; it does not lock the snapshot. Both modes, including previews, refuse CI before authentication or queue access when any of `CI`, `CONTINUOUS_INTEGRATION`, `GITHUB_ACTIONS`, or `COPILOT_AGENT_SESSION_ID` is nonempty. There is no CI bypass flag.

A stable `--request-id`, reused by the same authorized operator with the same reason, recovers only the accepted cancellation or reset, including after checkpointing or later admissions. Incompatible reuse fails. Hard-reset recovery records the request-ID hash and prior Git SHA in the policy epoch and verifies the reset's control-request fingerprints; it does not perform another reset after a lost acknowledgment. This recovery depends on retaining that reset's evidence: a subsequent hard reset intentionally forgets earlier request identities.

Neither mode stops native runs or force-releases reservations. Cancellation is not proof of settlement; definitive native evidence and separate reconciliation remain necessary. Hard reset is not a repair path for a missing or malformed ledger and cannot bypass unresolved production obligations.

### Alternatives Considered

The following alternatives describe the tradeoffs assessed in this record, not prior maintainer decisions.

#### Use compaction/checkpointing for hard clear

Compaction reduces ledger size while preserving identities and fairness state. Extending it to discard Work would blur the distinction between semantics-preserving maintenance and destructive reset. A separate fresh-Genesis path makes identity loss explicit while preserving Git ancestry.

#### Offer cancel-all only

Atomic cancellation alone avoids identity loss but cannot provide a clean identity/accounting state after, for example, test runs. An explicit hard mode supports that distinct need, with strict settlement preconditions and warnings, without making destructive behavior the default.

#### Automatically batch large cancellations

Multiple requests would avoid the atomic operation ceiling, but partial failure would leave a partially cleared queue and complicate concurrency and retry semantics. `clear` instead fails as a whole; operators can pause admission/grants and use explicit IDs with `cancel-work` in bounded batches when atomicity is not required.

### Consequences

#### Positive
- Default clear cancels cancellable unfinished Work atomically while preserving pending retained deliveries, deduplication, and reconciliation evidence.
- Hard reset retains current configuration and Git ancestry while refusing unresolved native or delivery obligations.
- Stable request recovery prevents retries from sweeping later admissions, and queue-incarnation fencing rejects stale selections after replacement; CI detection prevents accidental invocation in recognized CI environments.

#### Negative
- Hard reset permits identity reuse and repeated settled effects; warnings, maintainer/admin authorization, and confirmation do not eliminate that risk. Retaining ancestry also means this is not data erasure.
- Large backlogs require explicit, non-atomic cancellation batches. Native-run cleanup remains a separate operational responsibility.
- Hard-reset recovery depends on the `hard_clear_<hash>_<sha>` policy-epoch encoding and fixed control-request indices; future Genesis, control, and checkpoint changes must preserve compatibility.

#### Neutral
- Existing `cancel-work`, `control`, and semantics-preserving `compact` remain available; no workflow activation or native-run cancellation is added.
- Implementation is in `pkg/cli/work_command_clear.go` and `pkg/workqueue/hard_clear.go`, with regression coverage for atomicity, authorization, CI refusal, settlement, concurrency, ancestry retention, and recovery.

---

Implementation: [PR #70022](https://github.com/github/gh-aw/pull/70022). Operational reference: [Clear unfinished Work](../src/content/docs/reference/work-queue.md#clear-unfinished-work).

Provenance: generated by the adr-writer agent for the [Design Decision Gate](https://github.com/github/gh-aw/pull/70022#issuecomment-6255775474) in [commit ffae694310](https://github.com/github/gh-aw/commit/ffae694310a7900bbc3f719d42e5fc02d9b26ba2), then completed against the implementation. Maintainer acceptance remains pending.
