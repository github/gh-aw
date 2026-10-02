// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { DatabaseSync } from "node:sqlite";
import { materializeReplay, validateReplayOutput } from "./ledger_replay.cjs";
import { replayBuiltin } from "./ledger_builtin.cjs";

const table = (rows = [{ id: "a" }]) => ({ columns: { id: "text" }, primaryKey: ["id"], rows });
const output = tables => ({ version: 1, tables });

test("replay rejects malformed and excessive output", () => {
  for (const bad of [
    null,
    [],
    { tables: [] },
    output({ records: table() }),
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
});

test("multiple ledger replay databases are independent", () => {
  for (const name of ["findings", "experiments"]) {
    const db = new DatabaseSync(":memory:");
    try {
      const records = [{ type: "ledger_append", payload: { operation: "append", value: name } }];
      materializeReplay(db, name, "builtin:log", records, replayBuiltin({ type: "log" }, records));
      assert.equal(db.prepare("SELECT value FROM state").get().value, JSON.stringify(name));
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
