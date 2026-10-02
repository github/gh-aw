// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import fs from "node:fs";
import { createRequire } from "node:module";
import { createReducer, replayBuiltin } from "./ledger_builtin.cjs";
import { finalId, normalizeLedgerAppends } from "./ledger_transactions.cjs";
import { validateTransactions } from "./push_ledger_changes.cjs";

const require = createRequire(import.meta.url);
const { operations, sourceDigest } = require("./generate_ledger_protocol_vectors.cjs");
const { sourceDigest: generatedFrom, vectors } = JSON.parse(fs.readFileSync(new URL("./ledger_protocol_vectors.json", import.meta.url), "utf8"));

function canonicalId(id) {
  return finalId(`model-${id}`, 0);
}

function payload(kind, event) {
  const { op, key, value } = event;
  switch (kind) {
    case "raw":
      return { value };
    case "log":
    case "set":
      return { operation: op, value };
    case "map":
      return op === "delete" ? { operation: op, key } : { operation: op, key, value };
    case "table":
      if (op === "insert" || op === "upsert") return { operation: op, value: { id: key, field: value } };
      return op === "update" ? { operation: op, key, patch: { field: value } } : { operation: op, key };
    case "counter":
      return { operation: op, name: key, amount: 1 };
    case "notes":
      return op === "note"
        ? { operation: "note", subject: "Model note", note: value, reason: "Model evidence", citations: [{ type: "repository", path: "README.md" }] }
        : { operation: "vote", note_id: canonicalId(key), vote: value === "x" ? "up" : "down" };
    default:
      throw new Error(`Unknown modeled ledger: ${kind}`);
  }
}

function compareProjection(kind, history, projection) {
  if (kind === "raw") {
    assert.deepEqual(
      projection.log,
      history.map(event => event.value)
    );
    return;
  }
  const config = { type: kind, ...(kind === "table" ? { key: "id" } : {}) };
  const records = history.map(event => ({ id: canonicalId(event.id), payload: payload(kind, event) }));
  const result = replayBuiltin(config, records);
  if (kind === "notes") {
    assert.deepEqual(result.tables.notes.rows.map(row => row.id).sort(), projection.notes.map(canonicalId).sort());
    assert.deepEqual(result.tables.note_votes.rows.map(row => [row.record_id, row.note_id, row.vote]).sort(), projection.votes.map(([id, key, value]) => [canonicalId(id), canonicalId(key), value === "x" ? "up" : "down"]).sort());
    return;
  }
  const rows = result.tables.state.rows;
  if (kind === "log") {
    assert.deepEqual(
      rows.map(row => JSON.parse(row.value)),
      projection.log
    );
  } else if (kind === "set") {
    assert.deepEqual(rows.map(row => JSON.parse(row.value)).sort(), projection.members.slice().sort());
  } else if (kind === "counter") {
    assert.deepEqual(Object.fromEntries(rows.map(row => [row.name, row.value])), Object.fromEntries(Object.entries(projection.counts).filter(([key]) => history.some(event => event.key === key))));
  } else {
    assert.deepEqual(Object.fromEntries(rows.map(row => [row.key, kind === "table" ? JSON.parse(row.value).field : JSON.parse(row.value)])), Object.fromEntries(Object.entries(projection.cells).filter(([, value]) => value !== "none")));
  }
}

test("generated vectors match all ledger operations and current TLA+ sources", () => {
  assert.equal(generatedFrom, sourceDigest(), "TLA+ sources changed; regenerate vectors with TLA2TOOLS_JAR");
  assert.deepEqual(
    vectors.map(vector => `${vector.kind}/${vector.op}`).sort(),
    Object.entries(operations())
      .flatMap(([kind, ops]) => ops.map(op => `${kind}/${op}`))
      .sort()
  );
});

for (const vector of vectors) {
  test(`TLC trace: ${vector.kind}/${vector.op}`, () => {
    const { kind, op, initial, steps } = vector;
    const config = kind === "raw" ? {} : { type: kind, ...(kind === "table" ? { key: "id" } : {}) };
    let previous = initial;
    compareProjection(kind, initial.history, initial.projection);
    for (const { action, state } of steps) {
      assert.deepEqual(state.shard.slice().sort(), state.history.map(event => event.id).sort());
      assert.deepEqual(state.history.slice(0, previous.history.length), previous.history);
      if (action === "Queue" || action === "Validate") {
        assert.deepEqual(state.history, previous.history, "queued/validated intents must not become durable");
        assert.deepEqual(state.snapshot, previous.snapshot, "the agent's projection is read-only");
        assert.ok(!state.shard.includes(state.request.id));
      }
      if (action === "Validate") {
        const request = { ledger: "example", ...(kind === "raw" ? { record: payload(kind, state.request) } : payload(kind, state.request)) };
        const transactionId = `model-${state.request.id}`;
        const normalized = normalizeLedgerAppends([request], { transactionId, ledgerNames: new Set(["example"]), ledgers: { example: config } });
        const artifact = { version: 1, transaction_id: transactionId, ledgers: { example: { appends: normalized.appends } } };
        assert.doesNotThrow(() => validateTransactions(artifact, [{ name: "example", ...config }]));
      }
      if (action === "Persist" || action === "ExternalAppend") {
        assert.equal(state.history.length, previous.history.length + 1);
        if (action === "Persist") assert.deepEqual(state.history.at(-1), state.artifact);
      }
      compareProjection(kind, state.history, state.projection);
      previous = state;
    }
    assert.equal(steps.at(-1).action, "Persist");
    assert.equal(previous.history.at(-1).op, op);
  });
}
