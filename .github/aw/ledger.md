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
is rejected during compilation. Each standalone ledger has a `ledgers/<name>`
branch and a disposable read-only SQLite projection rebuilt from persisted
JSONL shards.

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

Each record is limited to 32 KiB, each shard defaults to 100 KiB, and each run
defaults to a 10 KiB append budget. Configure per-ledger limits with
`max-segment-kb`, `max-record-kb`, and `max-patch-kb`. Projection preparation
inspects at most 1024 canonical files and 100 MiB of history.
Each writing workflow invocation creates a shard, so a frequently running
workflow can exhaust the shard limit. Use ordinary repo-memory files for
replaceable snapshots, pruned baselines, or histories that need more than 1024
writer shards. Compaction belongs to Agentic Maintenance, never the agent or
persistence job. Use per-ledger `compaction` settings (`schedule`,
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

## Tradeoffs

| ✅ Pros | ❌ Cons |
|---|---|
| Persists indefinitely (no expiry) | Produces Git commits — repository noise |
| Auditable: Git history shows every change | Slower: requires Git clone + push |
| Survives cache invalidation | Not available for Copilot engine (requires GitHub tools) |
| Human-readable via GitHub branch UI | More complex setup |
| Can target a different repository | |
| Provides immutable structured records and queries | Append-only and limited to 1024 shard files by default |
