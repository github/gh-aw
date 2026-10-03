# Extension to ADR-64955: Versioned Work Queue Messages

**Date**: 2026-10-02
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

The [work queue ledger](64955-git-backed-work-queue-coordination.md) is durable. Its messages can outlive the version of gh-aw that wrote them, so protocol changes need safe, portable upgrades without changing replay authority.

### Decision

Every workflow ledger message has an integer version. Define declarative codemods for successive protocol versions in `actions/setup/js/work_queue_codemods.cjs`. On load, check older messages against their closed historical field sets, apply the codemods in version order, then compact and validate the resulting ledger before writing it back through the queue's version-checked publication path. Reject unknown versions or messages that cannot be upgraded or validated; do not publish a partial upgrade.

The original unversioned messages (and explicit version 0 messages) upgrade to version 1, which retains the existing transaction fields and adds `version: 1`. Version 1 remains exactly `version`, `kind`, `work`, `claim`, and `attempt`; it does not permit `enqueued` or other extra fields.

Version 2 adds optional immutable `enqueued` metadata on Work only: Unix milliseconds in the integer range `0..9007199254740991`. The deterministic 1-to-2 codemod changes only `version` to 2. Historical Work keeps absent enqueue metadata and age zero; migration never derives timestamps from the clock or log position. New intents and serialized logs use version 2. Trusted write-capable readers publish the canonical upgraded log using the same fast-forward-only, retrying path as other queue writes. Read-only activation loads upgrade and validate in memory for their immutable snapshots, deferring publication until a trusted write-capable reader accesses the log.

The canonical workflow record is the `WorkQueueTransaction` union in
[`transactions.tsp`](../../specs/work-queue/transactions.tsp), with required
`version: 2`, `kind`, `work`, `claim`, and `attempt`, plus optional `enqueued` on
Work. The activation snapshot envelope's separate version remains 2; it is not
the transaction protocol version. This decision covers the workflow
ledger on `work-queue`, not the separate operator `Transaction` format on
`gh-aw-work-queue`.

### Alternatives Considered

#### Imperative migrations

Handwritten migration scripts offer more flexibility but make transformations harder to inspect and reproduce across readers. Declarative, ordered codemods keep the upgrade path explicit.

#### Ledger-wide replacement on each protocol change

Rewriting every ledger immediately would require coordinated deployment and risk leaving older readers unable to interpret the data. Upgrading on load allows existing durable ledgers to transition when accessed.

### Consequences

#### Positive

- Older messages have a defined path to the current protocol without replacing the ledger as a separate operation.
- Compaction and validation precede publication, preserving a single checked replay input.

#### Negative

- Codemods must remain available and deterministic for every supported historical version.
- Loading an older ledger costs additional work, and a failed upgrade blocks publication until the incompatibility is resolved.

#### Neutral

- This extends the queue's replay and version-checked publication commitments without changing the authority model.
