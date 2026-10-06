// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { DatabaseSync } from "node:sqlite";
import { createReducer, replayBuiltin, validateOperation } from "./ledger_builtin.cjs";
import { normalizeLedgerAppends } from "./ledger_transactions.cjs";
import { finalId } from "./ledger_transactions.cjs";
import { validateTransactions } from "./push_ledger_changes.cjs";
import { materializeReplay, validateReplayOutput } from "./ledger_replay.cjs";

const configs = {
  log: { type: "log", schema: { type: "number" } },
  set: { type: "set" },
  map: { type: "map", schema: { type: "number" } },
  table: { type: "table", key: "id", schema: { type: "object", required: ["id", "score"], properties: { id: { type: "string" }, score: { type: "number" } }, additionalProperties: false } },
  counter: { type: "counter" },
};

const sequences = {
  log: [
    { operation: "append", value: 1 },
    { operation: "append", value: 1 },
    { operation: "append", value: 2 },
  ],
  set: [
    { operation: "add", value: { a: 1, b: [true, null] } },
    { operation: "remove", value: { b: [true, null], a: 1 } },
    { operation: "add", value: [false, 0, "0"] },
  ],
  map: [
    { operation: "put", key: "a", value: 1 },
    { operation: "put", key: "a", value: 2 },
    { operation: "delete", key: "absent" },
    { operation: "put", key: "b", value: 3 },
  ],
  table: [
    { operation: "insert", value: { id: "a", score: 1 } },
    { operation: "update", key: "a", patch: { score: 2 } },
    { operation: "upsert", value: { id: "b", score: 3 } },
    { operation: "delete", key: "b" },
  ],
  counter: [
    { operation: "increment", name: "a", amount: 3 },
    { operation: "increment", name: "a", amount: 5 },
    { operation: "decrement", name: "a", amount: 2 },
    { operation: "increment", name: "b", amount: 0 },
  ],
};

for (const [type, config] of Object.entries(configs)) {
  test(`${type} validates transactions and replays deterministically`, () => {
    const records = sequences[type];
    const reducer = createReducer(config);
    assert.deepEqual(reducer.output().tables.state.rows, []);
    for (const record of records) reducer.apply(record);
    const output = reducer.output();
    assert.deepEqual(
      replayBuiltin(
        config,
        records.map(payload => ({ payload }))
      ),
      output
    );
    assert.deepEqual(
      replayBuiltin(
        config,
        records.map(payload => ({ payload }))
      ),
      output
    );
    const options = { transactionId: "tx", ledgerNames: new Set(["memory"]), ledgers: { memory: { ...config, max_record_kb: 32, max_patch_kb: 10 } } };
    const normalized = normalizeLedgerAppends(
      records.map(record => ({ ledger: "memory", ...record })),
      options
    );
    assert.equal(normalized.appends[0].record.id, finalId("tx", 0));
    const ledgerConfig = [{ name: "memory", ...options.ledgers.memory }];
    assert.doesNotThrow(() => validateTransactions({ version: 1, transaction_id: "tx", ledgers: { memory: { appends: normalized.appends } } }, ledgerConfig));
    assert.throws(() => normalizeLedgerAppends([{ operation: "invent", value: 1 }], options), /Unsupported ledger operation/);
    assert.throws(() => normalizeLedgerAppends([{ record: { value: 1 } }], options), /requires an operation/);
  });
}

test("log preserves duplicates in exact order", () => {
  assert.deepEqual(
    replayBuiltin(
      configs.log,
      sequences.log.map(payload => ({ payload }))
    ).tables.state.rows,
    [
      { position: 0, value: "1" },
      { position: 1, value: "1" },
      { position: 2, value: "2" },
    ]
  );
});

test("built-in logs can materialize more rows than the default materialization limit", () => {
  const records = Array.from({ length: 10001 }, (_, value) => ({ payload: { operation: "append", value } }));
  const output = replayBuiltin({ type: "log" }, records);
  assert.throws(() => validateReplayOutput(output), /Too many replay rows/);
  const db = new DatabaseSync(":memory:");
  try {
    materializeReplay(db, "history", "builtin:log", records, output, records.length);
    assert.equal(db.prepare("SELECT count(*) AS count FROM state").get().count, records.length);
  } finally {
    db.close();
  }
});

test("set uses canonical JSON equality across all supported JSON types", () => {
  const reducer = createReducer(configs.set);
  for (const value of ["x", 1, true, null, [1, { a: 1, b: 2 }], { a: [null, false], b: 1 }]) reducer.apply({ operation: "add", value });
  reducer.apply({ operation: "add", value: { b: 1, a: [null, false] } });
  reducer.apply({ operation: "remove", value: [1, { b: 2, a: 1 }] });
  reducer.apply({ operation: "remove", value: "missing" });
  assert.equal(reducer.output().tables.state.rows.length, 5);
  assert.throws(() => reducer.apply({ operation: "add", value: NaN }), /finite/);
  assert.throws(() => reducer.apply({ operation: "add", value: { x: undefined } }), /finite/);
});

test("table requires an existing row for update and rejects duplicate and invalid transitions", () => {
  const reducer = createReducer(configs.table);
  assert.throws(() => reducer.apply({ operation: "update", key: "a", patch: { score: 2 } }), /does not exist/);
  reducer.apply({ operation: "insert", value: { id: "a", score: 1 } });
  assert.throws(() => reducer.apply({ operation: "insert", value: { id: "a", score: 2 } }), /already exists/);
  assert.throws(() => reducer.apply({ operation: "update", key: "a", patch: { id: "b" } }), /primary key/);
  assert.throws(() => reducer.apply({ operation: "update", key: "a", patch: { score: "bad" } }), /does not match schema/);
  reducer.apply({ operation: "upsert", value: { id: "a", score: 9 } });
  assert.deepEqual(reducer.output().tables.state.rows, [{ key: "a", value: '{"id":"a","score":9}' }]);
});

test("table rejects oversized merged rows during updates", () => {
  const reducer = createReducer({
    type: "table",
    key: "id",
    schema: { type: "object", required: ["id", "data"], properties: { id: { type: "string" }, data: { type: "string" } }, additionalProperties: false },
  });
  reducer.apply({ operation: "insert", value: { id: "a", data: "x" } });
  assert.throws(() => reducer.apply({ operation: "update", key: "a", patch: { data: "x".repeat(65530) } }), /Replay cell exceeds size limit/);
});

test("counter rejects coercion, nonfinite and unsafe arithmetic without changing state", () => {
  const reducer = createReducer(configs.counter);
  for (const amount of ["2", NaN, Infinity, -1, 0.5, Number.MAX_SAFE_INTEGER + 1]) {
    assert.throws(() => reducer.apply({ operation: "increment", name: "x", amount }), /safe integer/);
  }
  reducer.apply({ operation: "increment", name: "x", amount: Number.MAX_SAFE_INTEGER });
  assert.throws(() => reducer.apply({ operation: "increment", name: "x", amount: 1 }), /safe integer range/);
  assert.equal(reducer.output().tables.state.rows[0].value, Number.MAX_SAFE_INTEGER);
});

test("notes rejects the former claims type, operations, and fields", () => {
  const note = { operation: "note", subject: "README", note: "Documented", reason: "Evidence", citations: [{ type: "repository", path: "README.md" }] };
  assert.throws(() => createReducer({ type: "claims" }), /Unknown built-in ledger type/);
  assert.throws(() => validateOperation({ ...note, operation: "claim" }, { type: "notes" }), /Unsupported ledger operation/);
  assert.throws(() => validateOperation({ ...note, claim: "Documented" }, { type: "notes" }), /Invalid ledger operation fields/);
  assert.throws(() => validateOperation({ operation: "vote", claim_id: "old", vote: "up" }, { type: "notes" }), /Invalid ledger operation fields/);
});

test("notes projection retains assertions, ordered citations, and every independent vote", () => {
  const config = { type: "notes" };
  const noteId = finalId("envelope", 2);
  const note = {
    operation: "note",
    subject: "README",
    note: "Documented",
    reason: "Checked source",
    citations: [
      { type: "repository", path: "README.md", start_line: 1 },
      { type: "repository", path: "docs/guide.md", start_line: 5, end_line: 8 },
      { type: "repository", path: "CONTRIBUTING.md" },
      { type: "repository", path: "LICENSE", end_line: 10 },
    ],
  };
  const up = { operation: "vote", note_id: noteId, vote: "up", reason: "Confirmed" };
  const down = { operation: "vote", note_id: noteId, vote: "down", reason: "Disputed" };
  const records = [down, up, note].map((payload, index) => ({ id: finalId("envelope", index), timestamp: "2026-01-01T00:00:00.000Z", sha: `sha256:${"a".repeat(64)}`, payload }));
  const output = replayBuiltin(config, records);
  assert.deepEqual(output.tables.notes.rows, [{ id: noteId, subject: "README", note: "Documented", reason: "Checked source", created_at: records[2].timestamp, record_sha: records[2].sha }]);
  assert.deepEqual(
    output.tables.note_citations.rows,
    note.citations.map((citation, ordinal) => ({ note_id: noteId, ordinal, citation_type: citation.type, path: citation.path, start_line: citation.start_line ?? null, end_line: citation.end_line ?? null }))
  );
  assert.deepEqual(
    output.tables.note_votes.rows.map(row => row.record_id),
    records
      .filter(record => record.payload.operation === "vote")
      .sort((a, b) => a.id.localeCompare(b.id))
      .map(record => record.id)
  );
  assert.deepEqual(replayBuiltin(config, [...records].reverse()), output);
  assert.doesNotThrow(() => validateReplayOutput(output));
  assert.throws(() => replayBuiltin(config, [{ ...records[2], payload: { ...note, id: noteId } }]), /must not duplicate/);
});

test("notes projection rejects malformed records and missing vote targets", () => {
  const config = { type: "notes" };
  const note = { id: finalId("notes", 0), operation: "note", subject: "Source", note: "Assertion", reason: "Evidence", citations: [{ type: "repository", path: "README.md", start_line: 1 }] };
  assert.throws(() => validateOperation({ ...note, citations: "README" }, config), /citations/);
  assert.throws(() => validateOperation({ ...note, citations: [] }, config), /at least one/);
  assert.throws(() => validateOperation({ ...note, citations: [{}] }, config), /valid citation/);
  assert.throws(() => validateOperation({ ...note, unknown: 1 }, config), /fields/);
  assert.throws(() => validateOperation({ ...note, subject: "" }, config), /subject/);
  assert.throws(() => validateOperation({ ...note, subject: "a".repeat(513) }, config), /bounded/);
  assert.throws(() => validateOperation({ ...note, note: "a".repeat(4097) }, config), /bounded/);
  assert.doesNotThrow(() => validateOperation({ ...note, reason: "é".repeat(1024) }, config));
  assert.throws(() => validateOperation({ ...note, reason: "é".repeat(1025) }, config), /bounded/);
  assert.throws(() => validateOperation({ ...note, reason: "a".repeat(4097) }, config), /bounded/);
  assert.throws(() => validateOperation({ ...note, citations: Array.from({ length: 33 }, () => ({ type: "repository", path: "README.md", start_line: 1 })) }, config), /valid citation/);
  assert.doesNotThrow(() => validateOperation({ ...note, citations: [{ type: "repository", path: "README.md" }] }, config));
  assert.doesNotThrow(() => validateOperation({ ...note, citations: [{ type: "repository", path: "README.md", end_line: 3 }] }, config));
  for (const citation of [
    { type: "url", path: "README.md", start_line: 1 },
    { type: "repository", path: "../README.md", start_line: 1 },
    { type: "repository", path: "/README.md", start_line: 1 },
    { type: "repository", path: "docs\\README.md", start_line: 1 },
    { type: "repository", path: "docs//README.md", start_line: 1 },
    { type: "repository", path: "C:/README.md", start_line: 1 },
    { type: "repository", path: "README.md", start_line: 0 },
    { type: "repository", path: "README.md", start_line: 1.5 },
    { type: "repository", path: "README.md", end_line: 0 },
    { type: "repository", path: "README.md", start_line: 1, unexpected: true },
  ]) {
    assert.throws(() => validateOperation({ ...note, citations: [citation] }, config), /valid citation/);
  }
  assert.throws(
    () => replayBuiltin(config, [{ id: finalId("envelope", 0), payload: { operation: "note", subject: note.subject, note: note.note, reason: note.reason, citations: [{ type: "repository", path: "../README.md", start_line: 1 }] } }]),
    /valid citation/
  );
  assert.throws(() => validateOperation({ ...note, citations: [{ type: "repository", path: "README.md", start_line: 3, end_line: 2 }] }, config), /valid citation/);
  assert.throws(() => validateOperation({ ...note, citations: [{ type: "repository", path: "a".repeat(2048), start_line: 1 }] }, config), /size limit/);
  assert.throws(() => validateOperation({ operation: "vote", note_id: note.id, vote: "maybe", reason: "No" }, config), /up or down/);
  assert.throws(() => validateOperation({ operation: "vote", note_id: note.id, vote: "up", reason: "é".repeat(1025) }, config), /bounded/);
  assert.throws(() => validateOperation({ operation: "vote", note_id: note.id, vote: "up", reason: "a".repeat(4097) }, config), /bounded/);
  assert.throws(() => replayBuiltin(config, [{ id: finalId("envelope", 1), payload: { operation: "vote", note_id: note.id, vote: "up", reason: "Confirmed" } }]), /missing note/);
  const reducer = createReducer(config);
  assert.throws(() => reducer.apply({ id: finalId("notes", 1), operation: "vote", note_id: note.id, vote: "up", reason: "Confirmed" }), /missing note/);
  reducer.apply(note);
  reducer.apply({ id: finalId("notes", 1), operation: "vote", note_id: note.id, vote: "up" });
  assert.equal(reducer.output().tables.note_votes.rows[0].reason, null);
  assert.throws(() => reducer.apply({ id: finalId("notes", 3), operation: "vote", note_id: finalId("notes", 1), vote: "down", reason: "Not a note" }), /missing note/);
  assert.throws(() => reducer.apply(note), /Duplicate note/);
  assert.throws(() => reducer.apply(note, { id: "" }), /canonical record ID/);
});

test("note_state rejects votes mutated after validation instead of counting them as downvotes", () => {
  const reducer = createReducer({ type: "notes" });
  const noteId = finalId("notes", 0);
  reducer.apply({ id: noteId, operation: "note", subject: "Source", note: "Assertion", reason: "Evidence", citations: [{ type: "repository", path: "README.md" }] });
  const vote = { id: finalId("notes", 1), operation: "vote", note_id: noteId, vote: "up" };
  reducer.apply(vote);
  for (const invalidVote of ["neutral", "", null, undefined]) {
    vote.vote = invalidVote;
    assert.throws(() => reducer.output(), /up or down vote/);
  }
  vote.vote = "down";
  assert.deepEqual(reducer.output().tables.note_state.rows, [{ note_id: noteId, upvotes: 0, downvotes: 1, net_votes: -1, last_vote_at: null, last_positive_vote_at: null }]);
  vote.vote = "up";
  assert.deepEqual(reducer.output().tables.note_state.rows, [{ note_id: noteId, upvotes: 1, downvotes: 0, net_votes: 1, last_vote_at: null, last_positive_vote_at: null }]);
});

test("note_state derives zero votes and null last-vote timestamps", () => {
  const payload = { operation: "note", subject: "Source", note: "Assertion", reason: "Evidence", citations: [{ type: "repository", path: "README.md", start_line: 1 }] };
  const records = [{ id: finalId("envelope", 0), timestamp: "2026-01-01T00:00:00.000Z", sha: `sha256:${"a".repeat(64)}`, payload }];
  const output = replayBuiltin({ type: "notes" }, records);
  const db = new DatabaseSync(":memory:");
  try {
    materializeReplay(db, "notes", "builtin:notes", records, output, 3);
    assert.deepEqual({ ...db.prepare("SELECT * FROM note_state").get() }, { note_id: records[0].id, upvotes: 0, downvotes: 0, net_votes: 0, last_vote_at: null, last_positive_vote_at: null });
  } finally {
    db.close();
  }
});
