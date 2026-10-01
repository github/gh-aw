---
title: Standalone ledger replay projections
description: Build disposable read-only SQLite materialized views from immutable ledger history
---

Standalone `tools.ledger` ledgers may declare an inline JavaScript `replay.script`.
For common state models, use a built-in `type` instead: gh-aw validates each
operation and replays it in trusted code, without a custom script.

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

All writes use the **same** `ledger_append` safe output. For built-ins, provide
`ledger` and `operation` instead of `record`, plus the indicated fields:

| Type | Operations | Materialized `state` columns |
| --- | --- | --- |
| `log` | `append(value)` | `position`, `value` |
| `set` | `add(value)`, `remove(value)` | `identity`, `value` |
| `map` | `put(key, value)`, `delete(key)` | `key`, `value` |
| `table` | `insert(value)`, `update(key, patch)`, `upsert(value)`, `delete(key)` | `key`, `value` |
| `counter` | `increment(name, amount)`, `decrement(name, amount)` | `name`, `value` |

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

Built-in projections replay canonical transaction order and cannot be combined
with a custom `replay.script`. Domain concepts should be expressed as schemas
on generic ledger types, not as new built-in types; custom replay remains an
escape hatch. Existing maintenance compaction is deliberately lossless: it
merges verified source segments while preserving every record ID, hash, and
parent relation. This preserves set/map/counter state deterministically but
does **not** fold away historical operations, because doing so would violate
the current compaction integrity contract.

Replay interprets the **logical, ordered record stream** and returns a declarative
table model. Ledger JSONL records remain authoritative; replay tables are derived
and can always be rebuilt. Replay never writes back to Git or canonical ledger
files. Persist new events only through the ledger append safe output.

```yaml
tools:
  ledger:
    findings:
      replay:
        script: |
          const items = new Map()
          for (const record of records) {
            if (record.payload.kind === "created") {
              items.set(record.payload.id, { id: record.payload.id, status: "open" })
            }
            if (record.payload.kind === "closed" && items.has(record.payload.id)) {
              items.get(record.payload.id).status = "closed"
            }
          }
          return {
            version: 1,
            tables: {
              items: {
                columns: { id: "text", status: "text" },
                primaryKey: ["id"],
                rows: [...items.values()]
              }
            }
          }
```

The script receives deep-frozen `records` and optional `replay.config` (an empty
object by default). The config must be a bounded JSON object. The serialized
configuration for all ledgers is limited to 96 KiB after base64 encoding.
Records follow the ledger's canonical reconstruction order: topological parent
order, with SHA-256 lexical order for simultaneously ready records. This order
depends on logical history, not shard names or physical layout; equivalent
history after compaction gives the same replay input. Replay scripts are executable
workflow configuration; only use trusted scripts. They run in a separate Node.js
process with an empty environment and restrictive Node.js permissions as defense
in depth, but the JavaScript `vm` context is not a security boundary and must not
be used to execute hostile scripts. The context omits process, filesystem, network,
module-loading, wall-clock, and random APIs, but this is not a security guarantee.
The runtime provides no database handle or SQL interface. Treat ledger records as
untrusted data and never execute payload content.

Replay output must contain `tables` and may specify `version: 1` (the default).
Each table has `columns`, a nonempty `primaryKey`, and `rows`. Column types are
`text`, `integer`, `real`, `boolean`, and `json`; JSON values are stored as JSON
text and booleans as SQLite integers. Table and column identifiers must start
with a letter and contain only ASCII letters, digits, or underscores (up to 64
characters). Names beginning `sqlite_` and built-in tables (`records`, `parents`,
`shards`, `diagnostics`, `replay_metadata`) are reserved. Limits include a 64 KiB
script, 16 MiB input, 4 MiB output, 16 tables, 32 columns per table, 10,000
rows total, and 64 KiB per cell.

Trusted preparation validates canonical records before replay. A replay failure
produces a bounded warning and leaves the generic ledger projection intact,
without partial replay tables. The per-ledger SQLite database at
`/tmp/gh-aw/ledgers/<name>/ledger.db` remains read-only to the agent. The generated
agent prompt lists materialized replay tables and their column names and types
(or reports that replay fell back); exceptionally large lists are abbreviated.
Query `replay_metadata` for generated table names, columns, ledger name, record
count, script SHA-256, and projection/output versions; query `records` for event
history.
Each ledger runs replay independently. Change the replay script to reinterpret
older payload versions without rewriting past records.
