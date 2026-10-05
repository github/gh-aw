// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { DatabaseSync } from "node:sqlite";
import { Ledger } from "./ledger_store.cjs";
import { applyTransitionToDirectory, createPlan, loadSegments, parseCompactionConfig, prepareApply, selectSources, validatePlan } from "./ledger_compaction.cjs";
import { createProjection, formatReplayPrompt, formatReplayTable } from "./create_ledger_projection.cjs";
import { replayBuiltin } from "./ledger_builtin.cjs";
import { finalId } from "./ledger_transactions.cjs";

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

test("custom replay is rejected instead of silently falling back to generic records", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-replay-rejected-"));
  const sourceDir = path.join(root, "source");
  const databasePath = path.join(root, "projection", "ledger.db");
  const ledger = new Ledger({ memoryDir: sourceDir });
  try {
    fs.mkdirSync(sourceDir);
    ledger.append("finding", { id: "a" });
    assert.throws(
      () =>
        createProjection({
          sourceDir,
          databasePath,
          config: { name: "findings", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, replay: { script: `return { tables: { items: { columns: { id: "text" }, primaryKey: ["id"], rows: [{ id: "a" }, { id: "a" }] } } }` } },
        }),
      /Custom ledger replay is no longer supported/
    );
    assert.equal(fs.existsSync(databasePath), false);
  } finally {
    ledger.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("compaction preserves replay's ordered logical history", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-replay-compaction-"));
  const sourceDir = path.join(root, "source");
  fs.mkdirSync(sourceDir);
  const config = { name: "findings", type: "log", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10 };
  const first = new Ledger({ memoryDir: sourceDir });
  const second = new Ledger({ memoryDir: sourceDir });
  const compaction = parseCompactionConfig(
    JSON.stringify({ name: "findings", branch_name: "ledgers/findings", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, compaction: { schedule: "daily", min_segments: 2, max_segments: 2 } })
  );
  try {
    first.append("ledger_append", { operation: "append", value: "first" });
    second.append("ledger_append", { operation: "append", value: "second" });
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
          .prepare("SELECT value FROM state ORDER BY position")
          .all()
          .map(row => row.value),
        newDb
          .prepare("SELECT value FROM state ORDER BY position")
          .all()
          .map(row => row.value)
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

test("notes projection derives vote state from canonical records across compaction", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-notes-compaction-"));
  const sourceDir = path.join(root, "source");
  fs.mkdirSync(sourceDir);
  const config = { name: "knowledge", type: "notes", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10 };
  const compaction = parseCompactionConfig(JSON.stringify({ ...config, branch_name: "ledgers/knowledge", compaction: { schedule: "daily", min_segments: 2, max_segments: 2 } }));
  const noteId = finalId("knowledge", 0);
  const voteId = finalId("knowledge", 1);
  const dissentId = finalId("knowledge", 2);
  const first = new Ledger({ memoryDir: sourceDir });
  const second = new Ledger({ memoryDir: sourceDir });
  try {
    first.append(
      "ledger_append",
      {
        operation: "note",
        subject: "README",
        note: "Contains instructions",
        reason: "Read the source",
        citations: [
          { type: "repository", path: "README.md", start_line: 1 },
          { type: "repository", path: "CONTRIBUTING.md" },
          { type: "repository", path: "LICENSE", end_line: 10 },
        ],
      },
      noteId
    );
    second.append("ledger_append", { operation: "vote", note_id: noteId, vote: "up", reason: "Verified" }, voteId);
    first.append("ledger_append", { operation: "vote", note_id: noteId, vote: "down" }, dissentId);
    const envelopes = new Map(first.reconstruct().records.map(record => [record.id, record]));
    assert.deepEqual([...envelopes.keys()].sort(), [noteId, voteId, dissentId].sort());
    assert.ok([...envelopes.values()].every(record => !Object.hasOwn(record.payload, "id")));
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
    const inspect = file => {
      const db = new DatabaseSync(file, { readOnly: true });
      try {
        return {
          stateType: db.prepare("SELECT type FROM sqlite_master WHERE name = 'note_state'").get()?.type,
          notes: db
            .prepare("SELECT * FROM notes ORDER BY id")
            .all()
            .map(row => ({ ...row })),
          citations: db
            .prepare("SELECT * FROM note_citations ORDER BY note_id, ordinal")
            .all()
            .map(row => ({ ...row })),
          votes: db
            .prepare("SELECT * FROM note_votes ORDER BY record_id")
            .all()
            .map(row => ({ ...row })),
          state: db
            .prepare("SELECT * FROM note_state ORDER BY note_id")
            .all()
            .map(row => ({ ...row })),
          metadata: db
            .prepare("SELECT table_name FROM replay_metadata ORDER BY table_name")
            .all()
            .map(row => row.table_name),
        };
      } finally {
        db.close();
      }
    };
    const projected = inspect(before);
    assert.deepEqual(inspect(after), projected);
    assert.equal(projected.stateType, "table");
    assert.deepEqual(projected.notes, [{ id: noteId, subject: "README", note: "Contains instructions", reason: "Read the source", created_at: envelopes.get(noteId).timestamp, record_sha: envelopes.get(noteId).sha }]);
    assert.deepEqual(projected.citations, [
      { note_id: noteId, ordinal: 0, citation_type: "repository", path: "README.md", start_line: 1, end_line: null },
      { note_id: noteId, ordinal: 1, citation_type: "repository", path: "CONTRIBUTING.md", start_line: null, end_line: null },
      { note_id: noteId, ordinal: 2, citation_type: "repository", path: "LICENSE", start_line: null, end_line: 10 },
    ]);
    assert.deepEqual(
      projected.votes,
      [
        { record_id: envelopes.get(voteId).id, note_id: noteId, vote: "up", reason: "Verified", created_at: envelopes.get(voteId).timestamp },
        { record_id: envelopes.get(dissentId).id, note_id: noteId, vote: "down", reason: null, created_at: envelopes.get(dissentId).timestamp },
      ].sort((a, b) => a.record_id.localeCompare(b.record_id))
    );
    assert.deepEqual(projected.state, [
      {
        note_id: noteId,
        upvotes: 1,
        downvotes: 1,
        net_votes: 0,
        last_vote_at: [envelopes.get(voteId).timestamp, envelopes.get(dissentId).timestamp].sort().at(-1),
        last_positive_vote_at: envelopes.get(voteId).timestamp,
      },
    ]);
    assert.deepEqual(projected.metadata, ["note_citations", "note_state", "note_votes", "notes"]);
  } finally {
    first.close();
    second.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("notes projection materializes empty tables for read-only first-run validation", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-notes-empty-projection-"));
  const sourceDir = path.join(root, "source");
  const databasePath = path.join(root, "projection", "ledger.db");
  fs.mkdirSync(sourceDir, { recursive: true });
  try {
    createProjection({
      sourceDir,
      databasePath,
      config: { name: "knowledge", type: "notes", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10 },
    });
    const db = new DatabaseSync(databasePath, { readOnly: true });
    try {
      assert.equal(db.prepare("SELECT type FROM sqlite_master WHERE name = 'note_state'").get()?.type, "table");
      assert.deepEqual(
        db
          .prepare("PRAGMA table_info(note_state)")
          .all()
          .map(column => column.name),
        ["note_id", "upvotes", "downvotes", "net_votes", "last_vote_at", "last_positive_vote_at"]
      );
      assert.deepEqual(db.prepare("SELECT * FROM note_state").all(), []);
    } finally {
      db.close();
    }
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("independent votes reconcile to identical immutable rows and note state in either arrival order", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-notes-concurrent-"));
  const base = path.join(root, "base");
  const branchA = path.join(root, "branch-a");
  const branchB = path.join(root, "branch-b");
  const noteId = finalId("notes-concurrent", 0);
  const upId = finalId("notes-concurrent", 1);
  const downId = finalId("notes-concurrent", 2);
  const config = { name: "knowledge", type: "notes", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10 };
  const append = (dir, time, payload, id) => {
    const ledger = new Ledger({ memoryDir: dir, clock: () => new Date(time) });
    try {
      ledger.append("ledger_append", payload, id);
    } finally {
      ledger.close();
    }
  };
  try {
    fs.mkdirSync(base);
    append(base, "2026-01-01T00:00:00.000Z", { operation: "note", subject: "README", note: "Documented", reason: "Evidence", citations: [{ type: "repository", path: "README.md" }] }, noteId);
    fs.cpSync(base, branchA, { recursive: true });
    fs.cpSync(base, branchB, { recursive: true });
    append(branchA, "2026-01-02T00:00:00.000Z", { operation: "vote", note_id: noteId, vote: "up", reason: "Verified" }, upId);
    append(branchB, "2026-01-03T00:00:00.000Z", { operation: "vote", note_id: noteId, vote: "down" }, downId);
    const a = new Ledger({ memoryDir: branchA });
    const b = new Ledger({ memoryDir: branchB });
    try {
      const up = a.reconstruct().records.find(record => record.id === upId);
      const down = b.reconstruct().records.find(record => record.id === downId);
      assert.deepEqual(up.parents, down.parents);
      assert.notEqual(up.sha, down.sha);
    } finally {
      a.close();
      b.close();
    }

    const snapshots = [];
    for (const [index, order] of [
      [branchA, branchB],
      [branchB, branchA],
    ].entries()) {
      const merged = path.join(root, `merged-${index}`);
      const shardDir = path.join(merged, "ledger", "shards");
      fs.mkdirSync(shardDir, { recursive: true });
      for (const branch of order) {
        for (const file of fs.readdirSync(path.join(branch, "ledger", "shards"))) {
          fs.copyFileSync(path.join(branch, "ledger", "shards", file), path.join(shardDir, file));
        }
      }
      const databasePath = path.join(root, `merged-${index}.db`);
      createProjection({ sourceDir: merged, databasePath, config });
      const db = new DatabaseSync(databasePath, { readOnly: true });
      try {
        snapshots.push({
          records: db
            .prepare("SELECT id, payload FROM records ORDER BY id")
            .all()
            .map(row => ({ ...row })),
          votes: db
            .prepare("SELECT * FROM note_votes ORDER BY record_id")
            .all()
            .map(row => ({ ...row })),
          state: db
            .prepare("SELECT * FROM note_state")
            .all()
            .map(row => ({ ...row })),
        });
      } finally {
        db.close();
      }
    }
    assert.deepEqual(snapshots[0], snapshots[1]);
    assert.equal(snapshots[0].records.length, 3);
    assert.deepEqual(
      snapshots[0].votes,
      [
        { record_id: upId, note_id: noteId, vote: "up", reason: "Verified", created_at: "2026-01-02T00:00:00.000Z" },
        { record_id: downId, note_id: noteId, vote: "down", reason: null, created_at: "2026-01-03T00:00:00.000Z" },
      ].sort((a, b) => a.record_id.localeCompare(b.record_id))
    );
    assert.deepEqual(snapshots[0].state, [{ note_id: noteId, upvotes: 1, downvotes: 1, net_votes: 0, last_vote_at: "2026-01-03T00:00:00.000Z", last_positive_vote_at: "2026-01-02T00:00:00.000Z" }]);
  } finally {
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
