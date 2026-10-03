// @ts-check
import { describe, expect, it } from "vitest";
import { CODEMODS, CURRENT_VERSION, upgradeTransaction, upgradeTransactions } from "./dispatch_work_coordinator_codemods.cjs";
import { applyTransactions, parseTransactionLog, replayTransactions, serializeTransactionLog } from "./dispatch_work_coordinator_replay.cjs";
import { selectNext } from "./dispatch_work_coordinator_selection.cjs";

const versionTwo = [
  { version: 2, kind: "Work", work_id: "z", work: { priority: 3, group: "a", nested: { value: true } }, sequence: 42 },
  { version: 2, kind: "Work", work_id: "a", work: { priority: 1, group: "b" }, sequence: 43 },
  { version: 2, kind: "Claim", work_id: "a", claim_id: "old", run_id: "trusted-run" },
  { version: 2, kind: "ClaimCancellation", work_id: "a", claim_id: "old" },
];

describe("version 3 dispatch coordinator codemod", () => {
  it("declares an ordered, header-only version-2 upgrade", () => {
    expect(CURRENT_VERSION).toBe(3);
    expect(CODEMODS.find(codemod => codemod.from === 2)).toEqual({ from: 2, to: 3, rename: {}, set: { version: 3 } });
    const original = structuredClone(versionTwo);
    expect(upgradeTransactions(versionTwo)).toEqual(versionTwo.map(transaction => ({ ...transaction, version: 3 })));
    expect(versionTwo).toEqual(original);
    for (const transaction of upgradeTransactions(versionTwo)) expect(upgradeTransaction(transaction)).toBe(transaction);
  });

  it("preserves every message kind, identities, payloads, sequence, and provenance", () => {
    for (const transaction of [...versionTwo, { version: 2, kind: "Completion", work_id: "a", claim_id: "old", attempt_id: "attempt", outcome: "" }, { version: 2, kind: "WorkCancellation", work_id: "z" }]) {
      expect(upgradeTransaction(transaction)).toEqual({ ...transaction, version: 3 });
    }
  });

  it("preserves replay, selection, compaction, and future mutation decisions", () => {
    const upgraded = parseTransactionLog(versionTwo.map(transaction => JSON.stringify(transaction)).join("\n") + "\n");
    expect(replayTransactions(upgraded)).toEqual(replayTransactions(versionTwo));
    expect(selectNext(upgraded).work_id).toBe("z");
    expect(selectNext(parseTransactionLog(serializeTransactionLog(upgraded))).work_id).toBe("z");
    const claim = { version: 3, kind: "Claim", work_id: "z", claim_id: "new", run_id: "new-run" };
    expect(applyTransactions(upgraded, [claim])).toEqual(applyTransactions(versionTwo, [claim]));
  });

  it("does not invent missing version-2 metadata or accept future versions", () => {
    const invalid = [
      { version: 2, kind: "Work", work_id: "w", work: {} },
      { version: 2, kind: "Claim", work_id: "a", claim_id: "c" },
      { ...versionTwo[0], version: 4 },
    ];
    for (const transaction of invalid) {
      expect(() => parseTransactionLog(`${JSON.stringify(versionTwo[0])}\n${JSON.stringify(transaction)}\n`)).toThrow();
    }
  });

  it("rejects sequence exhaustion while assigning historical FIFO ranks", () => {
    const maximum = { version: 3, kind: "Work", work_id: "max", work: { task: "max" }, sequence: Number.MAX_SAFE_INTEGER };
    const historical = { version: 1, kind: "Work", work: "legacy", claim: null, attempt: null };
    expect(() => upgradeTransactions([maximum, historical])).toThrow("sequence exhausted");
  });
});
