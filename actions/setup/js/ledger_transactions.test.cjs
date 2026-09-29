// @ts-check
"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { normalizeLedgerAppends } = require("./ledger_transactions.cjs");

test("normalizes temporary IDs deterministically and rewrites references", () => {
  const options = { transactionId: "tx-1", ledgerNames: new Set(["default"]) };
  const first = normalizeLedgerAppends([{ temp_id: "finding", record: { type: "finding", payload: { subject: "x" } } }, { record: { type: "followup", related_records: ["finding"], payload: {} } }], options);
  const second = normalizeLedgerAppends([{ temp_id: "finding", record: { type: "finding", payload: { subject: "x" } } }, { record: { type: "followup", related_records: ["finding"], payload: {} } }], options);
  assert.deepEqual(first, second);
  assert.equal(first.appends[1].record.related_records[0], first.appends[0].record.id);
});

test("validates records, rejects envelope fields, and enforces ledger limits", () => {
  const options = {
    transactionId: "tx-1",
    ledgerConfigs: new Map([
      [
        "default",
        {
          schema: {
            type: "object",
            required: ["subject"],
            properties: { subject: { type: "string" } },
          },
          maxRecordKB: 1,
          maxPatchKB: 1,
        },
      ],
    ]),
  };

  assert.throws(() => normalizeLedgerAppends([{ record: { subject: "x", id: "forged" } }], options), /reserved field/);
  assert.throws(() => normalizeLedgerAppends([{ record: { subject: 1 } }], options), /configured schema/);
  assert.throws(() => normalizeLedgerAppends([{ record: { subject: "x".repeat(2 * 1024) } }], options), /record-size limit/);
});

test("enforces the configured per-ledger patch limit after temporary-ID resolution", () => {
  const options = {
    transactionId: "tx-1",
    ledgerConfigs: new Map([["default", { maxRecordKB: 1, maxPatchKB: 1 }]]),
  };
  assert.throws(() => normalizeLedgerAppends([{ temp_id: "first", record: { subject: "x".repeat(600) } }, { record: { related: "first", subject: "y".repeat(600) } }], options), /patch-size limit/);
});

test("rewrites temporary IDs only within the selected ledger", () => {
  const options = { transactionId: "tx-1", ledgerNames: new Set(["first", "second"]) };
  const result = normalizeLedgerAppends(
    [
      { ledger: "first", temp_id: "same", record: { subject: "first" } },
      { ledger: "second", temp_id: "same", record: { subject: "second" } },
      { ledger: "first", record: { parent: "same" } },
      { ledger: "second", record: { parent: "same" } },
    ],
    options
  );
  assert.equal(result.appends[2].record.parent, result.appends[0].record.id);
  assert.equal(result.appends[3].record.parent, result.appends[1].record.id);
});

test("rejects unknown ledgers and duplicate temporary IDs", () => {
  assert.throws(
    () =>
      normalizeLedgerAppends([{ ledger: "missing", record: {} }], {
        transactionId: "tx-1",
        ledgerNames: new Set(["default"]),
      }),
    /Unknown target ledger/
  );
  assert.throws(
    () =>
      normalizeLedgerAppends(
        [
          { temp_id: "same", record: {} },
          { temp_id: "same", record: {} },
        ],
        { transactionId: "tx-1", ledgerNames: new Set(["default"]) }
      ),
    /duplicate temporary ID/
  );
});
