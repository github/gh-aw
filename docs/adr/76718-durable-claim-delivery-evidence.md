# ADR-76718: Retain durable Claim delivery evidence

**Date**: 2026-11-11
**Status**: Draft
**Deciders**: Pending maintainer review

## Context

The reported November 3–10 ledger contained 13 Work items whose delivery ended
in `DeliveryFailure(verification_exhausted, effects unknown)` even though their
PRs, discussions or issues existed. Handler and delivery receipts lived in
process-local WeakMaps. Once the worker's `safe_outputs` process exited,
scheduled maintenance had no verifier inputs and could only expire the pending
barrier. Completion and successful native execution do not establish delivery,
and an existing resource alone does not establish that it matches the intended
effect.

[PR #76718](https://github.com/github/gh-aw/pull/76718) adds a durable receipt
transport while retaining independent native readback and the protected
in-process verification path. The delivery contract requires recovery after a
worker crash without re-executing effects, accepting agent assertions as proof,
or rewriting historical terminal decisions.

## Decision

Publish one authenticated `DeliveryEvidence` operation from the original bound
worker attempt after its handler pass and same-Claim Completion, before the
process exits or its native reservation is released. Bind the operation to the
Work, Claim, Dispatch, local handle, Completion and original run attempt 1.
Identical republication is idempotent; different evidence for the same
Completion is rejected. Failed, unsupported or oversized passes do not publish
partial inventories.

Limit canonical operation JSON to 32 KiB, with at most 128 ordered output
receipts and 256 recorded effects. Bound adapter configurations and adapter
fields or receipts to 8 KiB. Preserve native identifiers, immutable revisions
and content digests rather than resource bodies wherever possible. Declared
REST and GraphQL field values remain bounded readback expectations. Derive
queue-control request IDs from authenticated worker authority, not raw Actions
context. Validate controls against a fresh checked same-Claim ledger inventory.
These are protocol constants, not Policy limits, so no Policy epoch changes.
Configured adapter objects are persisted in evidence by design only after
closed-key validation and the 8 KiB bound. Any future adapter field that could
carry credentials or personal data must be reviewed before it is permitted into
durable evidence.

Keep the WeakMap verification path for the original process. Refactor native
creations, builtins, assets, code scanning, coverage, artifacts and configured
adapters to share receipt-explicit native readback. A serialized receipt is an
input to verification, never an attestation. Artifacts, copied private proofs,
agent claims and public resource metadata cannot substitute for an authenticated
ledger operation plus fresh readback.

Maintenance reads pending evidence from the checked ledger and authenticates as
a reconciler. Its read-only authorizer retains the original frozen assignment,
resource scope and worker run binding, including after native release. Run the
existing protected delivery recheck with a closed effect channel, independently
read resources and queue controls, and publish `Result` through the established
finalizer only after a new protected attestation. No handler or mutation is
replayed. Explicit caller-supplied verifiers retain precedence over the
maintenance verifier factory.

When maintenance first observes a terminal worker after the delivery deadline,
allow one final independent readback capped at 15 seconds before expiry. This
avoids a zero-attempt failure when durable evidence exists without reopening the
normal retry budget. Changed, missing or unverifiable resources still exhaust
verification and produce `DeliveryFailure`; neither an active native run nor
unavailable terminal-run evidence authorizes expiry.

Define the operation and receipt inventories in `specs/work-queue/transactions.tsp`
and generate the JSON schemas used by both JavaScript and Go. Implement matching
validation, replay and checkpoint semantics. Checkpoints retain evidence only
for completed Claims with pending
delivery barriers. Omit the new snapshot key when empty, preserving historical
checkpoint digests. `Result` or `DeliveryFailure` removes pending evidence;
terminal decisions remain immutable. TLA models are not changed by this PR.

Receipt selection and serialization live in `work_queue_claim_scope.cjs`, below
the verifier modules. `work_queue_delivery_evidence.cjs` owns publication and
the shared verifier registry; `work_queue_delivery_recovery.cjs` assembles
maintenance readback. Worker and reconciler authorization share scope loading
and target checks while retaining their separate state policies. The existing
Go checkpoint construction and restoration sequences remain intact, with
evidence retention and validation added in place.

## Alternatives Considered

These alternatives describe design tradeoffs, not a claim that they were
historically proposed or approved.

### Persist separate journal files or artifacts

A separate journal avoids growing the ledger but introduces another transport,
authentication boundary and retention policy. Artifacts cannot establish the
same-Claim provenance of a copied private proof. The checked ledger already
provides authenticated publication, idempotency, CAS and checkpoint retention,
so the evidence operation reuses that boundary.

### Replace the in-process path with durable verification everywhere

A unified path would reduce the number of entry points, but would make immediate
verification depend on successful evidence publication and require a broader
cutover. Keeping the protected WeakMap path preserves immediate delivery while
receipt-explicit readback shares native verification between both paths.

### Accept resource existence or replay failed handlers

Existence does not prove expected content, repository authority or pinned Git
changes. Replaying a handler can duplicate externally visible effects. Neither
option satisfies the delivery contract; recovery performs fresh reads only.

## Consequences

Pending delivery can survive process loss without relaxing resource scope or
repeating writes. Native identity, content and Git revision mutations remain
verification failures. Restart tests exercise actual handler execution,
evidence publication, cleared runtime modules and maintenance settlement;
JS-produced pending, checkpoint and terminal ledgers also replay in Go.

Evidence adds bounded ledger storage and native readback cost. A crash between
an external write and evidence publication remains unrecoverable through this
mechanism. Unsupported or oversized receipts retain the existing in-process
path but cannot acquire durable recovery by truncation. Bounded adapter
expectations can contain field values and are not a general-purpose log channel.

No existing Results or DeliveryFailures are rewritten, and no workflow was
dispatched for this change. Live post-merge confirmation is a newly published
`DeliveryEvidence` operation followed by a pending Claim reaching `Result` in
maintenance with `maintenance.reconcile.complete ... durable_verified=1`.
