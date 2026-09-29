// @ts-check
"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { normalizeLedgerAppends } = require("./ledger_transactions.cjs");

test("normalizes temporary IDs deterministically and rewrites references", () => {
  const options = { transactionId: "tx-1", ledgerNames: new Set(["default"]) };
  const first = normalizeLedgerAppends([{ temp_id: "finding", record: { type: "finding", payload: { subject: "x" } } }, { record: { type: "followup", parents: ["finding"], payload: {} } }], options);
  const second = normalizeLedgerAppends([{ temp_id: "finding", record: { type: "finding", payload: { subject: "x" } } }, { record: { type: "followup", parents: ["finding"], payload: {} } }], options);
  assert.deepEqual(first, second);
  assert.equal(first.appends[1].record.parents[0], first.appends[0].record.id);
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
