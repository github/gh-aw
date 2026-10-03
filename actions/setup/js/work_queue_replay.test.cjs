// @ts-check
import { describe, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import selectionFixtures from "../../../specs/work-queue/selection-fixtures.json";
import { applyTransactions, claimOldestAvailableWork, compactTransactions, createWorkTransaction, oldestAvailableWork, parseTransactionLog, replayTransactions, serializeTransactionLog, validateTransaction } from "./work_queue_replay.cjs";
import { CODEMODS, CURRENT_VERSION, upgradeTransaction } from "./work_queue_codemods.cjs";

const work = id => ({ version: CURRENT_VERSION, kind: "Work", work: id, claim: null, attempt: null });
const claim = (workId, id) => ({ version: CURRENT_VERSION, kind: "Claim", work: workId, claim: id, attempt: null });
const cancelClaim = (workId, id) => ({ version: CURRENT_VERSION, kind: "ClaimCancellation", work: workId, claim: id, attempt: null });
const complete = (workId, claimId, attempt) => ({ version: CURRENT_VERSION, kind: "Completion", work: workId, claim: claimId, attempt });
const cancelWork = id => ({ version: CURRENT_VERSION, kind: "WorkCancellation", work: id, claim: null, attempt: null });

function permutations(items) {
  if (items.length < 2) return [items];
  return items.flatMap((item, index) => permutations([...items.slice(0, index), ...items.slice(index + 1)]).map(permutation => [item, ...permutation]));
}

describe("work queue replay", () => {
  it("logs debug summaries without transaction identifiers", () => {
    const previousDebug = process.env.DEBUG;
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    process.env.DEBUG = "work_queue";
    try {
      replayTransactions([work("submitted-work-id")]);

      expect(errorSpy).toHaveBeenCalledOnce();
      expect(errorSpy.mock.calls.flat().join(" ")).not.toContain("submitted-work-id");
    } finally {
      if (previousDebug === undefined) delete process.env.DEBUG;
      else process.env.DEBUG = previousDebug;
      errorSpy.mockRestore();
    }
  });

  it("projects work and claim states from the transaction facts", () => {
    expect(replayTransactions([work("w"), claim("w", "c")])).toEqual({
      work: { w: "claimed" },
      winner: { w: "c" },
      claim: { c: "effective" },
      available: [],
      transactions: [claim("w", "c"), work("w")],
    });
  });

  describe("oldest-available work selection", () => {
    for (const fixture of selectionFixtures) {
      it(fixture.name, () => {
        const id = task => createHash("sha256").update(JSON.stringify({ task })).digest("hex");
        const transactions = fixture.works.flatMap(item => {
          const workId = id(item.task);
          const result = [item.enqueued === undefined ? work(workId) : createWorkTransaction(workId, item.enqueued)];
          const claimId = `claim-${item.task}`;
          if (["claimed", "completed", "released"].includes(item.state)) result.push(claim(workId, claimId));
          if (item.state === "completed") result.push(complete(workId, claimId, `attempt-${item.task}`));
          if (item.state === "cancelled") result.push(cancelWork(workId));
          if (item.state === "released") result.push(cancelClaim(workId, claimId));
          return result;
        });
        const expected = fixture.expected.map(id);
        for (const log of [transactions, [...transactions].reverse(), [...transactions, ...transactions], compactTransactions(transactions), parseTransactionLog(serializeTransactionLog(transactions))]) {
          expect(replayTransactions(log).available).toEqual(expected);
          expect(oldestAvailableWork(log)).toBe(expected[0] ?? null);
        }
      });
    }

    it("captures enqueue time once and keeps original age on resubmission", () => {
      const now = vi.spyOn(Date, "now").mockReturnValue(100);
      try {
        const first = createWorkTransaction("w");
        expect(first.enqueued).toBe(100);
        expect(Object.isFrozen(first)).toBe(true);
        now.mockReturnValue(200);
        const result = applyTransactions([first], [createWorkTransaction("w")]);
        expect(result.transactions).toEqual([first]);
        expect(result.rejected).toEqual([]);
      } finally {
        now.mockRestore();
      }
    });

    it("stages successive claims from the local view without head-of-line blocking", () => {
      let log = [createWorkTransaction("old", 1), createWorkTransaction("new", 2)];
      const first = claimOldestAvailableWork(log, "first");
      expect(first.work).toBe("old");
      log = applyTransactions(log, [first]).transactions;
      expect(claimOldestAvailableWork(log, "second").work).toBe("new");
      expect(() => claimOldestAvailableWork([], "none")).toThrow("no available Work");
      expect(() => claimOldestAvailableWork(log, "")).toThrow("claim must be");
    });

    it("uses UTF-8 identity order consistently with Go and handles numeric/prototype keys", () => {
      expect(replayTransactions(["\u{10000}", "\uE000", "2", "10", "__proto__"].map(id => createWorkTransaction(id, 1))).available).toEqual(["10", "2", "__proto__", "\uE000", "\u{10000}"]);
    });

    it("rejects invalid or conflicting enqueue metadata instead of rewriting history", () => {
      for (const enqueued of [-1, 0.5, Number.MAX_SAFE_INTEGER + 1, Infinity, "1", null, undefined]) {
        expect(() => validateTransaction({ ...work("w"), enqueued })).toThrow("enqueued");
      }
      expect(() => validateTransaction({ ...claim("w", "c"), enqueued: 1 })).toThrow("optional enqueued only on Work");
      expect(() => replayTransactions([createWorkTransaction("w", 1), createWorkTransaction("w", 2)])).toThrow("conflicting enqueue metadata");
      expect(() => replayTransactions([work("w"), createWorkTransaction("w", 1)])).toThrow("conflicting enqueue metadata");
      expect(replayTransactions([work("w"), { ...work("w"), enqueued: 0 }]).transactions).toEqual([work("w")]);
      expect(createWorkTransaction("maximum", Number.MAX_SAFE_INTEGER).enqueued).toBe(Number.MAX_SAFE_INTEGER);
    });
  });

  it("produces the same projection for every ordering of a valid fact set", () => {
    const transactions = [work("w"), claim("w", "a"), claim("w", "b"), cancelClaim("w", "a"), work("other")];
    const expected = replayTransactions(transactions);
    for (const permutation of permutations(transactions)) {
      expect(replayTransactions(permutation)).toEqual(expected);
      expect(JSON.stringify(replayTransactions(permutation))).toBe(JSON.stringify(expected));
    }
    expect(expected.winner.w).toBe("b");
    expect(expected.work.other).toBe("available");
  });

  it("supports prototype-named identifiers without treating inherited members as state", () => {
    const result = applyTransactions([], [claim("constructor", "toString"), cancelWork("valueOf"), work("__proto__"), claim("__proto__", "constructor")]);

    expect(result.rejected.map(item => item.reason)).toEqual(["work does not exist", "work does not exist"]);
    const projection = replayTransactions(result.transactions);
    expect(projection.work["__proto__"]).toBe("claimed");
    expect(projection.winner["__proto__"]).toBe("constructor");
    expect(projection.claim.constructor).toBe("effective");
  });

  it("uses lexicographic claim identity for deterministic arbitration", () => {
    const projection = replayTransactions([work("w"), claim("w", "claim-2"), claim("w", "claim-10")]);
    expect(projection.winner.w).toBe("claim-10");
    expect(projection.claim).toEqual({ "claim-10": "effective", "claim-2": "superseded" });
  });

  it("promotes the next active claim after cancellation", () => {
    const projection = replayTransactions([work("w"), claim("w", "a"), claim("w", "b"), cancelClaim("w", "a")]);
    expect(projection.winner.w).toBe("b");
    expect(projection.claim).toEqual({ a: "cancelled", b: "effective" });
  });

  it("fixes the winner when a valid completion exists", () => {
    const transactions = [work("w"), claim("w", "a"), claim("w", "z"), complete("w", "a", "run-1")];
    const projections = permutations(transactions).map(replayTransactions);
    expect(projections.every(projection => projection.work.w === "completed" && projection.winner.w === "a")).toBe(true);
  });

  it("cancels work and all its claims", () => {
    const projection = replayTransactions([work("w"), claim("w", "a"), cancelWork("w")]);
    expect(projection.work.w).toBe("cancelled");
    expect(projection.winner.w).toBeNull();
    expect(projection.claim.a).toBe("cancelled");
  });

  it("treats identical physical records as idempotent and compacts to a stable order", () => {
    const transactions = [claim("w", "c"), work("w"), claim("w", "c"), work("w")];
    const compacted = compactTransactions(transactions);
    expect(compacted).toEqual(compactTransactions([...transactions].reverse()));
    expect(compacted).toHaveLength(2);
    expect(replayTransactions(compacted)).toEqual(replayTransactions(transactions));

    const applied = applyTransactions(compacted, [claim("w", "c")]);
    expect(applied.rejected).toEqual([]);
    expect(applied.transactions).toHaveLength(2);
    expect(replayTransactions(applyTransactions(transactions, [claim("w", "later")]).transactions)).toEqual(replayTransactions(applyTransactions(compacted, [claim("w", "later")]).transactions));
  });

  it("returns immutable normalized facts instead of retaining caller-owned objects", () => {
    const submittedWork = work("w");
    const applied = applyTransactions([], [submittedWork]);
    submittedWork.work = "tampered";

    expect(replayTransactions(applied.transactions).work).toEqual({ w: "available" });
    expect(Object.isFrozen(applied.transactions[0])).toBe(true);
  });

  it("parses and serializes a canonical JSONL transaction log", () => {
    const log = serializeTransactionLog([claim("w", "c"), work("w"), claim("w", "c")]);
    expect(log).toBe(`${JSON.stringify(claim("w", "c"))}\n${JSON.stringify(work("w"))}\n`);
    expect(parseTransactionLog(log)).toEqual([claim("w", "c"), work("w")]);
    expect(parseTransactionLog("")).toEqual([]);
  });

  it("upgrades all historical kinds deterministically to v2 without inventing age", () => {
    expect(CURRENT_VERSION).toBe(2);
    expect(CODEMODS).toEqual([
      { from: 0, to: 1, set: { version: 1 } },
      { from: 1, to: 2, set: { version: 2 } },
    ]);
    const facts = [work("w"), claim("w", "a"), claim("w", "b"), cancelClaim("w", "a"), complete("w", "b", "run"), work("other"), cancelWork("other")];
    for (const version of [undefined, 0, 1]) {
      const historical = facts.map(({ version: _, ...fact }) => (version === undefined ? fact : { version, ...fact }));
      const before = JSON.stringify(historical);
      const upgraded = parseTransactionLog(`${historical.map(fact => JSON.stringify(fact)).join("\n")}\n`);
      expect(upgraded).toEqual(facts);
      expect(serializeTransactionLog(upgraded)).toBe(serializeTransactionLog(facts));
      expect(upgradeTransaction(historical[0])).toEqual(work("w"));
      expect(upgradeTransaction(upgradeTransaction(historical[0]))).toEqual(work("w"));
      expect(JSON.stringify(historical)).toBe(before);
      expect(upgraded.filter(fact => fact.kind === "Work").every(fact => !Object.hasOwn(fact, "enqueued"))).toBe(true);
    }
    const legacyWork = { version: 1, kind: "Work", work: "legacy", claim: null, attempt: null };
    const upgraded = parseTransactionLog(`${JSON.stringify(createWorkTransaction("new", 1))}\n${JSON.stringify(legacyWork)}\n`);
    expect(replayTransactions(upgraded).available).toEqual(["legacy", "new"]);
    expect(applyTransactions(upgraded, [createWorkTransaction("legacy", 100)]).transactions).toEqual(upgraded);
    expect(() => validateTransaction(legacyWork)).toThrow("unsupported work queue transaction version");
    expect(() => validateTransaction({ kind: "Work", work: "w", claim: null, attempt: null })).toThrow("exactly version");
  });

  it("rejects extra metadata on closed historical versions before upgrading", () => {
    for (const version of [undefined, 0, 1]) {
      for (const fact of [work("w"), claim("w", "c")]) {
        const { version: _, ...fields } = fact;
        const historical = version === undefined ? fields : { version, ...fields };
        for (const extra of [{ enqueued: 0 }, { enqueued: 123 }, { run_id: "run" }, { extra: true }]) {
          expect(() => upgradeTransaction({ ...historical, ...extra })).toThrow("historical transaction must contain exactly");
          expect(() => parseTransactionLog(`${JSON.stringify({ ...historical, ...extra })}\n`)).toThrow("historical transaction must contain exactly");
        }
        const missingField = { ...historical };
        delete missingField.attempt;
        expect(() => upgradeTransaction(missingField)).toThrow("historical transaction must contain exactly");
      }
    }
  });

  it("rejects unknown or malformed message versions and never accepts partial upgrades", () => {
    for (const version of [-1, 1.5, "1", 3, null, true, {}, Number.MAX_SAFE_INTEGER + 1]) {
      expect(() => parseTransactionLog(`${JSON.stringify(work("w"))}\n${JSON.stringify({ ...claim("w", "c"), version })}\n`)).toThrow("unsupported work queue transaction version");
    }
    expect(() => parseTransactionLog(`${JSON.stringify({ kind: "Work", work: "w", claim: null, attempt: null, extra: true })}\n`)).toThrow("exactly version");
    expect(() => parseTransactionLog(`${JSON.stringify({ version: 1, kind: "Work", work: "w", claim: "invalid", attempt: null })}\n`)).toThrow("must not include");
    expect(() => serializeTransactionLog([{ ...work("w"), version: 1 }])).toThrow("unsupported work queue transaction version");
  });

  it("rejects malformed JSONL records and blank lines", () => {
    expect(() => parseTransactionLog("{")).toThrow("malformed JSON");
    expect(() => parseTransactionLog(`${JSON.stringify(work("w"))}\n\n`)).toThrow("blank lines");
    expect(() => parseTransactionLog(`${JSON.stringify(work("w"))}\n${JSON.stringify(claim("missing", "c"))}\n`)).toThrow("missing work");
  });

  it("rejects malformed transaction shapes and identifiers", () => {
    for (const transaction of [null, [], { ...work("w"), unexpected: true }, { ...work(""), kind: "Claim" }, { ...claim("w", "c"), attempt: "run" }, { ...complete("w", "c", "run"), attempt: null }, { ...work("w"), kind: "Unknown" }]) {
      expect(() => validateTransaction(transaction)).toThrow(TypeError);
    }
  });

  it("rejects missing references and references to a different work item", () => {
    expect(() => replayTransactions([cancelClaim("w", "missing")])).toThrow("missing work");
    expect(() => replayTransactions([work("w"), cancelClaim("w", "missing")])).toThrow("missing claim");
    expect(() => replayTransactions([work("w"), work("other"), claim("w", "c"), complete("other", "c", "run")])).toThrow("different work");
  });

  it("rejects a completion for a superseded or cancelled claim", () => {
    expect(() => replayTransactions([work("w"), claim("w", "a"), claim("w", "b"), complete("w", "b", "run")])).toThrow("winning claim");
    expect(() => replayTransactions([work("w"), claim("w", "a"), cancelClaim("w", "a"), complete("w", "a", "run")])).toThrow("winning claim");
  });

  it("rejects conflicting claim identities, repeated attempts, and multiple terminal facts", () => {
    expect(() => replayTransactions([work("w"), work("other"), claim("w", "c"), claim("other", "c")])).toThrow("conflicting transactions");
    expect(() => replayTransactions([work("w"), claim("w", "a"), complete("w", "a", "run"), complete("w", "a", "run-2")])).toThrow("multiple terminal");
    expect(() => replayTransactions([work("w"), claim("w", "a"), complete("w", "a", "run"), cancelWork("w")])).toThrow("multiple terminal");
    expect(() => replayTransactions([work("w"), claim("w", "a"), complete("w", "a", "run"), work("other"), claim("other", "b"), complete("other", "b", "run")])).toThrow("multiple completions");
  });
});

describe("work queue transaction application", () => {
  it("applies valid intents in order and rejects state changes after terminal work", () => {
    const result = applyTransactions([], [work("w"), claim("w", "c"), complete("w", "c", "run"), claim("w", "late"), cancelWork("w")]);
    expect(replayTransactions(result.transactions).work.w).toBe("completed");
    expect(result.rejected).toEqual([
      { transaction: claim("w", "late"), reason: "work is terminal" },
      { transaction: cancelWork("w"), reason: "work is terminal" },
    ]);
  });

  it("reports invalid intents without adding them to the durable candidate", () => {
    const result = applyTransactions([work("w"), claim("w", "winner")], [claim("missing", "c"), claim("w", "winner"), complete("w", "unknown", "run"), complete("w", "winner", "run")]);
    expect(result.rejected).toEqual([
      { transaction: claim("missing", "c"), reason: "work does not exist" },
      { transaction: complete("w", "unknown", "run"), reason: "claim does not exist" },
    ]);
    expect(replayTransactions(result.transactions).work.w).toBe("completed");
  });

  it("rejects mutations with mismatched claim ownership", () => {
    const result = applyTransactions([work("w"), work("other"), claim("w", "c")], [cancelClaim("other", "c"), complete("other", "c", "run")]);
    expect(result.rejected.map(item => item.reason)).toEqual(["claim belongs to different work", "claim belongs to different work"]);
    expect(replayTransactions(result.transactions).claim.c).toBe("effective");
  });
});
