// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { DatabaseSync } from "node:sqlite";
import { applyLedgerCompactions, executeReplay, materializeReplay, validateReplayOutput } from "./ledger_replay.cjs";

const table = (rows = [{ id: "a" }]) => ({ columns: { id: "text" }, primaryKey: ["id"], rows });
const output = tables => ({ version: 1, tables });

test("logical compaction drops selected rows and retains inserted data in replay order", () => {
  const records = [
    { id: "old", type: "ledger_append", payload: { id: "old", subject: "raw" } },
    { id: "new", type: "ledger_append", payload: { id: "new", subject: "summary" } },
    { id: "control", type: "ledger_compact", payload: { operation: "drop", id: "old" } },
  ];
  assert.deepEqual(applyLedgerCompactions(records), [records[1]]);
  assert.equal(records.length, 3);
  const envelope = { id: "ldg-envelope", type: "ledger_append", payload: { id: "ldg-payload" } };
  assert.deepEqual(applyLedgerCompactions([envelope, { type: "ledger_compact", payload: { operation: "drop", id: envelope.id } }]), []);
  assert.deepEqual(applyLedgerCompactions([envelope, { type: "ledger_compact", payload: { operation: "drop", id: envelope.payload.id } }]), []);
  assert.deepEqual(applyLedgerCompactions([{ type: "ledger_compact", payload: { operation: "drop", id: envelope.id } }, envelope]), []);
});

test("replay receives frozen logical order and can interpret historical versions", () => {
  const records = [{ payload: { id: "a", version: 1, state: "open" } }, { payload: { id: "a", version: 2, status: "closed" } }];
  const script = `if (!Object.isFrozen(records) || !Object.isFrozen(records[0].payload)) throw Error("mutable input");
    const states = new Map();
    for (const record of records) states.set(record.payload.id, {
      id: record.payload.id, status: record.payload.version === 1 ? record.payload.state : record.payload.status
    });
    return { version: 1, tables: { items: { columns: { id: "text", status: "text" },
      primaryKey: ["id"], rows: [...states.values()] } } };`;
  const first = executeReplay(script, records, { mode: "latest" });
  assert.deepEqual(first.tables.items.rows, [{ id: "a", status: "closed" }]);
  assert.deepEqual(executeReplay(script, records, { mode: "latest" }), first);
  assert.equal(executeReplay("return {tables: {items: {columns: {id: 'text'}, primaryKey: ['id'], rows: [{id: String(Object.isFrozen(config))}]}}}", [], { mode: "latest" }).tables.items.rows[0].id, "true");
});

test("replay denies host capabilities, randomness and wall clock", () => {
  for (const expression of ["process", "require", "fetch", "Date", "Intl", "performance", "crypto", "Math.random", "globalThis.Math.random", "Function", "eval"]) {
    const result = executeReplay(`return {tables: {items: {columns: {id: "text"}, primaryKey: ["id"], rows: [{id: typeof ${expression}}]}}}`, []);
    assert.equal(result.tables.items.rows[0].id, "undefined", expression);
  }
  assert.throws(() => executeReplay("while (true) {}", []), /timed out|failed/i);
  assert.throws(() => executeReplay("return {tables: {items: {columns: {id: 'real'}, primaryKey: ['id'], rows: [{id: Infinity}]}}}", []));
});

test("replay rejects malformed and excessive output", () => {
  for (const bad of [
    null,
    [],
    { tables: [] },
    output({ records: table() }),
    output({ records_history: table() }),
    output({ parents_history: table() }),
    output({ sqlite_shadow: table() }),
    output({ records_by_id: table() }),
    output({ Items: table(), items: table() }),
    output({ items: { ...table(), columns: { id: "blob" } } }),
    output({ items: table([{ id: "a" }, { id: "a" }]) }),
    output({ items: { ...table(), rows: [{ id: 1 }] } }),
    output({ items: { ...table(), rows: [{ id: "a".repeat(65537) }] } }),
    output({ items: { columns: { id: "text", data: "json" }, primaryKey: ["id"], rows: [{ id: "a", data: { value: Infinity } }] } }),
    output({ items: { ...table(), rows: Array.from({ length: 10001 }, (_, i) => ({ id: String(i) })) } }),
  ])
    assert.throws(() => validateReplayOutput(bad));
  assert.throws(() => executeReplay("return undefined", []));
  assert.throws(() => executeReplay("return {tables: {items: {columns: {id: 'text'}, primaryKey: ['id'], rows: [{id: 'a'.repeat(5_000_000)}]}}}", []));
});

test("multiple ledger replay databases are independent", () => {
  const script = "return {tables: {items: {columns: {id: 'text'}, primaryKey: ['id'], rows: [{id: records[0].payload.id}]}}}";
  for (const name of ["findings", "experiments"]) {
    const db = new DatabaseSync(":memory:");
    try {
      const records = [{ payload: { id: name } }];
      materializeReplay(db, name, script, records, executeReplay(script, records));
      assert.equal(db.prepare("SELECT id FROM items").get().id, name);
      assert.equal(db.prepare("SELECT ledger_name FROM replay_metadata").get().ledger_name, name);
    } finally {
      db.close();
    }
  }
});

test("trusted materialization encodes booleans and JSON without executing SQL", () => {
  const db = new DatabaseSync(":memory:");
  try {
    const replay = output({
      facts: {
        columns: { id: "text", count: "integer", score: "real", active: "boolean", data: "json" },
        primaryKey: ["id"],
        rows: [{ id: "a", count: 2, score: 0.5, active: true, data: { query: "DROP TABLE records" } }],
      },
    });
    materializeReplay(db, "findings", "return {}", [], replay);
    const row = db.prepare("SELECT * FROM facts").get();
    assert.equal(row.active, 1);
    assert.equal(row.count, 2);
    assert.equal(row.score, 0.5);
    assert.deepEqual(JSON.parse(row.data), { query: "DROP TABLE records" });
  } finally {
    db.close();
  }
});
