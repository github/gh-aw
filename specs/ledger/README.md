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
- **PersistRevalidation (action precondition):** `Persist` checks the artifact
  against the latest replayed history; `RejectStale` discards it if no longer
  allowed, without changing history.

## Behavioral test coverage

The conformance tests use the repository's JavaScript ledger implementation;
there is no Go ledger package to test. `ledger_protocol_generated.test.cjs`
checks each TLC operation witness against `replayBuiltin`, verifies append-only
history and shard coverage, and ensures queueing or validation leaves the
snapshot unchanged. `ledger_protocol_model.test.cjs` adds hand-selected witnesses for each ledger
type, including invalid note votes.

| Property | Concrete test |
| --- | --- |
| `ReplayAgreement` | `ledger_protocol_generated.test.cjs` — each `TLC trace: <kind>/<op>` compares replay with the model projection. |
| `NoEarlyDurability` / `ReadOnlySnapshot` | `ledger_protocol_generated.test.cjs` — queue and validation preserve history, shard coverage, and snapshot. |
| `ImmutableHistory` | `ledger_protocol_generated.test.cjs` — shard IDs cover exactly the IDs in model history. |
| `NotesReferentialIntegrity` | `ledger_protocol_model.test.cjs` — valid and missing note targets. |
| Selected operation witnesses | `ledger_protocol_model.test.cjs` — duplicate log entries, set add/remove, map replacement, table insert/update/upsert, and signed counters; `ledger_builtin.test.cjs` covers idempotent set adds. |
| `CounterBound` | `ledger_builtin.test.cjs` — unsafe counter arithmetic is rejected without changing state. |
| `PersistRevalidation` | `push_ledger_changes.test.cjs` — persistence rejects a table insert that became invalid after a concurrent branch update. |
| `CompactPreservesStream` | `ledger_compaction.test.cjs` — compaction preserves record hashes, including concurrent appends. |

These implementation tests complement, rather than extend, the bounded TLC
claims: the model's `Compact` action preserves logical history and shard-ID
coverage without representing a physical layout change. Concrete compaction
and Git push behavior are tested separately and are not modeled by
`LedgerProtocol.tla`.

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
durable records. TLC from [TLA+ Tools 1.8.0](https://github.com/tlaplus/tlaplus/releases/tag/v1.8.0)
completed exhaustive exploration without invariant violations (distinct states:
raw **4,637**, log **4,637**, set
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

**Requirements**

- Results MUST be interpreted only as bounded checks of the abstractions and
  configurations listed above, not as an unbounded theorem or a proof of the
  implementation.
- Concrete runtime behavior MUST be assessed separately with implementation
  tests; the model's shortest operation witnesses are not exhaustive
  conformance tests for every reachable state.
- The model does not prove batch temporary-ID rewriting, forward references,
  SHA-256 collision resistance, JSON schemas, record/shard byte limits, Git
  authentication, failed network pushes, DAG topological ordering, or real
  SQLite behavior.

**Safeguards**

- A value MUST be understood as a canonical-JSON equality class; table rows,
  counters, notes, transaction IDs, and artifacts remain bounded abstractions.
- Claims about `Compact` MUST be limited to preserving modeled history and
  shard-ID coverage; physical compaction is not represented or verified.
- The model MUST NOT be cited as evidence of liveness or
  distributed-serializability. Production tests cover separate obligations.

## Last synced with implementation (2026-11-06)

- **Go:** None; this repository has no Go ledger package.
- **TLA+:** `LedgerProtocol.tla`, `LedgerProtocolWitness.tla`, and the `*.cfg`
  model configurations in this directory.
- **JavaScript implementation and tests:** `actions/setup/js/ledger_append.cjs`,
  `ledger_replay.cjs`, `ledger_builtin.cjs`, `ledger_transactions.cjs`,
  `push_ledger_changes.cjs`, `ledger_compaction.cjs`,
  `ledger_protocol_model.test.cjs`, and `ledger_protocol_generated.test.cjs`.
