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
import { applyTransitionToDirectory, createPlan, loadSegments, parseCompactionConfig, prepareApply, selectSources, validatePlan } from "./ledger_compaction.cjs";
import { createProjection, formatReplayPrompt, formatReplayTable } from "./create_ledger_projection.cjs";
import { replayBuiltin } from "./ledger_builtin.cjs";

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

test("built-in table projects keyed rows alongside provenance without executing a script", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-builtin-projection-"));
  const sourceDir = path.join(root, "source");
  const databasePath = path.join(root, "projection", "ledger.db");
  const ledger = new Ledger({ memoryDir: sourceDir });
  try {
    fs.mkdirSync(sourceDir);
    ledger.append("ledger_append", { id: "ldg-12345678-1234-4123-8123-123456789abc", operation: "insert", value: { id: "a", score: 1 } });
    ledger.append("ledger_append", { id: "ldg-12345678-1234-4123-8123-123456789abd", operation: "update", key: "a", patch: { score: 3 } });
    createProjection({
      sourceDir,
      databasePath,
      config: {
        name: "items",
        type: "table",
        key: "id",
        schema: { type: "object", required: ["id", "score"], properties: { id: { type: "string" }, score: { type: "number" } } },
        max_record_kb: 32,
        max_segment_kb: 100,
        max_patch_kb: 10,
      },
    });
    const db = new DatabaseSync(databasePath, { readOnly: true });
    try {
      assert.deepEqual(
        [...db.prepare("SELECT key, value FROM state").all()].map(row => ({ ...row })),
        [{ key: "a", value: '{"id":"a","score":3}' }]
      );
      assert.equal(db.prepare("SELECT count(*) AS n FROM records").get().n, 2);
      assert.equal(db.prepare("SELECT table_name FROM replay_metadata").get().table_name, "state");
    } finally {
      db.close();
    }
  } finally {
    ledger.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("replay guidance remains bounded for many valid tables and ledgers", () => {
  const lines = Array.from({ length: 1024 }, (_, index) => `- table_${index}(${Array.from({ length: 32 }, (_, column) => `column_${column}_${"a".repeat(48)}`).join(", ")})`);
  const prompt = formatReplayPrompt(lines);
  assert.ok(Buffer.byteLength(prompt) <= 65536);
  assert.match(prompt, /Additional replay tables or ledgers/);
  assert.match(prompt, /replay_metadata/);
});

test("replay table guidance includes column types", () => {
  assert.equal(formatReplayTable("items", { columns: { id: "text", count: "integer", data: "json" } }), "- items(id: text, count: integer, data: json)");
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

test("compaction preserves replay's ordered logical history", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-replay-compaction-"));
  const sourceDir = path.join(root, "source");
  fs.mkdirSync(sourceDir);
  const script = `return { tables: { history: { columns: { position: "integer", id: "text" },
    primaryKey: ["position"], rows: records.map((record, position) => ({ position, id: record.payload.id })) } } }`;
  const config = { name: "findings", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, replay: { script } };
  const first = new Ledger({ memoryDir: sourceDir });
  const second = new Ledger({ memoryDir: sourceDir });
  const compaction = parseCompactionConfig(
    JSON.stringify({ name: "findings", branch_name: "ledgers/findings", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, compaction: { schedule: "daily", min_segments: 2, max_segments: 2 } })
  );
  try {
    first.append("finding", { id: "first" });
    second.append("finding", { id: "second" });
    const before = path.join(root, "before.db");
    const after = path.join(root, "after.db");
    createProjection({ sourceDir, databasePath: before, config });
    const loaded = loadSegments(sourceDir, compaction);
    const { sources } = selectSources(loaded, compaction);
    assert.ok(sources);
    const plan = validatePlan(createPlan({ loaded, sources, config: compaction, trigger: "scheduled", baseCommit: "a".repeat(40) }), compaction);
    const prepared = prepareApply({ plan, sourceDir, config: compaction });
    assert.equal(prepared.status, "ready");
    applyTransitionToDirectory(sourceDir, prepared);
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
    fs.rmSync(root, { recursive: true, force: true });
  }
});

for (const [type, transactions] of Object.entries({
  set: [
    { operation: "add", value: "a" },
    { operation: "remove", value: "a" },
    { operation: "add", value: "a" },
  ],
  map: [
    { operation: "put", key: "a", value: 1 },
    { operation: "put", key: "a", value: 2 },
    { operation: "delete", key: "missing" },
  ],
  counter: [
    { operation: "increment", name: "a", amount: 3 },
    { operation: "increment", name: "a", amount: 5 },
    { operation: "decrement", name: "a", amount: 2 },
  ],
})) {
  test(`${type} replay is unchanged by trusted lossless compaction`, () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), `ledger-${type}-compact-`));
    fs.mkdirSync(root, { recursive: true });
    const config = { name: type, type };
    const compaction = parseCompactionConfig(
      JSON.stringify({
        name: type,
        branch_name: `ledgers/${type}`,
        max_record_kb: 32,
        max_segment_kb: 100,
        max_patch_kb: 10,
        compaction: { min_segments: 2, max_segments: 2 },
      })
    );
    const writers = [];
    try {
      for (const payload of transactions) {
        const writer = new Ledger({ memoryDir: root });
        writers.push(writer);
        writer.append("ledger_append", payload);
      }
      const original = replayBuiltin(config, writers[0].reconstruct().records);
      const loaded = loadSegments(root, compaction);
      const { sources } = selectSources(loaded, compaction);
      assert.ok(sources);
      const plan = validatePlan(createPlan({ loaded, sources, config: compaction, trigger: "scheduled", baseCommit: "a".repeat(40) }), compaction);
      const prepared = prepareApply({ plan, sourceDir: root, config: compaction });
      assert.equal(prepared.status, "ready");
      applyTransitionToDirectory(root, prepared);
      assert.deepEqual(replayBuiltin(config, writers[0].reconstruct().records), original);
    } finally {
      for (const writer of writers) writer.close();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
}
