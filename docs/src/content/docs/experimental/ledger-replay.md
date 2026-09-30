---
title: Standalone ledger replay projections
description: Build disposable read-only SQLite materialized views from immutable ledger history
---

Standalone `tools.ledger` ledgers may declare an inline JavaScript `replay.script`.
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
object by default). The config must be a bounded JSON object.
Records follow the ledger's canonical reconstruction order: topological parent
order, with SHA-256 lexical order for simultaneously ready records. This order
depends on logical history, not shard names or physical layout; equivalent
history after compaction gives the same replay input. Scripts must be deterministic:
the isolated subprocess exposes no process, filesystem, network, module loading,
wall-clock time, or randomness. Scripts cannot execute SQL or write SQLite.

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
`/tmp/gh-aw/ledgers/<name>/ledger.db` remains read-only to the agent. Query
The generated agent prompt lists each successfully materialized replay table and
its columns (or reports that replay fell back). Query `replay_metadata` for
generated table names, columns, ledger name, record count, script SHA-256, and
projection/output versions; query `records` for event history.
Each ledger runs replay independently. Change the replay script to reinterpret
older payload versions without rewriting past records.
