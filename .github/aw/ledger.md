---
description: tools.ledger — structured, immutable, append-only event history with built-in state-model projections.
---

# Ledger

Use the ledger for immutable, structured events when each run should add records
and later runs need to query them. For example, a low-volume audit can append
one result per scan and query prior results by a stable application-level key.
Configure new workflows with `tools.ledger`, independently of repo-memory.
Legacy `ledger` configuration is still accepted in a single-entry
`tools.repo-memory` array; the object-form `tools.repo-memory.ledger` declaration
is rejected during compilation. Git is the default storage backend: each
Git-backed ledger stores JSONL shards on a `ledgers/<name>` branch. Ledgers
using `storage: comment-memory` instead persist JSONL in managed comments
selected by `tools.comment-memory`. Both backends provide a disposable,
read-only SQLite projection.

```yaml
tools:
  ledger:
    audit-history:
      type: log
      compaction:
        min-segments: 32
        max-segments: 32
```

Declare `type: log`, `set`, `map`, `table`, `counter`, or `notes` for built-in state
models. Schemas validate operation values, not envelopes; tables require a
string primary-key field through `key`. Maps expose `ledger_map_put` and
`ledger_map_delete`; notes expose `ledger_note_add` and `ledger_note_vote`.
Other types use typed `ledger_append` operations.

Query `state` for current state or `records` for immutable history at
`/tmp/gh-aw/ledgers/<name>/ledger.db`. For a log, submit `ledger_append` with
`operation: append` and the event in `value`. Requests are deferred and become
durable only when the trusted `push_ledger_changes` job succeeds; queued writes
are not immediately visible in the run's projection. Never edit SQLite or
canonical ledger files.

## Choose a storage backend

Git storage is the default and is a better fit for larger or long-lived
histories. It creates a `ledgers/<name>` branch, requires Git access for trusted
persistence, and supports compaction by Agentic Maintenance. Projection
preparation inspects at most 1024 canonical files and 100 MiB of history, so a
frequently writing workflow can exhaust the file limit without compaction.

Comment storage keeps history with an issue or pull request and avoids ledger
branches. It requires `tools.comment-memory` and has smaller comment-specific
limits: 16 KiB per persisted batch, 48 KiB total per ledger, and a 50-page
comment scan limit. Comment-backed ledgers do not support compaction; use Git
storage when these limits are too small. Both backends also honor the ledger's
shared limits: 32 KiB per record, 100 KiB per shard, and a 10 KiB append budget
per run by default. Configure these with `max-record-kb`, `max-segment-kb`, and
`max-patch-kb`.

Choose comment storage when the issue or PR is the natural home for a small
history; choose Git storage for larger histories, compaction, or branch-based
auditing. Changing `storage` does not move or import existing history. Treat a
backend change as a separate migration, or use a new ledger name; do not expect
the new backend's projection to contain the old backend's records.

### Example: comment-backed ledger

```yaml
tools:
  comment-memory:
    target: 42
  ledger:
    audit-history:
      storage: comment-memory
      type: log
```

The target may be `triggering`, `*`, or a literal positive issue/PR number; a
configured `target-repo` must be literal. Ledger writes are queued during the
agent run and become durable only after the trusted persistence job succeeds.
The agent still queries the same read-only SQLite projection; for comment
storage, the managed JSONL comments—not SQLite—are durable history.

Comment publishers must be trusted, and persistence is serialized per target
within a workflow repository. Account for the workflow queue and cancellation,
comment size and scan limits, and publisher identity requirements described in
the [comment-backed ledger specification](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/specs/repo-memory-ledger-specification.md#comment-backed-storage).
Writers in different workflow repositories that share a target must coordinate
externally.

Compaction belongs to Agentic Maintenance, never the agent or persistence job.
For Git-backed ledgers, use per-ledger `compaction` settings (`schedule`,
`min-segments`, `max-segments`) or disable it with `compaction: false`.
Custom `replay.script`, `replay.config`, and `compaction.script` settings are
rejected. Ordinary repo-memory `validation.script` remains a separate supported
file-storage validator.

Removing custom replay does not rewrite history. Existing raw-record ledgers
can omit `type` and query `records.payload` with SQLite JSON functions. Do not
add a built-in type to raw history unless every payload already matches that
type's operation format. Use a new ledger name for a new typed state model.
For a ledger without `type`, submit `ledger_append` with the custom payload in
`record`, for example `{"ledger":"reviews","record":{"run_id":"42","outcome":"noop"}}`.
Do not use the built-in `operation: append` / `value` envelope for custom ledgers.
The agent-facing tool rejects invalid custom record shapes and configured-schema
mismatches before queuing; trusted finalization still validates the complete batch.
See the [ledger replay reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/ledger-replay.md)
and [compaction guide](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/ledger-compaction.md).

## Git storage tradeoffs

| ✅ Pros | ❌ Cons |
|---|---|
| Persists indefinitely (no expiry) | Produces Git commits — repository noise |
| Auditable: Git history shows every change | Slower: requires Git clone + push |
| Survives cache invalidation | Requires Git write access for persistence |
| Human-readable via GitHub branch UI | Projection inspects at most 1024 canonical files |
| Can target a different repository | |
| Provides immutable structured records and queries | Append-only; requires compaction to bound shard count |

## Comment storage tradeoffs

| ✅ Pros | ❌ Cons |
|---|---|
| Keeps history with the issue or PR | Limited to 16 KiB per batch and 48 KiB per ledger |
| Avoids a ledger Git branch | Requires a trusted comment publisher and configured target |
| Uses the same query tools and SQLite projection | 50-page comment scan limit; no compaction |
