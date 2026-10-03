// @ts-check
import { describe, expect, it } from "vitest";
import fs from "fs";
import { SELECTION_SCHEMA, selectNext, validateSelection } from "./dispatch_work_coordinator_selection.cjs";
import { compactTransactions, parseTransactionLog, serializeTransactionLog } from "./work_queue_replay.cjs";

const fixtures = JSON.parse(fs.readFileSync(new URL("../../../specs/work-queue/selection-fixtures.json", import.meta.url), "utf8"));

describe("shared work-queue selection contract", () => {
  for (const test of fixtures.cases) {
    it(test.name, () => {
      for (const transactions of [fixtures.transactions, [...fixtures.transactions].reverse(), compactTransactions(fixtures.transactions)]) {
        expect(selectNext(transactions, test.selection)?.work_id || null).toBe(test.expected);
      }
    });
  }

  it("rejects every shared invalid policy without executing code", () => {
    for (const selection of fixtures.invalid) expect(() => validateSelection(selection)).toThrow();
    expect(SELECTION_SCHEMA.additionalProperties).toBe(false);
  });

  it("preserves first-seen FIFO while upgrading and compacting legacy logs", () => {
    const transactions = parseTransactionLog(
      [
        { kind: "Work", work: "z", claim: null, attempt: null },
        { version: 1, kind: "Work", work: "a", claim: null, attempt: null },
        { kind: "Work", work: "z", claim: null, attempt: null },
        { version: 1, kind: "Claim", work: "a", claim: "c", attempt: null },
      ]
        .map(transaction => JSON.stringify(transaction))
        .join("\n") + "\n"
    );
    const roundtrip = parseTransactionLog(serializeTransactionLog(transactions));
    expect(selectNext(roundtrip)).toMatchObject({ work_id: "z", sequence: 1 });
    expect(transactions[1].sequence).toBe(2);
    expect(transactions[3].run_id).toBe("legacy:c");
  });
});
