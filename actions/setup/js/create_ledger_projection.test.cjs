// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { createHash } from "node:crypto";
import { DatabaseSync } from "node:sqlite";
import { Ledger } from "./ledger_store.cjs";
import { createProjection } from "./create_ledger_projection.cjs";

test("creates a read-only SQLite projection from canonical ledger shards", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-projection-"));
  const sourceDir = path.join(root, "source");
  const databasePath = path.join(root, "projection", "ledger.db");
  const config = {
    name: "findings",
    schema: { type: "object", required: ["subject"], properties: { subject: { type: "string" } }, additionalProperties: false },
    max_record_kb: 32,
    max_segment_kb: 100,
    max_patch_kb: 10,
  };
  fs.mkdirSync(sourceDir, { recursive: true });
  const ledger = new Ledger({ memoryDir: sourceDir });
  try {
    ledger.append("finding", { subject: "A finding" });
    ledger.close();
    createProjection({ sourceDir, databasePath, config });

    const database = new DatabaseSync(databasePath, { readOnly: true });
    try {
      const row = database.prepare("SELECT payload FROM records").get();
      assert.deepEqual(JSON.parse(row.payload), { subject: "A finding" });
    } finally {
      database.close();
    }
    assert.equal(fs.statSync(databasePath).mode & 0o777, 0o444);
  } finally {
    ledger.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("replay materializes state alongside immutable records and trusted metadata", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-replay-"));
  const sourceDir = path.join(root, "source");
  const databasePath = path.join(root, "projection", "ledger.db");
  const script = `const items = new Map();
    for (const record of records) {
      if (record.payload.kind === "created") items.set(record.payload.id, { id: record.payload.id, status: "open" });
      if (record.payload.kind === "closed") items.get(record.payload.id).status = "closed";
    }
    return { version: 1, tables: { items: { columns: { id: "text", status: "text" }, primaryKey: ["id"], rows: [...items.values()] },
      totals: { columns: { count: "integer" }, primaryKey: ["count"], rows: [{ count: items.size }] } } };`;
  const ledger = new Ledger({ memoryDir: sourceDir });
  try {
    fs.mkdirSync(sourceDir);
    ledger.append("finding", { kind: "created", id: "123", version: 1 });
    ledger.append("finding", { kind: "closed", id: "123", version: 2 });
    createProjection({ sourceDir, databasePath, config: { name: "findings", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, replay: { script } } });
    const db = new DatabaseSync(databasePath, { readOnly: true });
    try {
      assert.deepEqual(
        [...db.prepare("SELECT id, status FROM items").all()].map(row => ({ ...row })),
        [{ id: "123", status: "closed" }]
      );
      assert.equal(db.prepare("SELECT count(*) AS n FROM records").get().n, 2);
      assert.equal(db.prepare("SELECT count FROM totals").get().count, 1);
      const meta = db.prepare("SELECT * FROM replay_metadata WHERE table_name = 'items'").get();
      assert.equal(meta.ledger_name, "findings");
      assert.equal(meta.record_count, 2);
      assert.match(meta.script_sha256, /^[a-f0-9]{64}$/);
      assert.equal(meta.script_sha256, createHash("sha256").update(script).digest("hex"));
      assert.equal(meta.output_version, 1);
      assert.throws(() => db.exec("UPDATE items SET status = 'open'"));
    } finally {
      db.close();
    }
  } finally {
    ledger.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("invalid replay preserves generic records without partial tables", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-replay-fallback-"));
  const sourceDir = path.join(root, "source");
  const databasePath = path.join(root, "projection", "ledger.db");
  const ledger = new Ledger({ memoryDir: sourceDir });
  const warnings = [];
  try {
    fs.mkdirSync(sourceDir);
    ledger.append("finding", { id: "a" });
    createProjection({
      sourceDir,
      databasePath,
      config: { name: "findings", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, replay: { script: `return { tables: { items: { columns: { id: "text" }, primaryKey: ["id"], rows: [{ id: "a" }, { id: "a" }] } } }` } },
      onReplayError: message => warnings.push(message),
    });

    assert.equal(warnings.length, 1);
    const db = new DatabaseSync(databasePath, { readOnly: true });
    try {
      assert.equal(db.prepare("SELECT count(*) AS n FROM records").get().n, 1);
      assert.equal(db.prepare("SELECT count(*) AS n FROM sqlite_master WHERE name = 'items'").get().n, 0);
    } finally {
      db.close();
    }
  } finally {
    ledger.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("compaction preserves replay's ordered logical history", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-replay-compaction-"));
  const sourceDir = path.join(root, "source");
  fs.mkdirSync(sourceDir);
  const script = `return { tables: { history: { columns: { position: "integer", id: "text" },
    primaryKey: ["position"], rows: records.map((record, position) => ({ position, id: record.payload.id })) } } }`;
  const config = { name: "findings", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, replay: { script } };
  const first = new Ledger({ memoryDir: sourceDir });
  const second = new Ledger({ memoryDir: sourceDir });
  const compactor = new Ledger({ memoryDir: sourceDir });
  try {
    first.append("finding", { id: "first" });
    second.append("finding", { id: "second" });
    const before = path.join(root, "before.db");
    const after = path.join(root, "after.db");
    createProjection({ sourceDir, databasePath: before, config });
    assert.equal((await compactor.compact({ minSegments: 2, maxSegments: 2 })).changed, true);
    createProjection({ sourceDir, databasePath: after, config });
    const oldDb = new DatabaseSync(before, { readOnly: true });
    const newDb = new DatabaseSync(after, { readOnly: true });
    try {
      assert.deepEqual(
        oldDb
          .prepare("SELECT id FROM history ORDER BY position")
          .all()
          .map(row => row.id),
        newDb
          .prepare("SELECT id FROM history ORDER BY position")
          .all()
          .map(row => row.id)
      );
    } finally {
      oldDb.close();
      newDb.close();
    }
  } finally {
    first.close();
    second.close();
    compactor.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});
