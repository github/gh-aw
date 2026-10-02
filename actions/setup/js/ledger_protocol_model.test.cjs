// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { createReducer, replayBuiltin } from "./ledger_builtin.cjs";
import { normalizeLedgerAppends } from "./ledger_transactions.cjs";
import { validateTransactions } from "./push_ledger_changes.cjs";

// Concrete witnesses for the bounded transitions in specs/ledger/LedgerProtocol.tla.
const cases = [
  {
    type: "log",
    events: [
      { operation: "append", value: "x" },
      { operation: "append", value: "x" },
      { operation: "append", value: "y" },
    ],
    state: [
      { position: 0, value: '"x"' },
      { position: 1, value: '"x"' },
      { position: 2, value: '"y"' },
    ],
  },
  {
    type: "set",
    events: [
      { operation: "add", value: "x" },
      { operation: "add", value: "y" },
      { operation: "remove", value: "x" },
    ],
    state: [{ identity: '"y"', value: '"y"' }],
  },
  {
    type: "map",
    events: [
      { operation: "put", key: "a", value: "x" },
      { operation: "put", key: "a", value: "y" },
      { operation: "delete", key: "b" },
    ],
    state: [{ key: "a", value: '"y"' }],
  },
  {
    type: "table",
    config: { key: "id" },
    events: [
      { operation: "insert", value: { id: "a", field: "x" } },
      { operation: "update", key: "a", patch: { field: "y" } },
      { operation: "upsert", value: { id: "b", field: "x" } },
    ],
    state: [
      { key: "a", value: '{"field":"y","id":"a"}' },
      { key: "b", value: '{"field":"x","id":"b"}' },
    ],
  },
  {
    type: "counter",
    events: [
      { operation: "decrement", name: "a", amount: 1 },
      { operation: "increment", name: "a", amount: 1 },
      { operation: "increment", name: "b", amount: 1 },
    ],
    state: [
      { name: "a", value: 0 },
      { name: "b", value: 1 },
    ],
  },
];

for (const { type, config = {}, events, state } of cases) {
  test(`${type} model witness agrees with trusted artifact validation and replay`, () => {
    const definition = { type, ...config };
    const ledger = { name: "example", ...definition };
    const requests = events.map(event => ({ ledger: "example", ...event }));
    const normalized = normalizeLedgerAppends(requests, { transactionId: `model-${type}`, ledgerNames: new Set(["example"]), ledgers: { example: definition } });
    const artifact = { version: 1, transaction_id: `model-${type}`, ledgers: { example: { appends: normalized.appends } } };
    assert.doesNotThrow(() => validateTransactions(artifact, [ledger]));

    const reducer = createReducer(definition);
    assert.deepEqual(reducer.output().tables.state.rows, []);
    const durable = [];
    for (const event of events) {
      // Validation and artifact creation do not change the agent's snapshot.
      assert.deepEqual(
        replayBuiltin(
          definition,
          durable.map(payload => ({ payload }))
        ).tables.state.rows,
        reducer.output().tables.state.rows
      );
      reducer.apply(event);
      durable.push(event);
      assert.deepEqual(
        replayBuiltin(
          definition,
          durable.map(payload => ({ payload }))
        ),
        reducer.output()
      );
    }
    assert.deepEqual(reducer.output().tables.state.rows, state);
    assert.deepEqual(
      replayBuiltin(
        definition,
        durable.map(payload => ({ payload }))
      ).tables.state.rows,
      state
    );
  });
}

test("notes model witness keeps immutable notes and votes with valid targets", () => {
  const definition = { type: "notes" };
  const normalized = normalizeLedgerAppends(
    [
      { temp_id: "first", operation: "note", subject: "A", note: "x", reason: "Evidence", citations: [{ type: "repository", path: "README.md" }] },
      { operation: "vote", note_id: "#first", vote: "up" },
      { operation: "vote", note_id: "#first", vote: "down" },
    ],
    { transactionId: "model-notes", ledgerNames: new Set(["example"]), ledgers: { example: definition } }
  );
  const artifact = { version: 1, transaction_id: "model-notes", ledgers: { example: { appends: normalized.appends } } };
  assert.doesNotThrow(() => validateTransactions(artifact, [{ name: "example", ...definition }]));
  const records = normalized.appends.map(({ record: { id, ...payload } }) => ({ id, payload }));
  assert.equal(records[1].payload.note_id, records[0].id);
  const output = replayBuiltin(definition, records);
  assert.deepEqual(
    output.tables.notes.rows.map(row => row.id),
    [records[0].id]
  );
  assert.deepEqual(
    output.tables.note_votes.rows.map(row => row.vote),
    ["up", "down"]
  );
  assert.throws(() => replayBuiltin(definition, records.slice(1)), /missing note/);
});

test("raw model witness keeps generic record payloads and deterministic artifact IDs", () => {
  const requests = [{ record: { value: "x" } }, { record: { value: "x" } }, { record: { value: "y" } }];
  const options = { transactionId: "model-raw", ledgerNames: new Set(["example"]) };
  const first = normalizeLedgerAppends(requests, options);
  assert.deepEqual(normalizeLedgerAppends(requests, options), first);
  assert.equal(new Set(first.appends.map(item => item.record.id)).size, 3);
  assert.doesNotThrow(() => validateTransactions({ version: 1, transaction_id: "model-raw", ledgers: { example: { appends: first.appends } } }, [{ name: "example" }]));
});
