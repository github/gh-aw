# Ledger protocol: bounded TLA+ model-checking results

`LedgerProtocol.tla` models the generic raw-record ledger and all six declared
types (`log`, `set`, `map`, `table`, `counter`, `notes`). Its actions represent the
agent's queued intent, validation in `safe_outputs`, the versioned transaction
artifact, independent revalidation and atomic Git-branch push in
`push_ledger_changes`, a disposable read-only agent snapshot, and the
maintenance plan/apply boundary. The immutable history is replayed independently
of the incremental projection. Compaction changes only the physical layout, not
the ordered logical record stream.

The model checks these safety properties in every reachable state:

- **ReplayAgreement:** incrementally applied state equals replay from canonical
  records (including duplicate log entries, idempotent set membership, map
  replacement, table insert/update/upsert/delete, signed counters, and notes).
- **NoEarlyDurability:** queued and validated requests are not durable; only
  successful persistence adds their record IDs to the branch.
- **ImmutableHistory:** physical shard coverage includes exactly the IDs in the
  immutable logical history, even after maintenance.
- **ReadOnlySnapshot:** the agent's snapshot is a replay of an earlier prefix
  of committed records; safe-output requests never mutate it.
- **NotesReferentialIntegrity:** persisted votes refer to existing notes.
- **CounterBound:** arithmetic stays within the modeled safe range.

## Reproduce

With Java and a TLA+ Tools `tla2tools.jar` installed outside the repository:

```sh
cd specs/ledger
for kind in raw log set map table counter notes; do
  java -cp /path/to/tla2tools.jar tlc2.TLC \
    -config "$kind.cfg" -workers 4 -deadlock \
    -metadir "/tmp/gh-aw-tlc-$kind" LedgerProtocol.tla
done
```

Each configuration has two keys, two abstract JSON values, and at most three
durable records. TLC 2026.10.01 completed exhaustive exploration without
invariant violations (distinct states: raw **6,221**, log **6,221**, set
**28,185**, map **29,457**, table **119,673**, counter **42,361**, notes
**160,521**). The state counts may change with TLC version or model changes.
`actions/setup/js/ledger_protocol_model.test.cjs` exercises concrete
corresponding traces through transaction normalization, trusted artifact
validation, and the production built-in reducers/replay.

## Abstraction boundary

These are **bounded model-checking results**, not an unbounded TLAPS theorem or
a proof of the implementation. A value denotes a canonical-JSON equality
class, a table row has a primary key and one non-key field, a counter operation
has amount one, and a note has a valid citation represented abstractly. One
artifact contains one operation at a time; batch temporary-ID rewriting,
forward references in a batch, SHA-256 collision resistance, JSON schemas,
record/shard byte limits, Git authentication, failed network pushes, DAG
topological ordering, and real SQLite are not modeled. In particular,
`Compact` abstracts the verified replacement as a layout change preserving
the entire logical history; it does not verify the compaction implementation.
The production tests and the Git-backed ledger specification cover these
separate obligations. Do not interpret the safety results as a liveness or
distributed-serializability guarantee.
