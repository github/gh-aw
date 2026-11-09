# ADR-76180: Add bounded obsolete Work cancellation

**Date**: 2026-11-09
**Status**: Draft
**Deciders**: Pending maintainer review

## Context

An AW-managed queue can contain available Work admitted under a logical worker
contract that no longer matches its approved active deployment. Maintenance
already retires this backlog, but operators need an explicit bounded command
without dispatching workers or changing routes. [PR #76180](https://github.com/github/gh-aw/pull/76180)
adds that command using the checked cancellation semantics landed in
[PR #76126](https://github.com/github/gh-aw/pull/76126). The native implementation
must remain pure Go, preserve historical receipts and queue accounting, and
reject cancellation when ownership or deployment state changes before publication.

## Decision

Implement `gh aw work cancel-work --obsolete` in Go, sharing the queue's
`obsoleteWork` predicate through
[`ObsoleteWorkIDs`](../../pkg/workqueue/deployment.go), rather than invoking a
JavaScript selector or publisher. Select only available, unclaimed, unpinned Work
with barrier `none` whose admission contract differs from an available approved
active deployment. Exclude elapsed Work using the later of selection time and
the ledger time watermark; missing or unavailable deployments are not evidence
of obsolescence. Sort IDs in UTF-8 byte order and take one prefix bounded by
`--limit` (1..256, default 256) and the installed Policy operation limit.

Retain the existing `cancel-work` command name without adding a `cancel` alias,
as explicitly requested. The new behavior is an option on the established
operator surface rather than another command spelling.

Publish an atomic `cancel_work` request containing `WorkCancellation` operations
with fixed reason `worker_contract_obsolete` and operation `version: 1`; the
ledger remains version 3. Reuse
[`workPublishOperator`](../../pkg/cli/work_command_operations.go), which
authenticates repository administrator authority. This operator authority is
distinct from the JavaScript maintenance publisher's authenticated `reconciler`
role; matching selection semantics does not grant the CLI maintenance authority.
The existing publication path rereads the current ledger prefix and validates
the checked operations at committed time. A concurrent Claim, execution pin,
delivery barrier, expiration, or compatible/unavailable deployment rejects the
entire batch rather than cancelling a stale selection.

Keep `--dry-run` a read-only ledger selection preview. It does not authenticate
administrator publication authority, reserve a request ID, publish a receipt,
synchronize Issues, or establish that a later publication will succeed. An
empty selection returns `no_work` without recording a request.

Use stable request identity for recovery: an accepted request restores its
original operation bytes, targets and fingerprint, never a newly selected batch.
Reject a retry limit too small for the original receipt and incompatible request
reuse. Preserve accepted legacy reason-only cancellations without retrofitting
version 1 checks or rewriting terminal metadata. After publication, synchronize
existing Issue mirrors separately; a synchronization failure reports the
committed cancellation and directs recovery through `sync-issues --request-id`.

## Alternatives Considered

These options describe tradeoffs evaluated for this record, not a claim that
they were historically proposed or approved.

### Invoke the JavaScript maintenance implementation

This would reuse the existing selector directly, but adds a JavaScript runtime
dependency to the native operator command and conflates administrator and
reconciler publication paths. Pure-Go selection with the shared replay predicate
keeps native execution independent while retaining the maintenance semantics.

### Leave retirement to maintenance or exact-ID cancellation

Existing mechanisms avoid a new selector and remain available. However,
maintenance does not provide this explicit operator preview, and manually
enumerating exact IDs leaves bounded ordering and eligibility discovery to the
operator. The new command provides those guarantees without replacing either
mechanism.

### Automatically reselect on conflict or retry

Reselecting could make progress despite concurrent changes, but would change an
operator's intended targets and could sweep newly obsolete Work under a reused
request ID. Fixed checked operations and stable receipts favor safety over
automatic progress; another batch requires a new request ID.

## Consequences

### Positive

- Operators obtain deterministic bounded retirement and a non-mutating preview
  without JavaScript shell-outs or worker dispatch.
- Current-prefix checks and immutable retry receipts protect concurrent
  ownership and preserve historical cancellation compatibility.

### Negative

- Read-only preview cannot prove publication permissions or eliminate later
  races; operators must handle rejected batches explicitly.
- Go and JavaScript selection semantics must remain aligned, and Issue mirror
  recovery remains separate from successful ledger publication.

### Neutral

- Cancellation retains Claims, Results, delivery barriers, native reservations,
  Policy/epoch/limits, fairness debt, history and unselected Work. It does not
  stop native runs, release capacity without definitive evidence, or modify
  deployment routes.
- Existing selection and CLI tests cover bounds, protected Work, read-only
  preview, authority, publication races and receipt recovery. This record does
  not claim live validation: the reported live Git-ledger dry-run stalled and
  was stopped with its ref unchanged, so it establishes no successful live
  command or publication.
