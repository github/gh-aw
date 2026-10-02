# Ledger protocol: bounded TLA+ model-checking results

`LedgerProtocol.tla` models the generic raw-record ledger and all six declared
types (`log`, `set`, `map`, `table`, `counter`, `notes`). Its actions represent the
agent's queued intent, validation in `safe_outputs`, the versioned transaction
artifact, independent revalidation and atomic Git-branch push in
`push_ledger_changes`, a disposable read-only agent snapshot, and the
maintenance plan/apply boundary. A separate writer may advance the branch after
validation; persistence rechecks the operation against the latest history and
rejects one made invalid by that writer. The immutable history is replayed independently
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

Each configuration has two keys, two abstract JSON values, and at most two
durable records. TLC 2026.10.01 completed exhaustive exploration without
invariant violations (distinct states: raw **4,637**, log **4,637**, set
**19,097**, map **19,241**, table **111,795**, counter **26,009**, notes
**203,331**). The state counts may change with TLC version or model changes.
`actions/setup/js/ledger_protocol_model.test.cjs` exercises hand-selected
concrete witnesses. The generated vectors in
`actions/setup/js/ledger_protocol_vectors.json` cover every declared
operation. Regenerate them after editing the model or its configurations:

```sh
TLA2TOOLS_JAR=/path/to/tla2tools.jar \
  node actions/setup/js/generate_ledger_protocol_vectors.cjs
cd actions/setup/js
npm run test:js -- ledger_protocol_generated.test.cjs
```

`LedgerProtocolWitness.tla` adds one deliberately violated coverage invariant
per operation. TLC's shortest JSON counterexample supplies the sequence of
model states and actions; the generator checks each trace and writes the
resulting test vectors. The JavaScript tests translate the abstract events into
real transaction artifacts and built-in operations, then compare every
intermediate replay result to the TLA+ projection. They also check that
queuing and validation do not persist records. The source digest fails tests
if the TLA+ model or its bounds change without regenerating vectors. TLC
returns exit code 12 for the intentional witness invariant violations; this
is distinct from the passing safety checks above.

## Abstraction boundary

These are **bounded model-checking results**, not an unbounded TLAPS theorem or
a proof of the implementation. A value denotes a canonical-JSON equality
class, a table row has a primary key and one non-key field, a counter operation
has amount one, and a note has a valid citation represented abstractly. IDs
distinguish transactions but do not model hashes or duplicate-ID retries. One
artifact contains one operation at a time; the generated traces are shortest
operation witnesses, **not** exhaustive conformance tests for every reachable
state. Batch temporary-ID rewriting,
forward references in a batch, SHA-256 collision resistance, JSON schemas,
record/shard byte limits, Git authentication, failed network pushes, DAG
topological ordering, and real SQLite are not modeled. In particular,
`Compact` abstracts the verified replacement as a layout change preserving
the entire logical history; it does not verify the compaction implementation.
The production tests and the Git-backed ledger specification cover these
separate obligations. Do not interpret the safety results as a liveness or
distributed-serializability guarantee.
