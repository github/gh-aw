# Extension to ADR-64955: Versioned Dispatch Coordinator Messages

**Date**: 2026-10-02
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

The [dispatch coordinator ledger](64955-git-backed-dispatch-work-coordination.md) is durable. Its messages can outlive the version of gh-aw that wrote them, so protocol changes need safe, portable upgrades without changing replay authority.

### Decision

Every ledger message has an integer version. Define declarative codemods for successive protocol versions and keep their files in the gh-aw repository under `actions/setup/js/`. On load, apply the codemods in version order to older messages, then compact and validate the resulting ledger before writing it back through the coordinator's version-checked publication path. Reject unknown versions or messages that cannot be upgraded or validated; do not publish a partial upgrade.

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

- This extends the coordinator's replay and version-checked publication commitments; it does not implement runtime migration.
