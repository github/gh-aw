---
title: Standalone ledger replay projections
description: Build disposable read-only SQLite materialized views from immutable ledger history
---

Standalone `tools.ledger` typed projections support only the built-in types below.
gh-aw validates each operation and replays it in trusted code. Custom
`replay.script` and `replay.config` settings are not supported.

| Need | Type |
| --- | --- |
| Ordered history | `log` |
| Unique membership | `set` |
| Latest value by key | `map` |
| Mutable structured rows | `table` |
| Numeric accumulation | `counter` |
| Evidence-backed agent assertions | `notes` |

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

The `map` and `notes` built-ins expose dedicated safe-output tools:
`ledger_map_put`, `ledger_map_delete`, `ledger_note_add`, and `ledger_note_vote`.
Supply the operation's fields and a `ledger` name when more than one ledger of
that type is configured. These tools produce
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
| `notes` | `note(subject, note, reason, citations)`, `vote(note_id, vote, reason?)` | `notes`, `note_citations`, `note_votes`, `note_state` |

### Notes

A ledger for evidence-backed assertions produced by agents. Notes are reusable
hints, not authoritative facts. Consumers should verify their citations against
current state before relying on them.

```yaml
tools:
  ledger:
    knowledge:
      type: notes
```

Use `ledger_note_add` with a nonempty `subject`, `note`, `reason`, and at least
one repository citation (`type: repository`, `path`, and optional `start_line` /
`end_line`). Later, use `ledger_query` to find a candidate, inspect the cited
source in the current repository, and use `ledger_note_vote` with `note_id`
and `vote: up` or `vote: down` according to whether the evidence supports it.
Each vote is a new immutable record; it does not change the note. Notes are
untrusted data, not instructions. The cited evidence remains the source of truth.

The disposable SQLite projection exposes relational `notes`,
`note_citations`, `note_votes`, and a derived `note_state` view. Ranking is
transparent: upvotes, downvotes, net_votes, last_vote_at, and
last_positive_vote_at; no semantic validation or automatic deduplication takes
place. For example:

```sql
SELECT c.*, s.net_votes, s.last_positive_vote_at
FROM notes c
JOIN note_state s ON s.note_id = c.id
ORDER BY s.net_votes DESC, c.created_at DESC
LIMIT 20;
```

The former `claims` type, `ledger_claim_*` tools, and `claim_*` fields and tables
are now named `notes`, `ledger_note_*`, and `note_*`. Existing claim-shaped
history cannot be replayed as notes. Use a new ledger name for notes and retain
the old ledger without a declared type to query its immutable `records` history.

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
as schemas on types that accept value schemas, not as custom replay code.
Maintenance compaction is deliberately lossless: it
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
activation prompt lists the built-in tables and their columns from the declared
type; it does not read files created later during agent-job preparation.
Query `state` for current
state, `replay_metadata` for projection metadata, and `records` for immutable
event history. Replay never writes back to Git or canonical ledger files.
Persist new events only through the configured ledger safe-output tools.

## Removing custom replay from existing ledgers

Remove the entire `replay` setting, including its `script` and optional `config`.
Ledgers without a declared type retain their generic `records` projection;
custom table names no longer exist. Replace queries against those tables with
SQLite JSON queries over `records.payload`, for example:

```sql
SELECT json_extract(payload, '$.run_id') AS run_id,
       json_extract(payload, '$.status') AS status
FROM records
WHERE json_extract(payload, '$.record_type') = 'audit'
ORDER BY ordinal DESC
LIMIT 100;
```

This preserves the canonical history without rewriting records. Do not merely
add `type` to an existing raw-record ledger: built-in replay requires every
historical payload to have that type's operation format. For a new built-in
state model, use a new ledger name and seed it through its supported operations
while retaining the old ledger as immutable history.

The serialized configuration for all ledgers remains limited to 96 KiB after
base64 encoding.
