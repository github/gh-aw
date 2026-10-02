---
title: Standalone ledger replay projections
description: Build disposable read-only SQLite materialized views from immutable ledger history
---

Standalone `tools.ledger` projections support only the built-in types below.
gh-aw validates each operation and replays it in trusted code. Custom
`replay.script` and `replay.config` settings are not supported.

| Need | Type |
| --- | --- |
| Ordered history | `log` |
| Unique membership | `set` |
| Latest value by key | `map` |
| Mutable structured rows | `table` |
| Numeric accumulation | `counter` |

```yaml
tools:
  ledger:
    history:
      type: log
      schema: { type: string }
    processed:
      type: set
    repositories:
      type: map
      schema: { type: object }
    findings:
      type: table
      key: id
      schema:
        type: object
        required: [id, severity]
        properties:
          id: { type: string }
          severity: { type: string }
    metrics:
      type: counter
```

The `map` built-in exposes dedicated safe-output tools:
`ledger_map_put` and `ledger_map_delete`. Supply the operation's fields and a
`ledger` name when more than one map ledger is configured. These tools produce
`ledger_append` entries internally; they do not expose the low-level operation
envelope to the agent. Other ledger types continue to use `ledger_append`
with `ledger` and `operation` instead of `record`, plus the indicated fields:

| Type | Operations | Materialized `state` columns |
| --- | --- | --- |
| `log` | `append(value)` | `position`, `value` |
| `set` | `add(value)`, `remove(value)` | `identity`, `value` |
| `map` | `put(key, value)`, `delete(key)` | `key`, `value` |
| `table` | `insert(value)`, `update(key, patch)`, `upsert(value)`, `delete(key)` | `key`, `value` |
| `counter` | `increment(name, amount)`, `decrement(name, amount)` | `name`, `value` |

Ledger writes are **deferred** safe outputs: their immediate response only confirms
that the intent was queued. The trusted persistence job resolves the outcome
later, after the agent has finished.

The declared schema validates **values**, not operation envelopes. `table` rows
must be objects with a string primary key. `insert` rejects duplicate keys;
`update` requires an existing row and shallowly merges the object `patch`,
without changing the primary key. The resulting row is schema-validated.
`upsert` **replaces** the entire row; `delete` of an absent key is harmless.
`map` keys and counter names are strings; map puts replace earlier values.
`counter` amounts are nonnegative safe integers (including zero), with no
coercion; arithmetic must remain within the safe-integer range. Counter names
start at zero. A `set` add/remove is idempotent: equality is the canonical
JSON serialization of finite, acyclic JSON values, with object keys sorted
recursively. Thus arrays retain order, object key order does not affect
membership, and strings, numbers, booleans, and null retain their JSON types.
The `state.value` column stores canonical JSON **text** for all types except
`counter`, whose `value` is an SQLite integer. Key columns remain directly
queryable. The generic `records` table still exposes immutable provenance.
The trusted persistence result reports ledger type, operation, deterministic
record ID, transaction ID, and validation status without echoing values.

Built-in projections replay canonical transaction order. Express domain concepts
as schemas on these types, not as custom replay code. Maintenance compaction is deliberately lossless: it
merges verified source segments while preserving every record ID, hash, and
parent relation. This preserves set/map/counter state deterministically but
does **not** fold away historical operations, because doing so would violate
the current compaction integrity contract.

Replay interprets the **logical, ordered record stream**. Records follow
topological parent order, with SHA-256 lexical order for simultaneously ready
records. This order depends on logical history, not shard names or physical layout.
Equivalent history after compaction produces the same state.

Trusted preparation validates canonical records before replay and fails if
built-in operations are invalid. Each ledger's disposable SQLite database at
`/tmp/gh-aw/ledgers/<name>/ledger.db` remains read-only to the agent. The generated
prompt lists the derived tables and their columns. Query `state` for current
state, `replay_metadata` for projection metadata, and `records` for immutable
event history. Replay never writes back to Git or canonical ledger files.
Persist new events only through the configured ledger safe-output tools.

Ledgers without a declared type retain their generic `records` projection.
To preserve existing raw-record history when removing custom replay, query
`records.payload` with SQLite JSON functions. Switching that history to a
built-in type requires migrating its payloads to that type's operation format.
The serialized configuration for all ledgers remains limited to 96 KiB after
base64 encoding.
