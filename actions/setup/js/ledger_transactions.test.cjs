// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { normalizeLedgerAppends } from "./ledger_transactions.cjs";

test("normalizes temporary IDs deterministically and rewrites references", () => {
  const options = { transactionId: "tx-1", ledgerNames: new Set(["default"]) };
  const first = normalizeLedgerAppends([{ temp_id: "finding", record: { type: "finding", payload: { subject: "x" } } }, { record: { type: "followup", related_to: ["#finding"], note: "finding" } }], options);
  const second = normalizeLedgerAppends([{ temp_id: "finding", record: { type: "finding", payload: { subject: "x" } } }, { record: { type: "followup", related_to: ["#finding"], note: "finding" } }], options);
  assert.deepEqual(first, second);
  assert.equal(first.appends[1].record.related_to[0], first.appends[0].record.id);
  assert.equal(first.appends[1].record.note, "finding");
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

test("validates schemas and configured record and patch limits before assigning IDs", () => {
  const options = {
    transactionId: "tx-1",
    ledgerNames: new Set(["findings"]),
    ledgers: {
      findings: {
        schema: { type: "object", required: ["subject"], properties: { subject: { type: "string" } }, additionalProperties: false },
        max_record_kb: 1,
        max_patch_kb: 1,
      },
    },
  };
  assert.throws(() => normalizeLedgerAppends([{ record: { subject: 1 } }], options), /does not match schema/);
  assert.throws(() => normalizeLedgerAppends([{ record: { id: "agent-controlled", subject: "x" } }], options), /reserved field/);
  assert.throws(() => normalizeLedgerAppends([{ record: { payload_sha: "agent-controlled", subject: "x" } }], options), /reserved field/);
  assert.throws(() => normalizeLedgerAppends([{ record: { subject: "x".repeat(1024) } }], options), /max-record-kb/);
  assert.throws(() => normalizeLedgerAppends([{ record: { subject: "x".repeat(600) } }, { record: { subject: "y".repeat(600) } }], options), /max-patch-kb/);
  assert.deepEqual(normalizeLedgerAppends([{ record: { subject: "valid" } }], options).appends[0].record.subject, "valid");
});

test("rewrites temporary IDs only within the selected ledger and only for explicit references", () => {
  const result = normalizeLedgerAppends(
    [
      { ledger: "first", temp_id: "same", record: { subject: "first" } },
      { ledger: "second", temp_id: "same", record: { subject: "second" } },
      { ledger: "first", record: { parent: "#same", note: "same" } },
      { ledger: "second", record: { parent: "#same" } },
    ],
    { transactionId: "tx-1", ledgerNames: new Set(["first", "second"]) }
  );
  assert.equal(result.appends[2].record.parent, result.appends[0].record.id);
  assert.equal(result.appends[2].record.note, "same");
  assert.equal(result.appends[3].record.parent, result.appends[1].record.id);
});
