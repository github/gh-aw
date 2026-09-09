# ADR-59453: Add oldest-run pruning to logs downloads

**Date**: 2026-09-08
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw logs` command already supports `--max-storage` to prune non-essential cache data and stop new downloads when the output directory cannot be reduced below a configured storage budget. This PR adds an opt-in mode for cases where cache-only pruning is insufficient, while preserving concurrent download behavior and resumable continuation data across CLI, stdin, multi-target, and MCP entry points. The implementation evidence shows a need to reclaim space from completed runs without deleting active downloads or newer runs, and to avoid partial deletions when older candidates cannot free enough space. The repository needs an explicit decision on whether storage enforcement may remove previously downloaded runs as part of log collection.

### Decision

We will add an opt-in `--prune-older-runs` mode to `gh aw logs` that allows the storage limiter to delete completed older run directories after non-essential cache pruning cannot satisfy `--max-storage`. The pruning algorithm will operate oldest-first by run ID, will only consider completed runs older than the run currently being processed, and will preserve active or newer runs. We will propagate this mode through CLI parsing, stdin and multi-target flows, MCP arguments, and continuation payloads so resumed downloads preserve the same storage-management policy.

### Alternatives Considered

#### Alternative 1: Keep the current behavior of pruning cache data only and then stopping downloads

The project could continue treating `--max-storage` as a hard stop once non-essential cache files have been removed. This was considered because it is simpler and avoids deleting previously downloaded runs. It was not chosen because the PR explicitly introduces a user-controlled mode for cases where cache pruning alone cannot keep downloads within budget.

#### Alternative 2: Always prune older runs whenever `--max-storage` is set

Another option would be to make older-run deletion automatic instead of opt-in. This was considered because it would maximize the chance of staying under the configured storage cap. It was not chosen because the diff adds a dedicated boolean flag and tests for default `false`, indicating the repository wants preservation of existing runs to remain the default behavior.

#### Alternative 3: Prune runs by filesystem modification time or allow partial deletion of any candidate run

The implementation could have selected deletion candidates using filesystem timestamps or freed space incrementally from whatever run data is available. This was considered because timestamps are easy to query and partial deletion may free space sooner. It was not chosen because the tests and pruning logic explicitly prefer run-ID ordering, preserve newer runs, and avoid pruning when eligible older runs cannot free enough space in full.

### Consequences

#### Positive
- Users can continue downloading logs under a storage budget in repositories with large historical runs when cache pruning alone is insufficient.
- The feature is explicit and reversible because destructive run pruning only happens when `--prune-older-runs` is set.
- Continuation, stdin, multi-target, and MCP paths all preserve the same storage-limiting semantics, reducing inconsistent behavior across entry points.

#### Negative
- Log collection can now delete previously downloaded completed runs, which may surprise users who expect the local cache to be preserved.
- The storage limiter becomes more complex because it must track completed runs, candidate ordering, and concurrency-safe pruning behavior.
- Additional tests and maintenance are required to preserve correctness around concurrent downloads and resumable runs.

#### Neutral
- Run retention is now part of the `gh aw logs` resource-budget policy alongside API-rate and storage limits.
- Continuation payloads and MCP schemas gain a new `prune_older_runs` field to carry this mode across boundaries.
- The implementation depends on `run-<id>` directory naming to infer pruning order for completed run directories.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
